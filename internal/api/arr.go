package api

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"regexp"
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

func settingKey(app arr.App) string { return arr.SettingKey(app) }

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
		writeErrorWith(w, http.StatusConflict, "{{app}} isn't connected", map[string]string{"app": app.Name()})
	case errors.Is(err, arr.ErrNotFound):
		writeErrorWith(w, http.StatusNotFound, "{{app}} doesn't have this title", map[string]string{"app": app.Name()})
	case errors.As(err, &ae):
		s.Log.Warn("arr", "app", app, "err", err)
		if !currentUser(r).IsAdmin {
			writeErrorWith(w, http.StatusBadGateway, "{{app}} isn't answering right now. Try again later.", map[string]string{"app": app.Name()})
			return
		}
		writeError(w, http.StatusBadGateway, ae.Message)
	default:
		s.serverError(w, err)
	}
}

// ttlCache keeps answers from other services (Radarr and Sonarr, Box
// Office, TMDB, Overseerr and the like) for a while, so pages that show
// them don't each wait on those services, and so a service that's slow or
// down never holds a page up (see cached). Changing a connection clears it.
type ttlCache struct {
	mu      sync.Mutex
	entries map[string]cacheEntry
	loading map[string]chan struct{} // keys being fetched, closed when done
	errs    map[string]error         // the last fetch's error, for whoever waited on it
}

type cacheEntry struct {
	expires time.Time
	value   any
}

// How long a page waits for an answer it doesn't have yet, how long the
// fetch itself may take, and how long past its ttl an answer may still be
// shown while a fresh one is fetched. Variables so tests can shorten them.
var (
	cacheWait  = 3 * time.Second
	cacheFetch = 30 * time.Second
	cacheStale = 24 * time.Hour
)

// errSlow is an answer that didn't come within cacheWait. It's still
// being fetched, and the next page that asks gets it.
var errSlow = errors.New("still waiting for an answer")

// cached answers from c when it can, so a page never waits long on
// another service. A fresh answer comes back as it is. A stale one (up to
// cacheStale past its ttl) comes back at once while a fresh one is fetched
// in the background. Without one, cached waits up to cacheWait for load and
// then returns errSlow, letting load finish in the background so the next
// page has the answer. Only one load per key runs at a time. load gets a
// context of its own, ended after cacheFetch, so it isn't cut short when
// the page that started it is done. Errors aren't kept.
func cached[T any](c *ttlCache, key string, ttl time.Duration, load func(ctx context.Context) (T, error)) (T, error) {
	return cachedWithin(c, key, ttl, cacheWait, load)
}

// cachedWithin is cached for an answer someone asked for and is watching
// for, such as a search they typed, which may wait up to wait for it.
func cachedWithin[T any](c *ttlCache, key string, ttl, wait time.Duration, load func(ctx context.Context) (T, error)) (T, error) {
	var zero T
	c.mu.Lock()
	e, have := c.entries[key]
	now := time.Now()
	if have && now.Before(e.expires) {
		c.mu.Unlock()
		return e.value.(T), nil
	}
	if have && now.After(e.expires.Add(cacheStale)) {
		have = false
	}
	done, fetching := c.loading[key]
	if !fetching {
		if c.loading == nil {
			c.loading = map[string]chan struct{}{}
		}
		done = make(chan struct{})
		c.loading[key] = done
		go c.fetch(key, ttl, done, func(ctx context.Context) (any, error) { return load(ctx) })
	}
	c.mu.Unlock()
	if have {
		return e.value.(T), nil
	}
	t := time.NewTimer(wait)
	defer t.Stop()
	select {
	case <-done:
	case <-t.C:
		return zero, errSlow
	}
	c.mu.Lock()
	e, have = c.entries[key]
	err := c.lastErr(key)
	c.mu.Unlock()
	if have && e.value != nil {
		if v, ok := e.value.(T); ok {
			return v, nil
		}
	}
	if err == nil {
		err = errSlow
	}
	return zero, err
}

// fetch runs one load for key and keeps what it answers.
func (c *ttlCache) fetch(key string, ttl time.Duration, done chan struct{}, load func(ctx context.Context) (any, error)) {
	ctx, cancel := context.WithTimeout(context.Background(), cacheFetch)
	defer cancel()
	v, err := load(ctx)
	c.mu.Lock()
	if err == nil {
		if c.entries == nil {
			c.entries = map[string]cacheEntry{}
		}
		c.entries[key] = cacheEntry{time.Now().Add(ttl), v}
		delete(c.errs, key)
	} else {
		if c.errs == nil {
			c.errs = map[string]error{}
		}
		c.errs[key] = err
	}
	delete(c.loading, key)
	c.mu.Unlock()
	close(done)
}

func (c *ttlCache) lastErr(key string) error { return c.errs[key] }

func (c *ttlCache) clear() {
	c.mu.Lock()
	c.entries, c.errs = nil, nil
	c.mu.Unlock()
}

func (s *Server) qualityProfiles(ctx context.Context, cl *arr.Client) ([]arr.QualityProfile, error) {
	return cached(&s.arrCache, "profiles:"+string(cl.App), 5*time.Minute, func(ctx context.Context) ([]arr.QualityProfile, error) {
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
			return in, badRequest("enter the API key (under Settings → General)")
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
		writeErrorWith(w, http.StatusBadRequest, "{{app}} has no such quality profile", map[string]string{"app": app.Name()})
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
	s.boxOffices.forget(app)
	s.arrChanged()
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
	s.boxOffices.forget(app)
	s.arrChanged()
	w.WriteHeader(http.StatusNoContent)
}

// ---- what people can do ----

type requestOptions struct {
	Movies bool `json:"movies"` // Radarr is connected
	Shows  bool `json:"shows"`  // Sonarr is connected
	// Discover says Overseerr or Box Office is connected, for the Discover
	// tab (see discover.go).
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
	// BoxOffice is what Box Office offers here beyond Radarr and Sonarr
	// (see boxoffice_pairing.go); nil without it.
	BoxOffice *pairingView `json:"boxOffice,omitempty"`
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
	_, bo := s.boxOfficeDiscovery(ctx)
	out.Discover = oc.URL != "" || bo
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
	if !u.Guest {
		if out.BoxOffice, err = s.pairing(ctx, u); err != nil {
			s.serverError(w, err)
			return
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
	req, ok := s.requestTitle(w, r, auto, in.Kind, cl, t, in.QualityProfileID)
	if !ok {
		return
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
	if req.BoxOfficeID != 0 {
		return s.approveWithBoxOffice(ctx, req, profile)
	}
	var err error
	if req.Ask != "" {
		// Another edition, or a better or replacement copy, from Box Office.
		err = s.sendAsk(ctx, req)
	} else {
		var cl *arr.Client
		var c arr.Config
		cl, c, err = s.arrClient(ctx, app)
		if err == nil {
			_, err = cl.Add(ctx, titleID(req.Kind, req.TMDBID, req.TVDBID), arr.AddOptions{
				QualityProfileID: cmp.Or(profile, c.QualityProfileID), RootFolder: c.RootFolder,
			})
		}
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
	if req.BoxOfficeID != 0 && req.State == store.RequestPending {
		s.declineWithBoxOffice(w, r, req)
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
	s.withdrawFromBoxOffice(r.Context(), req)
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
			items, err := cached(&s.arrCache, "upcoming:"+string(app)+":"+strconv.Itoa(days), 10*time.Minute, func(ctx context.Context) ([]arr.Upcoming, error) {
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
	// Fix is how Box Office fixing it by itself is going, when it was
	// asked to.
	Fix *issueFix `json:"fix,omitempty"`
}

// issueFix is where Box Office's fix for a reported problem stands.
type issueFix struct {
	// State is waiting (for an admin to approve), fixing (Box Office is
	// getting another copy), failed (asking Box Office failed; Error says
	// why) or fixed (another copy is in).
	State string `json:"state"`
	Error string `json:"error,omitempty"`
}

var reLanguage = regexp.MustCompile(`^[a-z]{2}$`)

func (s *Server) reportIssue(w http.ResponseWriter, r *http.Request) {
	it, ok := s.loadItem(w, r)
	if !ok {
		return
	}
	var in struct {
		Kind     string `json:"kind"`
		Note     string `json:"note"`
		Language string `json:"language"`
	}
	if err := readJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if !slices.Contains(store.IssueKinds, in.Kind) {
		writeError(w, http.StatusBadRequest, "kind must be one of: "+strings.Join(store.IssueKinds, ", "))
		return
	}
	in.Language = strings.ToLower(strings.TrimSpace(in.Language))
	if in.Kind != "language" {
		in.Language = ""
	} else if !reLanguage.MatchString(in.Language) {
		writeError(w, http.StatusBadRequest, "say which language it should be in")
		return
	}
	ctx, u := r.Context(), currentUser(r)
	key, err := s.Store.FileKey(ctx, it.ID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		s.serverError(w, err)
		return
	}
	x, err := s.Store.CreateIssue(ctx, store.Issue{ItemID: it.ID, Kind: in.Kind, Note: clip(in.Note, 1000), Language: in.Language, UserID: u.ID, FileKey: key})
	if err != nil {
		s.serverError(w, err)
		return
	}
	if err := s.fixIssue(ctx, u, it, &x); err != nil {
		s.serverError(w, err)
		return
	}
	v := issueView{Issue: x, Item: &it}
	if v.Fix, err = s.issueFixState(ctx, x); err != nil {
		s.serverError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, v)
}

// unreadableNote is the note on the problem Matinee reports itself for a
// video it gave up reading.
const unreadableNote = "Reported by Matinee: reading this file keeps timing out while its share answers, so it's most likely damaged (for example, a download whose end was never written)."

// ReportUnreadable reports a problem with a video the background jobs gave
// up reading as damaged, when a Box Office that fixes problems is
// connected, so Box Office gets another copy by itself. It's filed once,
// in the name of the first admin, and not while the video has a problem
// reported already.
func (s *Server) ReportUnreadable(ctx context.Context, itemID int64) {
	it, err := s.Store.Item(ctx, 0, itemID)
	if err != nil {
		return
	}
	app, ok := appFor(it.Kind)
	if !ok || !replaceable(it.Kind) || !s.boxOffices.offers(app, featureProblems) {
		return
	}
	if open, err := s.Store.OpenIssue(ctx, itemID); err != nil || open {
		return
	}
	users, err := s.Store.Users(ctx)
	if err != nil {
		return
	}
	i := slices.IndexFunc(users, func(u store.User) bool { return u.IsAdmin })
	if i < 0 {
		return
	}
	admin := users[i]
	key, err := s.Store.FileKey(ctx, it.ID)
	if err != nil {
		return
	}
	x, err := s.Store.CreateIssue(ctx, store.Issue{ItemID: it.ID, Kind: "video", Note: unreadableNote, UserID: admin.ID, FileKey: key})
	if err != nil {
		s.Log.Warn("report a damaged video", "item", it.ID, "err", err)
		return
	}
	if err := s.fixIssue(ctx, admin, it, &x); err != nil {
		s.Log.Warn("report a damaged video", "item", it.ID, "err", err)
		return
	}
	s.Log.Info("reported a damaged video to Box Office", "item", it.ID, "title", it.Title, "path", it.Path)
}

// fixIssue asks Box Office to fix a reported problem by itself, when it
// can: a problem with a movie's or an episode's copy (store.FixableIssues)
// that Box Office has, with a Box Office that fixes problems. It blocks
// the bad copy's release and gets another. The fix waits for an admin
// when Settings says what's asked of Box Office does. Otherwise the
// issue waits for an admin, as before.
func (s *Server) fixIssue(ctx context.Context, u store.User, it store.Item, x *store.Issue) error {
	app, ok := appFor(it.Kind)
	if !ok || !replaceable(it.Kind) || !slices.Contains(store.FixableIssues, x.Kind) || !s.boxOffices.offers(app, featureProblems) {
		return nil
	}
	m, err := s.managedItem(ctx, it)
	if err != nil {
		// Not one Box Office has: an admin will see to it.
		s.Log.Info("a reported problem waits for an admin: Box Office doesn't have the title", "item", it.ID, "err", err)
		return nil
	}
	req := store.Request{Ask: store.AskReplace, Problem: x.Kind, Language: x.Language, Reason: x.Note,
		UserID: u.ID, Title: m.title.Title, Year: m.title.Year, Poster: m.title.Poster}
	if it.Path != "" {
		req.FileName = filepath.Base(it.Path)
	}
	if m.app == arr.Radarr {
		req.Kind, req.TMDBID = "movie", m.title.TMDBID
	} else {
		req.Kind, req.TMDBID, req.TVDBID, req.Season, req.Episode = "show", m.title.TMDBID, m.title.TVDBID, m.season, m.episode
		if req.TVDBID == 0 || req.Episode == 0 {
			return nil
		}
	}
	need, err := s.reportsNeedApproval(ctx, u)
	if err != nil {
		return err
	}
	saved, err := s.Store.CreateRequest(ctx, req)
	if err != nil {
		return err
	}
	if err := s.Store.SetIssueRequest(ctx, x.ID, saved.ID); err != nil {
		return err
	}
	x.RequestID = saved.ID
	if !need {
		_, err = s.approve(ctx, saved, 0)
	}
	return err
}

// issueFixState is where Box Office's fix for an issue stands, or nil
// when it wasn't asked to fix it.
func (s *Server) issueFixState(ctx context.Context, x store.Issue) (*issueFix, error) {
	if x.RequestID == "" {
		return nil, nil
	}
	if x.Resolution == store.IssueReplaced {
		return &issueFix{State: "fixed"}, nil
	}
	req, err := s.Store.Request(ctx, x.RequestID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	switch req.State {
	case store.RequestPending:
		return &issueFix{State: "waiting"}, nil
	case store.RequestApproved:
		return &issueFix{State: "fixing"}, nil
	case store.RequestFailed:
		return &issueFix{State: "failed", Error: req.Error}, nil
	}
	return nil, nil // declined: an admin sees to it
}

// settleIssues closes the problems Box Office was asked to fix once
// another copy has taken the reported one's place, and tells whoever
// reported each. A scan keeps an item when its file is replaced, so the
// item's file changing is the sign.
func (s *Server) settleIssues(ctx context.Context) {
	issues, err := s.Store.FixingIssues(ctx)
	if err != nil {
		s.Log.Warn("couldn't list the problems Box Office is fixing", "err", err)
		return
	}
	for _, x := range issues {
		if x.FileKey == "" {
			continue
		}
		key, err := s.Store.FileKey(ctx, x.ItemID)
		if err != nil || key == "" || key == x.FileKey {
			continue
		}
		if err := s.Store.ResolveIssue(ctx, x.ID, store.IssueReplaced); err != nil {
			s.Log.Warn("couldn't close a fixed problem", "issue", x.ID, "err", err)
			continue
		}
		s.tellFixed(ctx, x)
	}
}

// tellFixed tells the person who reported a problem that another copy is
// in.
func (s *Server) tellFixed(ctx context.Context, x store.Issue) {
	it, err := s.Store.Item(ctx, 0, x.ItemID)
	if err != nil {
		return
	}
	req, _ := s.Store.Request(ctx, x.RequestID)
	title := it.Title
	if req.Title != "" && it.Kind == "episode" {
		title = fmt.Sprintf("%s S%02dE%02d", req.Title, req.Season, req.Episode)
	}
	n := store.Notification{
		UserID: x.UserID, Source: "issue:" + x.ID + ":fixed", Event: "issue.fixed", Level: store.NotificationInfo,
		Title: "Another copy of " + title + " is in", Body: "The problem you reported is fixed. Box Office got another copy.",
		Kind: req.Kind, TMDBID: req.TMDBID, TVDBID: req.TVDBID, Poster: req.Poster, CreatedAt: time.Now().Unix(),
	}
	saved, added, err := s.Store.AddNotification(ctx, n)
	if err != nil {
		s.Log.Warn("couldn't tell someone their problem is fixed", "issue", x.ID, "err", err)
		return
	}
	if added == store.AddedNew {
		s.pushNotification(ctx, x.UserID, s.notificationView(ctx, saved))
	}
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
		if out[i].Fix, err = s.issueFixState(ctx, x); err != nil {
			s.serverError(w, err)
			return
		}
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
	if err := s.Store.ResolveIssue(ctx, x.ID, store.IssueReplaced); err != nil {
		s.serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// requestTitle asks for a title the app looked up, unless it's in a
// library, on its way or asked for already, and approves it straight away
// when auto. A failure is answered on w.
func (s *Server) requestTitle(w http.ResponseWriter, r *http.Request, auto bool, kind string, cl *arr.Client, t arr.Title, profileID int) (store.Request, bool) {
	ctx := r.Context()
	app, _ := appFor(kind)
	st, err := s.titleStatus(ctx, kind, t)
	if err != nil {
		s.serverError(w, err)
		return store.Request{}, false
	}
	switch st.Status {
	case titleAvailable:
		writeErrorWith(w, http.StatusConflict, "{{title}} is already in your library", map[string]string{"title": t.Title})
		return store.Request{}, false
	case titleAdded:
		writeErrorWith(w, http.StatusConflict, "{{title}} is already on its way", map[string]string{"title": t.Title})
		return store.Request{}, false
	case titleRequested:
		writeErrorWith(w, http.StatusConflict, "someone already asked for {{title}}", map[string]string{"title": t.Title})
		return store.Request{}, false
	}
	if profileID != 0 {
		profiles, err := s.qualityProfiles(ctx, cl)
		if err != nil {
			s.arrError(w, r, app, err)
			return store.Request{}, false
		}
		if !slices.ContainsFunc(profiles, func(p arr.QualityProfile) bool { return p.ID == profileID }) {
			writeError(w, http.StatusBadRequest, "no such quality profile")
			return store.Request{}, false
		}
	}
	if desk, ok := s.requestDesk(ctx, kind); ok && t.TMDBID != 0 {
		return s.fileWithBoxOffice(w, r, auto, desk, kind, t, profileID)
	}
	req, err := s.Store.CreateRequest(ctx, store.Request{
		Kind: kind, TMDBID: t.TMDBID, TVDBID: t.TVDBID, Title: t.Title, Year: t.Year, Poster: t.Poster,
		QualityProfileID: profileID, UserID: currentUser(r).ID,
	})
	if err != nil {
		s.serverError(w, err)
		return store.Request{}, false
	}
	if auto {
		if req, err = s.approve(ctx, req, 0); err != nil {
			s.serverError(w, err)
			return store.Request{}, false
		}
	}
	return req, true
}
