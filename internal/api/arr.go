package api

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/yeixio/matinee/internal/arr"
	"github.com/yeixio/matinee/internal/store"
)

// Radarr and Sonarr, connected by an admin, let people ask for movies and
// shows, see what's coming soon, report problems with what they watch, and
// let admins change a title's quality or replace a bad file.
//
// The address and API key stay on the server: clients only ever learn
// whether each app is connected.
//
// Who may add titles is a server setting (requestMode). Admins always may.
// The home network's shared account can see what's coming soon but can't
// ask for or report anything, like everything else beyond looking and playing.

const (
	settingRequests = "requests"

	requestsOff     = "off"     // only admins add titles
	requestsApprove = "approve" // people ask; an admin approves
	requestsAuto    = "auto"    // people's requests are added right away
)

var errNotConnected = errors.New("not connected")

// badRequest is a mistake in what an admin entered.
type badRequest string

func (b badRequest) Error() string { return string(b) }

func settingKey(app arr.App) string { return "arr_" + string(app) }

// appFor is the app that handles a kind of title.
func appFor(kind string) (arr.App, bool) {
	switch kind {
	case "movie":
		return arr.Radarr, true
	case "show", "season", "episode":
		return arr.Sonarr, true
	}
	return "", false
}

func (s *Server) arrConfig(ctx context.Context, app arr.App) (arr.Config, error) {
	var c arr.Config
	v, err := s.Store.Setting(ctx, settingKey(app))
	if err != nil || v == "" {
		return c, err
	}
	return c, json.Unmarshal([]byte(v), &c)
}

func (s *Server) arrClient(ctx context.Context, app arr.App) (*arr.Client, arr.Config, error) {
	c, err := s.arrConfig(ctx, app)
	if err != nil {
		return nil, c, err
	}
	if c.URL == "" {
		return nil, c, errNotConnected
	}
	cl, err := arr.New(app, c)
	return cl, c, err
}

func (s *Server) connected(ctx context.Context) (map[arr.App]bool, error) {
	out := map[arr.App]bool{}
	for _, app := range arr.Apps {
		c, err := s.arrConfig(ctx, app)
		if err != nil {
			return nil, err
		}
		out[app] = c.URL != ""
	}
	return out, nil
}

func (s *Server) requestMode(ctx context.Context) (string, error) {
	v, err := s.Store.Setting(ctx, settingRequests)
	return cmp.Or(v, requestsApprove), err
}

// arrError answers for a failed call to Radarr or Sonarr. Only admins see
// the details, which can include the app's address.
func (s *Server) arrError(w http.ResponseWriter, r *http.Request, app arr.App, err error) {
	var ae *arr.Error
	var bad badRequest
	switch {
	case errors.As(err, &bad):
		writeError(w, http.StatusBadRequest, bad.Error())
	case errors.Is(err, errNotConnected):
		writeError(w, http.StatusConflict, app.Name()+" isn't connected")
	case errors.Is(err, arr.ErrNotFound):
		writeError(w, http.StatusNotFound, app.Name()+" doesn't have this title")
	case errors.As(err, &ae):
		s.Log.Warn("arr", "app", app, "err", err)
		msg := ae.Message
		if !currentUser(r).IsAdmin {
			msg = app.Name() + " isn't answering right now. Try again later."
		}
		writeError(w, http.StatusBadGateway, msg)
	default:
		s.serverError(w, err)
	}
}

// ttlCache keeps answers from Radarr and Sonarr for a while, so pages that
// show them don't each wait on those apps. Changing a connection clears it.
type ttlCache struct {
	mu      sync.Mutex
	entries map[string]cacheEntry
}

type cacheEntry struct {
	expires time.Time
	value   any
}

func cached[T any](c *ttlCache, key string, ttl time.Duration, load func() (T, error)) (T, error) {
	c.mu.Lock()
	if e, ok := c.entries[key]; ok && time.Now().Before(e.expires) {
		c.mu.Unlock()
		return e.value.(T), nil
	}
	c.mu.Unlock()
	v, err := load()
	if err != nil {
		return v, err
	}
	c.mu.Lock()
	if c.entries == nil {
		c.entries = map[string]cacheEntry{}
	}
	c.entries[key] = cacheEntry{time.Now().Add(ttl), v}
	c.mu.Unlock()
	return v, nil
}

func (c *ttlCache) clear() {
	c.mu.Lock()
	c.entries = nil
	c.mu.Unlock()
}

func (s *Server) qualityProfiles(ctx context.Context, cl *arr.Client) ([]arr.QualityProfile, error) {
	return cached(&s.arrCache, "profiles:"+string(cl.App), 5*time.Minute, func() ([]arr.QualityProfile, error) {
		return cl.QualityProfiles(ctx)
	})
}

// readOptionalJSON is readJSON for bodies that may be left out.
func readOptionalJSON(r *http.Request, v any) error {
	if err := readJSON(r, v); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

// ---- connecting (admins) ----

type integrationView struct {
	Connected        bool   `json:"connected"`
	URL              string `json:"url"`
	HasAPIKey        bool   `json:"hasApiKey"`
	QualityProfileID int    `json:"qualityProfileId"`
	RootFolder       string `json:"rootFolder"`
}

func viewIntegration(c arr.Config) integrationView {
	return integrationView{Connected: c.URL != "", URL: c.URL, HasAPIKey: c.APIKey != "", QualityProfileID: c.QualityProfileID, RootFolder: c.RootFolder}
}

func pathApp(w http.ResponseWriter, r *http.Request) (arr.App, bool) {
	app, ok := arr.ParseApp(r.PathValue("app"))
	if !ok {
		writeError(w, http.StatusNotFound, "connect radarr or sonarr")
	}
	return app, ok
}

func (s *Server) getIntegrations(w http.ResponseWriter, r *http.Request) {
	out := map[arr.App]integrationView{}
	for _, app := range arr.Apps {
		c, err := s.arrConfig(r.Context(), app)
		if err != nil {
			s.serverError(w, err)
			return
		}
		out[app] = viewIntegration(c)
	}
	writeJSON(w, http.StatusOK, out)
}

// candidate is the connection an admin is trying. Leaving the API key out
// keeps the saved one, but only for the saved address, so the key can't be
// sent somewhere new without typing it again.
func (s *Server) candidate(ctx context.Context, app arr.App, in arr.Config) (arr.Config, error) {
	saved, err := s.arrConfig(ctx, app)
	if err != nil {
		return in, err
	}
	u, err := arr.NormalizeURL(in.URL)
	if err != nil {
		return in, badRequest(err.Error())
	}
	in.URL, in.APIKey = u, strings.TrimSpace(in.APIKey)
	if in.APIKey == "" {
		if saved.APIKey == "" || saved.URL != in.URL {
			return in, badRequest("enter the API key (in " + app.Name() + " under Settings → General)")
		}
		in.APIKey = saved.APIKey
	}
	return in, nil
}

type connectionCheck struct {
	// URL is the address that works, which differs from the one entered
	// when the app has a URL base it redirected to.
	URL             string               `json:"url"`
	AppName         string               `json:"appName"`
	Version         string               `json:"version"`
	QualityProfiles []arr.QualityProfile `json:"qualityProfiles"`
	RootFolders     []arr.RootFolder     `json:"rootFolders"`
}

func check(ctx context.Context, app arr.App, c arr.Config) (connectionCheck, error) {
	var out connectionCheck
	cl, err := arr.New(app, c)
	if err != nil {
		return out, badRequest(err.Error())
	}
	st, err := cl.Status(ctx)
	if err != nil {
		return out, err
	}
	out.URL, out.AppName, out.Version = cl.URL(), st.AppName, st.Version
	if out.QualityProfiles, err = cl.QualityProfiles(ctx); err != nil {
		return out, err
	}
	out.RootFolders, err = cl.RootFolders(ctx)
	return out, err
}

// testIntegration checks an address and key without saving them, and lists
// the quality profiles and folders to choose defaults from.
func (s *Server) testIntegration(w http.ResponseWriter, r *http.Request) {
	app, ok := pathApp(w, r)
	if !ok {
		return
	}
	var in arr.Config
	if err := readOptionalJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if in.URL == "" {
		saved, err := s.arrConfig(r.Context(), app)
		if err != nil {
			s.serverError(w, err)
			return
		}
		in.URL = saved.URL
	}
	c, err := s.candidate(r.Context(), app, in)
	if err != nil {
		s.arrError(w, r, app, err)
		return
	}
	res, err := check(r.Context(), app, c)
	if err != nil {
		s.arrError(w, r, app, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// putIntegration connects an app, after checking it answers.
func (s *Server) putIntegration(w http.ResponseWriter, r *http.Request) {
	app, ok := pathApp(w, r)
	if !ok {
		return
	}
	var in arr.Config
	if err := readJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	c, err := s.candidate(r.Context(), app, in)
	if err != nil {
		s.arrError(w, r, app, err)
		return
	}
	res, err := check(r.Context(), app, c)
	if err != nil {
		s.arrError(w, r, app, err)
		return
	}
	c.URL = res.URL
	// Defaults for what's added from Matinee: the admin's pick, else the app's first.
	if c.QualityProfileID == 0 && len(res.QualityProfiles) > 0 {
		c.QualityProfileID = res.QualityProfiles[0].ID
	}
	if !slices.ContainsFunc(res.QualityProfiles, func(p arr.QualityProfile) bool { return p.ID == c.QualityProfileID }) {
		writeError(w, http.StatusBadRequest, app.Name()+" has no such quality profile")
		return
	}
	if c.RootFolder == "" && len(res.RootFolders) > 0 {
		c.RootFolder = res.RootFolders[0].Path
	}
	if !slices.ContainsFunc(res.RootFolders, func(f arr.RootFolder) bool { return f.Path == c.RootFolder }) {
		writeError(w, http.StatusBadRequest, "add a root folder in "+app.Name()+" first (Settings → Media Management)")
		return
	}
	b, _ := json.Marshal(c)
	if err := s.Store.SetSetting(r.Context(), settingKey(app), string(b)); err != nil {
		s.serverError(w, err)
		return
	}
	s.arrCache.clear()
	writeJSON(w, http.StatusOK, viewIntegration(c))
}

func (s *Server) deleteIntegration(w http.ResponseWriter, r *http.Request) {
	app, ok := pathApp(w, r)
	if !ok {
		return
	}
	if err := s.Store.SetSetting(r.Context(), settingKey(app), ""); err != nil {
		s.serverError(w, err)
		return
	}
	s.arrCache.clear()
	w.WriteHeader(http.StatusNoContent)
}

// ---- what people can do ----

type requestOptions struct {
	Movies bool `json:"movies"` // Radarr is connected
	Shows  bool `json:"shows"`  // Sonarr is connected
	// Discover says Overseerr is connected, for the Discover tab (see discover.go).
	Discover bool `json:"discover"`
	// CanRequest says this account may ask for titles, and AutoApprove that
	// what it asks for is added without waiting for an admin.
	CanRequest  bool `json:"canRequest"`
	AutoApprove bool `json:"autoApprove"`
	// CanReport says this account may report problems with what it watches.
	CanReport bool `json:"canReport"`
	// Quality profiles to pick from when asking, by kind (movie, show), and
	// the one used when nobody picks.
	QualityProfiles       map[string][]arr.QualityProfile `json:"qualityProfiles,omitempty"`
	DefaultQualityProfile map[string]int                  `json:"defaultQualityProfile,omitempty"`
	// For admins: what's waiting for them.
	PendingRequests int `json:"pendingRequests,omitempty"`
	OpenIssues      int `json:"openIssues,omitempty"`
}

func (s *Server) canRequest(ctx context.Context, u store.User) (can, auto bool, err error) {
	if u.Guest {
		return false, false, nil
	}
	if u.IsAdmin {
		return true, true, nil
	}
	mode, err := s.requestMode(ctx)
	return mode != requestsOff, mode == requestsAuto, err
}

func (s *Server) getRequestOptions(w http.ResponseWriter, r *http.Request) {
	ctx, u := r.Context(), currentUser(r)
	conn, err := s.connected(ctx)
	if err != nil {
		s.serverError(w, err)
		return
	}
	out := requestOptions{Movies: conn[arr.Radarr], Shows: conn[arr.Sonarr], CanReport: !u.Guest}
	oc, err := s.overseerrConfig(ctx)
	if err != nil {
		s.serverError(w, err)
		return
	}
	out.Discover = oc.URL != ""
	can, auto, err := s.canRequest(ctx, u)
	if err != nil {
		s.serverError(w, err)
		return
	}
	out.CanRequest, out.AutoApprove = can && (out.Movies || out.Shows), auto
	if out.CanRequest {
		out.QualityProfiles, out.DefaultQualityProfile = map[string][]arr.QualityProfile{}, map[string]int{}
		for kind, app := range map[string]arr.App{"movie": arr.Radarr, "show": arr.Sonarr} {
			if !conn[app] {
				continue
			}
			cl, c, err := s.arrClient(ctx, app)
			if err != nil {
				continue
			}
			// An app that's down only means no choice of quality for now.
			if p, err := s.qualityProfiles(ctx, cl); err == nil {
				out.QualityProfiles[kind] = p
			} else {
				s.Log.Warn("quality profiles", "app", app, "err", err)
			}
			out.DefaultQualityProfile[kind] = c.QualityProfileID
		}
	}
	if u.IsAdmin {
		if out.PendingRequests, out.OpenIssues, err = s.Store.Waiting(ctx); err != nil {
			s.serverError(w, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// ---- asking for titles ----

// Where a title stands.
const (
	titleAvailable = "available" // in a library, ready to watch
	titleAdded     = "added"     // Radarr or Sonarr has it, still to download
	titleRequested = "requested" // waiting for an admin
)

type titleView struct {
	Kind string `json:"kind"` // movie or show
	arr.Title
	Status    string `json:"status,omitempty"`
	ItemID    int64  `json:"itemId,omitempty"`    // when available
	RequestID string `json:"requestId,omitempty"` // when requested
}

// titleID is how a request names its title: TMDB for movies, TVDB for shows.
func titleID(kind string, tmdb, tvdb int) int {
	if kind == "show" {
		return tvdb
	}
	return tmdb
}

func (s *Server) requireRequester(w http.ResponseWriter, r *http.Request) (auto, ok bool) {
	can, auto, err := s.canRequest(r.Context(), currentUser(r))
	if err != nil {
		s.serverError(w, err)
		return false, false
	}
	if !can {
		writeError(w, http.StatusForbidden, "only admins add titles on this server")
		return false, false
	}
	return auto, true
}

func requestKind(w http.ResponseWriter, kind string) (arr.App, bool) {
	if kind != "movie" && kind != "show" {
		writeError(w, http.StatusBadRequest, "kind must be movie or show")
		return "", false
	}
	app, _ := appFor(kind)
	return app, true
}

func (s *Server) searchTitles(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireRequester(w, r); !ok {
		return
	}
	ctx, kind, q := r.Context(), r.URL.Query().Get("kind"), strings.TrimSpace(r.URL.Query().Get("q"))
	app, ok := requestKind(w, kind)
	if !ok {
		return
	}
	if q == "" {
		writeJSON(w, http.StatusOK, []titleView{})
		return
	}
	cl, _, err := s.arrClient(ctx, app)
	if err != nil {
		s.arrError(w, r, app, err)
		return
	}
	found, err := cl.Search(ctx, q)
	if err != nil {
		s.arrError(w, r, app, err)
		return
	}
	out := []titleView{}
	for _, t := range found {
		if titleID(kind, t.TMDBID, t.TVDBID) == 0 {
			continue
		}
		v, err := s.titleStatus(ctx, kind, t)
		if err != nil {
			s.serverError(w, err)
			return
		}
		out = append(out, v)
		if len(out) == 20 {
			break
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) titleStatus(ctx context.Context, kind string, t arr.Title) (titleView, error) {
	v := titleView{Kind: kind, Title: t}
	id, err := s.Store.LibraryMatch(ctx, kind, t.TMDBID, t.Title, t.Year)
	if err != nil || id != 0 {
		v.Status, v.ItemID = titleAvailable, id
		return v, err
	}
	if t.ID != 0 {
		v.Status = titleAdded
		return v, nil
	}
	req, err := s.Store.OpenRequest(ctx, kind, titleID(kind, t.TMDBID, t.TVDBID))
	if errors.Is(err, store.ErrNotFound) {
		return v, nil
	}
	if req.State == store.RequestPending {
		v.Status, v.RequestID = titleRequested, req.ID
	} else {
		v.Status = titleAdded
	}
	return v, err
}

type requestView struct {
	store.Request
	ItemID int64 `json:"itemId,omitempty"` // set once it's in a library
}

func (s *Server) viewRequest(ctx context.Context, req store.Request) (requestView, error) {
	v := requestView{Request: req}
	if req.State != store.RequestApproved {
		return v, nil
	}
	id, err := s.Store.LibraryMatch(ctx, req.Kind, req.TMDBID, req.Title, req.Year)
	v.ItemID = id
	return v, err
}

func (s *Server) createRequest(w http.ResponseWriter, r *http.Request) {
	auto, ok := s.requireRequester(w, r)
	if !ok {
		return
	}
	var in struct {
		Kind             string `json:"kind"`
		TMDBID           int    `json:"tmdbId"`
		TVDBID           int    `json:"tvdbId"`
		QualityProfileID int    `json:"qualityProfileId"`
	}
	if err := readJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	app, ok := requestKind(w, in.Kind)
	if !ok {
		return
	}
	id := titleID(in.Kind, in.TMDBID, in.TVDBID)
	if id <= 0 {
		writeError(w, http.StatusBadRequest, map[string]string{"movie": "tmdbId", "show": "tvdbId"}[in.Kind]+" is required")
		return
	}
	ctx := r.Context()
	cl, _, err := s.arrClient(ctx, app)
	if err != nil {
		s.arrError(w, r, app, err)
		return
	}
	// The title, as the app knows it, rather than as the client described it.
	t, err := cl.Lookup(ctx, id)
	if err != nil {
		s.arrError(w, r, app, err)
		return
	}
	st, err := s.titleStatus(ctx, in.Kind, t)
	if err != nil {
		s.serverError(w, err)
		return
	}
	switch st.Status {
	case titleAvailable:
		writeError(w, http.StatusConflict, t.Title+" is already in your library")
		return
	case titleAdded:
		writeError(w, http.StatusConflict, t.Title+" is already on its way")
		return
	case titleRequested:
		writeError(w, http.StatusConflict, "someone already asked for "+t.Title)
		return
	}
	if in.QualityProfileID != 0 {
		profiles, err := s.qualityProfiles(ctx, cl)
		if err != nil {
			s.arrError(w, r, app, err)
			return
		}
		if !slices.ContainsFunc(profiles, func(p arr.QualityProfile) bool { return p.ID == in.QualityProfileID }) {
			writeError(w, http.StatusBadRequest, "no such quality profile")
			return
		}
	}
	req, err := s.Store.CreateRequest(ctx, store.Request{
		Kind: in.Kind, TMDBID: t.TMDBID, TVDBID: t.TVDBID, Title: t.Title, Year: t.Year, Poster: t.Poster,
		QualityProfileID: in.QualityProfileID, UserID: currentUser(r).ID,
	})
	if err != nil {
		s.serverError(w, err)
		return
	}
	if auto {
		if req, err = s.approve(ctx, req, 0); err != nil {
			s.serverError(w, err)
			return
		}
	}
	v, err := s.viewRequest(ctx, req)
	if err != nil {
		s.serverError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, v)
}

// approve adds a requested title to Radarr or Sonarr. A failure to add it is
// recorded on the request rather than returned.
func (s *Server) approve(ctx context.Context, req store.Request, profile int) (store.Request, error) {
	app, _ := appFor(req.Kind)
	profile = cmp.Or(profile, req.QualityProfileID)
	cl, c, err := s.arrClient(ctx, app)
	if err == nil {
		_, err = cl.Add(ctx, titleID(req.Kind, req.TMDBID, req.TVDBID), arr.AddOptions{
			QualityProfileID: cmp.Or(profile, c.QualityProfileID), RootFolder: c.RootFolder,
		})
	}
	state, msg := store.RequestApproved, ""
	if err != nil {
		state, msg = store.RequestFailed, err.Error()
		if errors.Is(err, errNotConnected) {
			msg = app.Name() + " isn't connected"
		}
		s.Log.Warn("add requested title", "app", app, "title", req.Title, "err", err)
	} else {
		s.arrCache.clear() // it may be coming soon now
	}
	if err := s.Store.UpdateRequest(ctx, req.ID, state, msg, profile); err != nil {
		return req, err
	}
	return s.Store.Request(ctx, req.ID)
}

func (s *Server) listRequests(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	uid := u.ID
	if u.IsAdmin && r.URL.Query().Get("all") != "" {
		uid = 0
	}
	reqs, err := s.Store.Requests(r.Context(), uid, 200)
	if err != nil {
		s.serverError(w, err)
		return
	}
	out := make([]requestView, len(reqs))
	for i, req := range reqs {
		if out[i], err = s.viewRequest(r.Context(), req); err != nil {
			s.serverError(w, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) loadRequest(w http.ResponseWriter, r *http.Request) (store.Request, bool) {
	req, err := s.Store.Request(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "request not found")
		return req, false
	}
	if err != nil {
		s.serverError(w, err)
		return req, false
	}
	return req, true
}

func (s *Server) approveRequest(w http.ResponseWriter, r *http.Request) {
	req, ok := s.loadRequest(w, r)
	if !ok {
		return
	}
	var in struct {
		QualityProfileID int `json:"qualityProfileId"`
	}
	if err := readOptionalJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if req.State == store.RequestApproved {
		writeError(w, http.StatusConflict, "already approved")
		return
	}
	req, err := s.approve(r.Context(), req, in.QualityProfileID)
	if err != nil {
		s.serverError(w, err)
		return
	}
	v, err := s.viewRequest(r.Context(), req)
	if err != nil {
		s.serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) declineRequest(w http.ResponseWriter, r *http.Request) {
	req, ok := s.loadRequest(w, r)
	if !ok {
		return
	}
	if req.State == store.RequestApproved {
		writeError(w, http.StatusConflict, "already approved")
		return
	}
	if err := s.Store.UpdateRequest(r.Context(), req.ID, store.RequestDeclined, "", req.QualityProfileID); err != nil {
		s.serverError(w, err)
		return
	}
	req.State = store.RequestDeclined
	writeJSON(w, http.StatusOK, requestView{Request: req})
}

// deleteRequest takes a request off the list. A title already added stays
// in Radarr or Sonarr.
func (s *Server) deleteRequest(w http.ResponseWriter, r *http.Request) {
	req, ok := s.loadRequest(w, r)
	if !ok {
		return
	}
	if u := currentUser(r); !u.IsAdmin && req.UserID != u.ID {
		writeError(w, http.StatusNotFound, "request not found")
		return
	}
	if err := s.Store.DeleteRequest(r.Context(), req.ID); err != nil {
		s.serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- coming soon ----

type upcomingView struct {
	Items []arr.Upcoming `json:"items"`
	// Apps that didn't answer, so the list may be missing some.
	Unavailable []string `json:"unavailable,omitempty"`
}

func (s *Server) upcoming(w http.ResponseWriter, r *http.Request) {
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	days = min(max(cmp.Or(days, 30), 1), 90)
	ctx := r.Context()
	// From a few hours back, so what aired earlier today but isn't in yet still shows.
	from := time.Now().Add(-6 * time.Hour).Truncate(time.Hour)
	to := from.Add(time.Duration(days) * 24 * time.Hour)

	out := upcomingView{Items: []arr.Upcoming{}}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, app := range arr.Apps {
		cl, _, err := s.arrClient(ctx, app)
		if errors.Is(err, errNotConnected) {
			continue
		}
		if err != nil {
			s.serverError(w, err)
			return
		}
		wg.Go(func() {
			items, err := cached(&s.arrCache, "upcoming:"+string(app)+":"+strconv.Itoa(days), 10*time.Minute, func() ([]arr.Upcoming, error) {
				return cl.Calendar(ctx, from, to)
			})
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				s.Log.Warn("coming soon", "app", app, "err", err)
				out.Unavailable = append(out.Unavailable, app.Name())
				return
			}
			out.Items = append(out.Items, items...)
		})
	}
	wg.Wait()
	slices.SortStableFunc(out.Items, func(a, b arr.Upcoming) int {
		return cmp.Or(a.Date.Compare(b.Date), strings.Compare(a.ShowTitle+a.Title, b.ShowTitle+b.Title))
	})
	writeJSON(w, http.StatusOK, out)
}

// ---- a title in the libraries, as Radarr or Sonarr has it ----

type managed struct {
	app             arr.App
	client          *arr.Client
	title           arr.Title
	season, episode int // for episodes
}

// managedItem finds the movie or show an item belongs to in Radarr or Sonarr.
func (s *Server) managedItem(ctx context.Context, it store.Item) (managed, error) {
	m := managed{}
	app, ok := appFor(it.Kind)
	if !ok {
		return m, arr.ErrNotFound
	}
	m.app = app
	cl, _, err := s.arrClient(ctx, app)
	if err != nil {
		return m, err
	}
	m.client = cl
	// Climb to the show, noting the season and episode numbers on the way.
	for it.Kind != "movie" && it.Kind != "show" {
		switch it.Kind {
		case "episode":
			m.episode = deref(it.IndexNumber)
		case "season":
			m.season = deref(it.IndexNumber)
		}
		if it.ParentID == nil {
			return m, arr.ErrNotFound
		}
		if it, err = s.Store.Item(ctx, 0, *it.ParentID); err != nil {
			return m, err
		}
	}
	var tmdb int
	if it.TMDBID != nil {
		tmdb = int(*it.TMDBID)
	}
	m.title, err = cl.Find(ctx, tmdb, it.Title, deref(it.Year))
	return m, err
}

func deref(n *int) int {
	if n == nil {
		return 0
	}
	return *n
}

// replaceable says whether a bad file can be swapped for another copy:
// Radarr and Sonarr replace single files, so movies and episodes.
func replaceable(kind string) bool { return kind == "movie" || kind == "episode" }

type qualityView struct {
	App              arr.App              `json:"app"`
	QualityProfileID int                  `json:"qualityProfileId"`
	QualityProfiles  []arr.QualityProfile `json:"qualityProfiles"`
}

func (s *Server) getItemQuality(w http.ResponseWriter, r *http.Request) {
	it, ok := s.loadItem(w, r)
	if !ok {
		return
	}
	m, err := s.managedItem(r.Context(), it)
	if err != nil {
		s.arrError(w, r, m.app, err)
		return
	}
	profiles, err := s.qualityProfiles(r.Context(), m.client)
	if err != nil {
		s.arrError(w, r, m.app, err)
		return
	}
	writeJSON(w, http.StatusOK, qualityView{App: m.app, QualityProfileID: m.title.QualityProfileID, QualityProfiles: profiles})
}

// putItemQuality changes the quality profile of a movie, or of the show an
// item belongs to, and by default searches for copies that match it.
func (s *Server) putItemQuality(w http.ResponseWriter, r *http.Request) {
	it, ok := s.loadItem(w, r)
	if !ok {
		return
	}
	in := struct {
		QualityProfileID int   `json:"qualityProfileId"`
		Search           *bool `json:"search"`
	}{}
	if err := readJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	m, err := s.managedItem(r.Context(), it)
	if err != nil {
		s.arrError(w, r, m.app, err)
		return
	}
	profiles, err := s.qualityProfiles(r.Context(), m.client)
	if err != nil {
		s.arrError(w, r, m.app, err)
		return
	}
	if !slices.ContainsFunc(profiles, func(p arr.QualityProfile) bool { return p.ID == in.QualityProfileID }) {
		writeError(w, http.StatusBadRequest, "no such quality profile")
		return
	}
	if err := m.client.SetQualityProfile(r.Context(), m.title.ID, in.QualityProfileID, in.Search == nil || *in.Search); err != nil {
		s.arrError(w, r, m.app, err)
		return
	}
	writeJSON(w, http.StatusOK, qualityView{App: m.app, QualityProfileID: in.QualityProfileID, QualityProfiles: profiles})
}

// ---- problems people report ----

type issueView struct {
	store.Issue
	Item *store.Item `json:"item,omitempty"`
	// Replaceable says an admin can have Radarr or Sonarr fetch another copy.
	Replaceable bool `json:"replaceable"`
}

func (s *Server) reportIssue(w http.ResponseWriter, r *http.Request) {
	it, ok := s.loadItem(w, r)
	if !ok {
		return
	}
	var in struct {
		Kind string `json:"kind"`
		Note string `json:"note"`
	}
	if err := readJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if !slices.Contains(store.IssueKinds, in.Kind) {
		writeError(w, http.StatusBadRequest, "kind must be one of: "+strings.Join(store.IssueKinds, ", "))
		return
	}
	x, err := s.Store.CreateIssue(r.Context(), store.Issue{ItemID: it.ID, Kind: in.Kind, Note: clip(in.Note, 1000), UserID: currentUser(r).ID})
	if err != nil {
		s.serverError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, issueView{Issue: x, Item: &it})
}

func (s *Server) listIssues(w http.ResponseWriter, r *http.Request) {
	ctx, u := r.Context(), currentUser(r)
	uid := u.ID
	if u.IsAdmin && r.URL.Query().Get("all") != "" {
		uid = 0
	}
	issues, err := s.Store.Issues(ctx, uid, 200)
	if err != nil {
		s.serverError(w, err)
		return
	}
	conn, err := s.connected(ctx)
	if err != nil {
		s.serverError(w, err)
		return
	}
	out := make([]issueView, len(issues))
	for i, x := range issues {
		out[i].Issue = x
		it, err := s.Store.Item(ctx, u.ID, x.ItemID)
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			s.serverError(w, err)
			return
		}
		out[i].Item = &it
		app, _ := appFor(it.Kind)
		out[i].Replaceable = u.IsAdmin && !x.Resolved && replaceable(it.Kind) && conn[app]
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) loadIssue(w http.ResponseWriter, r *http.Request) (store.Issue, bool) {
	x, err := s.Store.Issue(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "issue not found")
		return x, false
	}
	if err != nil {
		s.serverError(w, err)
		return x, false
	}
	return x, true
}

func (s *Server) resolveIssue(w http.ResponseWriter, r *http.Request) {
	x, ok := s.loadIssue(w, r)
	if !ok {
		return
	}
	if err := s.Store.ResolveIssue(r.Context(), x.ID, ""); err != nil {
		s.serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// replaceIssue deletes the reported file in Radarr or Sonarr and has it
// search for another copy, which the next scan picks up.
func (s *Server) replaceIssue(w http.ResponseWriter, r *http.Request) {
	x, ok := s.loadIssue(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	it, err := s.Store.Item(ctx, 0, x.ItemID)
	if err != nil {
		s.serverError(w, err)
		return
	}
	if !replaceable(it.Kind) {
		writeError(w, http.StatusBadRequest, "only a movie's or an episode's file can be replaced")
		return
	}
	m, err := s.managedItem(ctx, it)
	if err == nil {
		if it.Kind == "movie" {
			err = m.client.ReplaceMovie(ctx, m.title)
		} else {
			err = m.client.ReplaceEpisode(ctx, m.title.ID, m.season, m.episode)
		}
	}
	if err != nil {
		s.arrError(w, r, m.app, err)
		return
	}
	if err := s.Store.ResolveIssue(ctx, x.ID, "replaced"); err != nil {
		s.serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
