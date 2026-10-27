package api

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/yeixio/matinee/internal/overseerr"
	"github.com/yeixio/matinee/internal/store"
)

// Overseerr, connected by an admin, fills the Discover tab: what's trending
// and popular, titles like the ones someone watched lately, and a way to
// ask for any of them. Without it there's no Discover tab.
//
// Requests go to Overseerr as the Overseerr user with the same name as the
// person asking (or their account, for a profile), so Overseerr's own
// permissions decide what needs approval. People without an Overseerr
// account ask as the user the admin picked (requestAs). Who may ask at all
// follows the server's request setting, as for Radarr and Sonarr.
//
// The address and API key stay on the server: clients only learn whether
// Overseerr is connected.

const settingOverseerr = "overseerr"

func (s *Server) overseerrConfig(ctx context.Context) (overseerr.Config, error) {
	var c overseerr.Config
	v, err := s.Store.Setting(ctx, settingOverseerr)
	if err != nil || v == "" {
		return c, err
	}
	return c, json.Unmarshal([]byte(v), &c)
}

func (s *Server) overseerrClient(ctx context.Context) (*overseerr.Client, overseerr.Config, error) {
	c, err := s.overseerrConfig(ctx)
	if err != nil {
		return nil, c, err
	}
	if c.URL == "" {
		return nil, c, errNotConnected
	}
	cl, err := overseerr.New(c)
	return cl, c, err
}

// overseerrError answers for a failed call to Overseerr. Only admins see
// the details, which can include its address.
func (s *Server) overseerrError(w http.ResponseWriter, r *http.Request, err error) {
	var oe *overseerr.Error
	var bad badRequest
	switch {
	case errors.As(err, &bad):
		writeError(w, http.StatusBadRequest, bad.Error())
	case errors.Is(err, errNotConnected):
		writeError(w, http.StatusConflict, "Overseerr isn't connected")
	case errors.Is(err, overseerr.ErrNotFound):
		writeError(w, http.StatusNotFound, "Overseerr doesn't know this title")
	case errors.As(err, &oe):
		s.Log.Warn("overseerr", "err", err)
		msg := oe.Message
		if !currentUser(r).IsAdmin && oe.Status != http.StatusConflict {
			msg = "Overseerr isn't answering right now. Try again later."
		}
		writeError(w, http.StatusBadGateway, msg)
	default:
		s.serverError(w, err)
	}
}

// ---- connecting (admins) ----

type overseerrView struct {
	Connected bool   `json:"connected"`
	URL       string `json:"url"`
	HasAPIKey bool   `json:"hasApiKey"`
	RequestAs int    `json:"requestAs"`
}

func viewOverseerr(c overseerr.Config) overseerrView {
	return overseerrView{Connected: c.URL != "", URL: c.URL, HasAPIKey: c.APIKey != "", RequestAs: c.RequestAs}
}

func (s *Server) getOverseerr(w http.ResponseWriter, r *http.Request) {
	c, err := s.overseerrConfig(r.Context())
	if err != nil {
		s.serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, viewOverseerr(c))
}

// overseerrCandidate is the connection an admin is trying. Leaving the API
// key out keeps the saved one, but only for the saved address.
func (s *Server) overseerrCandidate(ctx context.Context, in overseerr.Config) (overseerr.Config, error) {
	saved, err := s.overseerrConfig(ctx)
	if err != nil {
		return in, err
	}
	if in.URL == "" {
		in.URL = saved.URL
	}
	cl, err := overseerr.New(overseerr.Config{URL: in.URL, APIKey: "-"})
	if err != nil {
		return in, badRequest(err.Error())
	}
	in.URL, in.APIKey = cl.URL(), strings.TrimSpace(in.APIKey)
	if in.APIKey == "" {
		if saved.APIKey == "" || saved.URL != in.URL {
			return in, badRequest("enter the API key (in Overseerr under Settings → General)")
		}
		in.APIKey = saved.APIKey
	}
	return in, nil
}

type overseerrUser struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type overseerrCheck struct {
	URL     string `json:"url"`
	Version string `json:"version"`
	// Owner is the API key's user, who requests are made as by default.
	Owner overseerrUser   `json:"owner"`
	Users []overseerrUser `json:"users"`
}

func checkOverseerr(ctx context.Context, c overseerr.Config) (overseerrCheck, error) {
	out := overseerrCheck{Users: []overseerrUser{}}
	cl, err := overseerr.New(c)
	if err != nil {
		return out, badRequest(err.Error())
	}
	st, owner, err := cl.Status(ctx)
	if err != nil {
		return out, err
	}
	out.URL, out.Version, out.Owner = cl.URL(), st.Version, overseerrUser{owner.ID, owner.Name()}
	users, err := cl.Users(ctx)
	for _, u := range users {
		out.Users = append(out.Users, overseerrUser{u.ID, u.Name()})
	}
	return out, err
}

// testOverseerr checks an address and key without saving them, and lists
// Overseerr's users to pick who requests are made as.
func (s *Server) testOverseerr(w http.ResponseWriter, r *http.Request) {
	var in overseerr.Config
	if err := readOptionalJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	c, err := s.overseerrCandidate(r.Context(), in)
	if err != nil {
		s.overseerrError(w, r, err)
		return
	}
	res, err := checkOverseerr(r.Context(), c)
	if err != nil {
		s.overseerrError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) putOverseerr(w http.ResponseWriter, r *http.Request) {
	var in overseerr.Config
	if err := readJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	c, err := s.overseerrCandidate(r.Context(), in)
	if err != nil {
		s.overseerrError(w, r, err)
		return
	}
	res, err := checkOverseerr(r.Context(), c)
	if err != nil {
		s.overseerrError(w, r, err)
		return
	}
	if c.RequestAs == res.Owner.ID {
		c.RequestAs = 0
	}
	if c.RequestAs != 0 && !slices.ContainsFunc(res.Users, func(u overseerrUser) bool { return u.ID == c.RequestAs }) {
		writeError(w, http.StatusBadRequest, "Overseerr has no such user")
		return
	}
	b, _ := json.Marshal(c)
	if err := s.Store.SetSetting(r.Context(), settingOverseerr, string(b)); err != nil {
		s.serverError(w, err)
		return
	}
	s.discoverCache.clear()
	writeJSON(w, http.StatusOK, viewOverseerr(c))
}

func (s *Server) deleteOverseerr(w http.ResponseWriter, r *http.Request) {
	if err := s.Store.SetSetting(r.Context(), settingOverseerr, ""); err != nil {
		s.serverError(w, err)
		return
	}
	s.discoverCache.clear()
	w.WriteHeader(http.StatusNoContent)
}

// ---- discovering ----

type discoverTitle struct {
	overseerr.Title
	// Status is available (in a library, see ItemID), added (on its way),
	// requested (waiting for approval in Overseerr), or empty.
	Status string `json:"status,omitempty"`
	ItemID int64  `json:"itemId,omitempty"`
}

type discoverRow struct {
	ID    string          `json:"id"`
	Title string          `json:"title"`
	Items []discoverTitle `json:"items"`
}

type discoverView struct {
	Rows []discoverRow `json:"rows"`
	// CanRequest says this account may ask for titles.
	CanRequest bool `json:"canRequest"`
}

func (s *Server) discoverStatus(ctx context.Context, t overseerr.Title) (discoverTitle, error) {
	v := discoverTitle{Title: t}
	id, err := s.Store.LibraryMatch(ctx, t.Kind, t.TMDBID, t.Title, t.Year)
	if err != nil || id != 0 {
		v.Status, v.ItemID = titleAvailable, id
		return v, err
	}
	switch t.MediaStatus {
	case overseerr.StatusPending:
		v.Status = titleRequested
	case overseerr.StatusProcessing, overseerr.StatusPartial, overseerr.StatusAvailable:
		v.Status = titleAdded
	}
	return v, nil
}

// discoverTitles adds where each title stands. dropOwned leaves out the
// ones already in a library.
func (s *Server) discoverTitles(ctx context.Context, titles []overseerr.Title, dropOwned bool) ([]discoverTitle, error) {
	out := []discoverTitle{}
	for _, t := range titles {
		v, err := s.discoverStatus(ctx, t)
		if err != nil {
			return nil, err
		}
		if dropOwned && v.Status == titleAvailable {
			continue
		}
		out = append(out, v)
	}
	return out, nil
}

// The rows of the Discover tab, after the ones based on what someone watched.
var discoverLists = []struct{ id, title string }{
	{"trending", "Trending"},
	{"popular-movies", "Popular movies"},
	{"popular-shows", "Popular shows"},
	{"upcoming-movies", "Coming to cinemas"},
	{"upcoming-shows", "New shows coming up"},
}

// becauseRows is how many recently watched titles get a row of their own.
const becauseRows = 2

func (s *Server) discover(w http.ResponseWriter, r *http.Request) {
	ctx, u := r.Context(), currentUser(r)
	cl, _, err := s.overseerrClient(ctx)
	if err != nil {
		s.overseerrError(w, r, err)
		return
	}
	can, _, err := s.canRequest(ctx, u)
	if err != nil {
		s.serverError(w, err)
		return
	}
	watched, err := s.Store.RecentlyWatched(ctx, u.ID, becauseRows)
	if err != nil {
		s.serverError(w, err)
		return
	}

	type job struct {
		row       discoverRow
		dropOwned bool
		load      func() ([]overseerr.Title, error)
	}
	var jobs []job
	for _, t := range watched {
		jobs = append(jobs, job{
			row:       discoverRow{ID: "because-" + strconv.FormatInt(t.ItemID, 10), Title: "Because you watched " + t.Title},
			dropOwned: true,
			load: func() ([]overseerr.Title, error) {
				return cached(&s.discoverCache, "rec:"+t.Kind+":"+strconv.Itoa(t.TMDBID), time.Hour, func() ([]overseerr.Title, error) {
					return cl.Recommendations(ctx, t.Kind, t.TMDBID)
				})
			},
		})
	}
	for _, l := range discoverLists {
		jobs = append(jobs, job{
			row: discoverRow{ID: l.id, Title: l.title},
			load: func() ([]overseerr.Title, error) {
				return cached(&s.discoverCache, "list:"+l.id, 30*time.Minute, func() ([]overseerr.Title, error) {
					return cl.Discover(ctx, l.id)
				})
			},
		})
	}

	results := make([][]overseerr.Title, len(jobs))
	errs := make([]error, len(jobs))
	var wg sync.WaitGroup
	for i, j := range jobs {
		wg.Go(func() { results[i], errs[i] = j.load() })
	}
	wg.Wait()

	out := discoverView{Rows: []discoverRow{}, CanRequest: can}
	var firstErr error
	for i, j := range jobs {
		if errs[i] != nil {
			s.Log.Warn("discover", "row", j.row.ID, "err", errs[i])
			firstErr = cmp.Or(firstErr, errs[i])
			continue
		}
		if j.row.Items, err = s.discoverTitles(ctx, results[i], j.dropOwned); err != nil {
			s.serverError(w, err)
			return
		}
		if len(j.row.Items) > 0 {
			out.Rows = append(out.Rows, j.row)
		}
	}
	// Some rows are fine without the others; none means Overseerr is down.
	if len(out.Rows) == 0 && firstErr != nil {
		s.overseerrError(w, r, firstErr)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) discoverSearch(w http.ResponseWriter, r *http.Request) {
	ctx, q := r.Context(), strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeJSON(w, http.StatusOK, []discoverTitle{})
		return
	}
	cl, _, err := s.overseerrClient(ctx)
	if err != nil {
		s.overseerrError(w, r, err)
		return
	}
	found, err := cl.Search(ctx, clip(q, 200))
	if err != nil {
		s.overseerrError(w, r, err)
		return
	}
	out, err := s.discoverTitles(ctx, found, false)
	if err != nil {
		s.serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// requestUser is the Overseerr user a request is made as: the one with
// the same name as the person asking or their account, else the admin's
// pick (the key's owner for Matinee's admins).
func (s *Server) requestUser(ctx context.Context, cl *overseerr.Client, c overseerr.Config, u store.User) (int, error) {
	users, err := cached(&s.discoverCache, "users", 10*time.Minute, func() ([]overseerr.User, error) {
		return cl.Users(ctx)
	})
	if err != nil {
		return 0, err
	}
	names := []string{u.Username}
	if u.AccountID != 0 {
		accounts, err := s.Store.Users(ctx)
		if err != nil {
			return 0, err
		}
		for _, a := range accounts {
			if a.ID == u.AccountID {
				names = append(names, a.Username)
			}
		}
	}
	for _, name := range names {
		for _, ou := range users {
			if ou.Matches(name) {
				return ou.ID, nil
			}
		}
	}
	if u.IsAdmin {
		return 0, nil
	}
	return c.RequestAs, nil
}

func (s *Server) discoverRequest(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireRequester(w, r); !ok {
		return
	}
	var in struct {
		Kind   string `json:"kind"`
		TMDBID int    `json:"tmdbId"`
	}
	if err := readJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if in.Kind != "movie" && in.Kind != "show" {
		writeError(w, http.StatusBadRequest, "kind must be movie or show")
		return
	}
	if in.TMDBID <= 0 {
		writeError(w, http.StatusBadRequest, "tmdbId is required")
		return
	}
	ctx := r.Context()
	cl, c, err := s.overseerrClient(ctx)
	if err != nil {
		s.overseerrError(w, r, err)
		return
	}
	t, seasons, err := cl.Lookup(ctx, in.Kind, in.TMDBID)
	if err != nil {
		s.overseerrError(w, r, err)
		return
	}
	v, err := s.discoverStatus(ctx, t)
	if err != nil {
		s.serverError(w, err)
		return
	}
	switch v.Status {
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
	as, err := s.requestUser(ctx, cl, c, currentUser(r))
	if err != nil {
		s.overseerrError(w, r, err)
		return
	}
	state, err := cl.Request(ctx, in.Kind, in.TMDBID, seasons, as)
	if err != nil {
		s.overseerrError(w, r, err)
		return
	}
	// Lists show where titles stand, so they're fetched again.
	s.discoverCache.clear()
	v.Status = titleRequested
	if state == overseerr.RequestApproved {
		v.Status = titleAdded
	}
	writeJSON(w, http.StatusCreated, v)
}
