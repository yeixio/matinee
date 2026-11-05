package api

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/yeixio/matinee/internal/arr"
)

// Box Office (github.com/yeixio/boxoffice) finds and downloads movies and
// shows, and answers as Radarr at /radarr and as Sonarr at /sonarr. Admins
// connect it here with one address and its API key, which sets both the
// Radarr and Sonarr connections to it; everything else (requests, coming
// soon, posters, notifications) then works as it does with those apps.
//
// Matinee looks for it on its own: Box Office says who it is, without
// signing in, at GET /api/v1/status.

// boxOfficeGuesses are where Box Office usually is, as Matinee sees it: its
// name in Kubernetes or Docker Compose, then the same machine.
var boxOfficeGuesses = []string{"http://boxoffice:7272", "http://localhost:7272", "http://host.docker.internal:7272"}

// boxOfficeApps are Box Office's Radarr and Sonarr, by where it serves them.
var boxOfficeApps = []arr.App{arr.Radarr, arr.Sonarr}

type boxOfficeView struct {
	// Connected says Radarr and Sonarr both go to Box Office; Movies and
	// Shows say which of them do.
	Connected bool   `json:"connected"`
	Movies    bool   `json:"movies"`
	Shows     bool   `json:"shows"`
	URL       string `json:"url"`
	HasAPIKey bool   `json:"hasApiKey"`
	// ID and Error are from Matinee's last check, every half a minute.
	ID    string `json:"id,omitempty"`
	Error string `json:"error,omitempty"`
}

// boxOfficeBase is the Box Office address an app's connection goes to: its
// address without /radarr or /sonarr.
func boxOfficeBase(app arr.App, c arr.Config) (string, bool) {
	return strings.CutSuffix(c.URL, "/"+string(app))
}

// trimBoxOfficeURL accepts Box Office's address as typed, even with
// /radarr or /sonarr on the end.
func trimBoxOfficeURL(raw string) (string, error) {
	u, err := arr.NormalizeURL(raw)
	if err != nil {
		return "", badRequest(err.Error())
	}
	for _, app := range boxOfficeApps {
		u = strings.TrimSuffix(u, "/"+string(app))
	}
	return u, nil
}

func (s *Server) boxOfficeView(ctx context.Context) (boxOfficeView, map[arr.App]arr.Config, error) {
	var v boxOfficeView
	saved := map[arr.App]arr.Config{}
	checked := map[arr.App]boxOfficeState{}
	for _, st := range s.boxOffices.list() {
		checked[st.App] = st
	}
	key := ""
	for _, app := range boxOfficeApps {
		c, err := s.arrConfig(ctx, app)
		if err != nil {
			return v, nil, err
		}
		saved[app] = c
		base, ok := boxOfficeBase(app, c)
		// Real Radarr behind a proxy at /radarr isn't Box Office.
		if st, seen := checked[app]; seen && st.ID == "" && st.Error == "" {
			ok = false
		}
		if !ok || (v.URL != "" && (base != v.URL || c.APIKey != key)) {
			continue
		}
		v.URL, key = base, c.APIKey
		if st := checked[app]; v.ID == "" {
			v.ID, v.Error = st.ID, st.Error
		}
		if app == arr.Radarr {
			v.Movies = true
		} else {
			v.Shows = true
		}
	}
	v.Connected, v.HasAPIKey = v.Movies && v.Shows, key != ""
	return v, saved, nil
}

// getBoxOffice is GET /integrations/boxoffice.
func (s *Server) getBoxOffice(w http.ResponseWriter, r *http.Request) {
	v, _, err := s.boxOfficeView(r.Context())
	if err != nil {
		s.serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

type foundBoxOffice struct {
	URL     string `json:"url"`
	ID      string `json:"id,omitempty"`
	Version string `json:"version"`
}

// findBoxOffice is POST /integrations/boxoffice/find: the Box Offices
// Matinee can reach, the likeliest first.
func (s *Server) findBoxOffice(w http.ResponseWriter, r *http.Request) {
	guesses := s.boxOfficeGuesses(r)
	found := make([]*foundBoxOffice, len(guesses))
	var wg sync.WaitGroup
	for i, g := range guesses {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
			defer cancel()
			found[i] = probeBoxOffice(ctx, g)
		}()
	}
	wg.Wait()
	out := []foundBoxOffice{}
	for _, f := range found {
		// One Box Office found at two addresses is listed at the likelier.
		if f != nil && !slices.ContainsFunc(out, func(o foundBoxOffice) bool { return o.URL == f.URL || (f.ID != "" && o.ID == f.ID) }) {
			out = append(out, *f)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"found": out})
}

// boxOfficeGuesses are the addresses to look for Box Office at: where
// Radarr or Sonarr already go, the usual ones, and boxoffice. beside
// Matinee's own domain.
func (s *Server) boxOfficeGuesses(r *http.Request) []string {
	var out []string
	for _, app := range boxOfficeApps {
		if c, err := s.arrConfig(r.Context(), app); err == nil {
			if base, ok := boxOfficeBase(app, c); ok {
				out = append(out, base)
			}
		}
	}
	out = append(out, boxOfficeGuesses...)
	hosts := []string{r.Host}
	if u, err := url.Parse(s.PublicURL); err == nil && s.PublicURL != "" {
		hosts = append([]string{u.Host}, hosts...)
	}
	for _, h := range hosts {
		if host, _, err := net.SplitHostPort(h); err == nil {
			h = host
		}
		if net.ParseIP(h) != nil {
			continue
		}
		if _, domain, ok := strings.Cut(h, "."); ok && strings.Contains(domain, ".") {
			out = append(out, "https://boxoffice."+domain)
		}
	}
	seen := map[string]bool{}
	return slices.DeleteFunc(out, func(u string) bool {
		d := seen[u]
		seen[u] = true
		return d
	})
}

// probeBoxOffice asks an address whether it's Box Office, without signing
// in, and says where it really is after any redirect (such as to https).
func probeBoxOffice(ctx context.Context, base string) *foundBoxOffice {
	req, err := http.NewRequestWithContext(ctx, "GET", base+"/api/v1/status", nil)
	if err != nil {
		return nil
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil
	}
	defer res.Body.Close()
	var st struct {
		Product string `json:"product"`
		ID      string `json:"id"`
		Version string `json:"version"`
	}
	if res.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(res.Body, 1<<16)).Decode(&st) != nil || st.Product != "boxoffice" {
		return nil
	}
	u := *res.Request.URL
	u.Path, u.RawQuery = strings.TrimSuffix(u.Path, "/api/v1/status"), ""
	return &foundBoxOffice{URL: u.String(), ID: st.ID, Version: st.Version}
}

type boxOfficeConfig struct {
	URL    string `json:"url"`
	APIKey string `json:"apiKey"`
}

type boxOfficeCheck struct {
	URL     string `json:"url"`
	ID      string `json:"id"`
	Version string `json:"version"`
	// Movies and Shows are what Box Office has for each: its quality
	// profiles and root folders.
	Movies connectionCheck `json:"movies"`
	Shows  connectionCheck `json:"shows"`
}

// checkBoxOffice checks an address and key reach Box Office, as both
// Radarr and Sonarr. Leaving the key out keeps the saved one, but only for
// the saved address.
func (s *Server) checkBoxOffice(ctx context.Context, in boxOfficeConfig) (boxOfficeCheck, string, error) {
	var out boxOfficeCheck
	base, err := trimBoxOfficeURL(in.URL)
	if err != nil {
		return out, "", err
	}
	key := strings.TrimSpace(in.APIKey)
	if key == "" {
		v, _, err := s.boxOfficeView(ctx)
		if err != nil {
			return out, "", err
		}
		if v.URL != base || !v.HasAPIKey {
			return out, "", badRequest("enter Box Office's API key (Box Office → Settings → Connect Matinee)")
		}
		c, _ := s.arrConfig(ctx, arr.Radarr)
		if !v.Movies {
			c, _ = s.arrConfig(ctx, arr.Sonarr)
		}
		key = c.APIKey
	}
	for _, app := range boxOfficeApps {
		cl, err := arr.New(app, arr.Config{URL: base + "/" + string(app), APIKey: key})
		if err != nil {
			return out, "", badRequest(err.Error())
		}
		st, err := cl.Status(ctx)
		if err != nil {
			return out, "", err
		}
		if st.BoxOfficeID == "" {
			return out, "", badRequest("that address is Radarr or Sonarr, not Box Office")
		}
		// It may have moved to https.
		b, _ := boxOfficeBase(app, arr.Config{URL: cl.URL()})
		out.URL, out.ID, out.Version = b, st.BoxOfficeID, st.Version
		c := connectionCheck{URL: cl.URL(), AppName: "Box Office", Version: st.Version}
		if c.QualityProfiles, err = cl.QualityProfiles(ctx); err != nil {
			return out, "", err
		}
		if c.RootFolders, err = cl.RootFolders(ctx); err != nil {
			return out, "", err
		}
		if app == arr.Radarr {
			out.Movies = c
		} else {
			out.Shows = c
		}
	}
	return out, key, nil
}

// testBoxOffice is POST /integrations/boxoffice/test.
func (s *Server) testBoxOffice(w http.ResponseWriter, r *http.Request) {
	var in boxOfficeConfig
	if err := readOptionalJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if in.URL == "" {
		v, _, err := s.boxOfficeView(r.Context())
		if err != nil {
			s.serverError(w, err)
			return
		}
		in.URL = v.URL
	}
	res, _, err := s.checkBoxOffice(r.Context(), in)
	if err != nil {
		s.boxOfficeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// putBoxOffice is PUT /integrations/boxoffice: Radarr and Sonarr both go to
// Box Office. Quality and folder stay as they were when they still exist
// there, else they're Box Office's first; they're changed under Radarr and
// Sonarr. A kind Box Office has no root folder for yet isn't connected.
func (s *Server) putBoxOffice(w http.ResponseWriter, r *http.Request) {
	var in boxOfficeConfig
	if err := readJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	res, key, err := s.checkBoxOffice(r.Context(), in)
	if err != nil {
		s.boxOfficeError(w, r, err)
		return
	}
	if len(res.Movies.RootFolders) == 0 && len(res.Shows.RootFolders) == 0 {
		writeError(w, http.StatusBadRequest, "add a root folder in Box Office first (Settings)")
		return
	}
	for app, chk := range map[arr.App]connectionCheck{arr.Radarr: res.Movies, arr.Sonarr: res.Shows} {
		if len(chk.RootFolders) == 0 || len(chk.QualityProfiles) == 0 {
			continue
		}
		old, err := s.arrConfig(r.Context(), app)
		if err != nil {
			s.serverError(w, err)
			return
		}
		c := arr.Config{URL: chk.URL, APIKey: key, QualityProfileID: old.QualityProfileID, RootFolder: old.RootFolder}
		if !slices.ContainsFunc(chk.QualityProfiles, func(p arr.QualityProfile) bool { return p.ID == c.QualityProfileID }) {
			c.QualityProfileID = chk.QualityProfiles[0].ID
		}
		if !slices.ContainsFunc(chk.RootFolders, func(f arr.RootFolder) bool { return f.Path == c.RootFolder }) {
			c.RootFolder = chk.RootFolders[0].Path
		}
		b, _ := json.Marshal(c)
		if err := s.Store.SetSetting(r.Context(), settingKey(app), string(b)); err != nil {
			s.serverError(w, err)
			return
		}
		s.boxOffices.set(app, boxOfficeState{ID: res.ID})
	}
	s.arrChanged()
	s.getBoxOffice(w, r)
}

// deleteBoxOffice is DELETE /integrations/boxoffice: Radarr and Sonarr
// stop going to Box Office.
func (s *Server) deleteBoxOffice(w http.ResponseWriter, r *http.Request) {
	v, saved, err := s.boxOfficeView(r.Context())
	if err != nil {
		s.serverError(w, err)
		return
	}
	for _, app := range boxOfficeApps {
		if base, ok := boxOfficeBase(app, saved[app]); !ok || base != v.URL {
			continue
		}
		if err := s.Store.SetSetting(r.Context(), settingKey(app), ""); err != nil {
			s.serverError(w, err)
			return
		}
		s.boxOffices.mu.Lock()
		delete(s.boxOffices.ids, app)
		s.boxOffices.mu.Unlock()
	}
	s.arrChanged()
	w.WriteHeader(http.StatusNoContent)
}

// arrChanged follows a change to the Radarr or Sonarr connection.
func (s *Server) arrChanged() {
	s.arrCache.clear()
	if s.OnArr != nil {
		go s.OnArr()
	}
}

// boxOfficeError answers for a failed check of Box Office. The page is for
// admins, so they see what went wrong.
func (s *Server) boxOfficeError(w http.ResponseWriter, r *http.Request, err error) {
	s.arrError(w, r, arr.Radarr, err)
}
