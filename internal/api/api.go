// Package api is Matinee's HTTP API and the host for the web player.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/yeixio/matinee/internal/artwork"
	"github.com/yeixio/matinee/internal/auth"
	"github.com/yeixio/matinee/internal/probe"
	"github.com/yeixio/matinee/internal/scan"
	"github.com/yeixio/matinee/internal/store"
	"github.com/yeixio/matinee/internal/stream"
	"github.com/yeixio/matinee/internal/subtitle"
	"github.com/yeixio/matinee/internal/version"
)

type Server struct {
	Store      *store.Store
	Scanner    *scan.Scanner
	Transcoder *stream.Transcoder
	Art        *artwork.Cache
	Subtitles  *subtitle.Converter
	Web        fs.FS // built web player; nil serves a placeholder
	Log        *slog.Logger

	// BackgroundCtx scopes scans started by API calls; it outlives any one request.
	BackgroundCtx context.Context
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "version": version.Version})
	})
	// Kubernetes probes: alive means the process answers, ready means the database does too.
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := s.Store.Ping(ctx); err != nil {
			http.Error(w, "database unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Write([]byte("ok"))
	})
	mux.HandleFunc("GET /api/setup", s.getSetup)
	mux.HandleFunc("POST /api/setup", s.postSetup)
	mux.HandleFunc("POST /api/auth/login", s.login)
	mux.HandleFunc("POST /api/auth/logout", s.logout)

	user := func(pattern string, h http.HandlerFunc) { mux.Handle(pattern, s.requireUser(h, false)) }
	admin := func(pattern string, h http.HandlerFunc) { mux.Handle(pattern, s.requireUser(h, true)) }

	user("GET /api/me", s.me)
	admin("GET /api/users", s.listUsers)
	admin("POST /api/users", s.createUser)

	user("GET /api/libraries", s.listLibraries)
	admin("POST /api/libraries", s.createLibrary)
	admin("DELETE /api/libraries/{id}", s.deleteLibrary)
	admin("POST /api/libraries/{id}/scan", s.scanLibrary)
	user("GET /api/libraries/{id}/items", s.libraryItems)
	admin("GET /api/fs", s.browseFS)

	user("GET /api/home", s.home)
	user("GET /api/search", s.search)
	user("GET /api/items/{id}", s.getItem)
	user("GET /api/items/{id}/children", s.itemChildren)
	user("PUT /api/items/{id}/progress", s.setProgress)
	user("GET /api/items/{id}/image/{type}", s.itemImage)
	user("GET /api/items/{id}/file", s.directFile)
	user("GET /api/items/{id}/subtitles/{track}", s.subtitleFile)
	user("POST /api/items/{id}/hls", s.startHLS)
	user("GET /api/hls/{session}/{file}", s.hlsFile)

	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "no such endpoint")
	})
	mux.Handle("/", s.webHandler())
	return s.logRequests(mux)
}

// ---- helpers ----

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func (s *Server) serverError(w http.ResponseWriter, err error) {
	s.Log.Error("request failed", "err", err)
	writeError(w, http.StatusInternalServerError, "something went wrong")
}

func readJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

func pathID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id, err == nil
}

func currentUser(r *http.Request) store.User {
	u, _ := auth.UserFrom(r.Context())
	return u
}

func (s *Server) requireUser(h http.HandlerFunc, adminOnly bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := auth.Token(r)
		if token == "" {
			writeError(w, http.StatusUnauthorized, "sign in required")
			return
		}
		u, err := s.Store.SessionUser(r.Context(), token)
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusUnauthorized, "sign in required")
			return
		}
		if err != nil {
			s.serverError(w, err)
			return
		}
		if adminOnly && !u.IsAdmin {
			writeError(w, http.StatusForbidden, "admins only")
			return
		}
		h(w, r.WithContext(auth.WithUser(r.Context(), u)))
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) { s.status = code; s.ResponseWriter.WriteHeader(code) }

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: 200}
		next.ServeHTTP(rec, r)
		if strings.HasPrefix(r.URL.Path, "/api/") && !strings.HasPrefix(r.URL.Path, "/api/hls/") {
			s.Log.Debug("http", "method", r.Method, "path", r.URL.Path, "status", rec.status, "took", time.Since(start))
		}
	})
}

// ---- setup and auth ----

func (s *Server) getSetup(w http.ResponseWriter, r *http.Request) {
	n, err := s.Store.CountUsers(r.Context())
	if err != nil {
		s.serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"needsSetup": n == 0})
}

type credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (c credentials) validate() string {
	switch {
	case strings.TrimSpace(c.Username) == "":
		return "username is required"
	case len(c.Password) < 8:
		return "password must be at least 8 characters"
	}
	return ""
}

// postSetup creates the first (admin) account. It only works once.
func (s *Server) postSetup(w http.ResponseWriter, r *http.Request) {
	var c credentials
	if err := readJSON(r, &c); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if msg := c.validate(); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	n, err := s.Store.CountUsers(r.Context())
	if err != nil {
		s.serverError(w, err)
		return
	}
	if n > 0 {
		writeError(w, http.StatusConflict, "setup is already done")
		return
	}
	hash, err := auth.HashPassword(c.Password)
	if err != nil {
		s.serverError(w, err)
		return
	}
	if _, err := s.Store.CreateUser(r.Context(), strings.TrimSpace(c.Username), hash, true); err != nil {
		s.serverError(w, err)
		return
	}
	s.login(w, r.WithContext(context.WithValue(r.Context(), credsKey{}, c)))
}

type credsKey struct{}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	c, ok := r.Context().Value(credsKey{}).(credentials)
	if !ok {
		if err := readJSON(r, &c); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request")
			return
		}
	}
	u, token, err := auth.Login(r.Context(), s.Store, strings.TrimSpace(c.Username), c.Password)
	if errors.Is(err, auth.ErrBadCredentials) {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	if err != nil {
		s.serverError(w, err)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: auth.CookieName, Value: token, Path: "/", HttpOnly: true,
		SameSite: http.SameSiteLaxMode, Secure: r.TLS != nil, MaxAge: int(auth.SessionTTL.Seconds()),
	})
	writeJSON(w, http.StatusOK, map[string]any{"user": u, "token": token})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if token := auth.Token(r); token != "" {
		s.Store.DeleteSession(r.Context(), token)
	}
	http.SetCookie(w, &http.Cookie{Name: auth.CookieName, Value: "", Path: "/", MaxAge: -1})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, currentUser(r))
}

func (s *Server) listUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.Store.Users(r.Context())
	if err != nil {
		s.serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, users)
}

func (s *Server) createUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		credentials
		IsAdmin bool `json:"isAdmin"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if msg := req.validate(); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		s.serverError(w, err)
		return
	}
	u, err := s.Store.CreateUser(r.Context(), strings.TrimSpace(req.Username), hash, req.IsAdmin)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			writeError(w, http.StatusConflict, "that username is taken")
			return
		}
		s.serverError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, u)
}

// ---- libraries ----

type libraryView struct {
	store.Library
	Scanning bool `json:"scanning"`
}

func (s *Server) listLibraries(w http.ResponseWriter, r *http.Request) {
	libs, err := s.Store.Libraries(r.Context())
	if err != nil {
		s.serverError(w, err)
		return
	}
	out := make([]libraryView, len(libs))
	for i, l := range libs {
		out[i] = libraryView{l, s.Scanner.Running(l.ID)}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) createLibrary(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name  string   `json:"name"`
		Kind  string   `json:"kind"`
		Paths []string `json:"paths"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if _, ok := scan.KindByName(req.Kind); !ok {
		writeError(w, http.StatusBadRequest, "kind must be one of: "+strings.Join(scan.KindNames(), ", "))
		return
	}
	if len(req.Paths) == 0 {
		writeError(w, http.StatusBadRequest, "add at least one folder")
		return
	}
	for i, p := range req.Paths {
		p = filepath.Clean(p)
		if info, err := os.Stat(p); err != nil || !info.IsDir() {
			writeError(w, http.StatusBadRequest, "folder not found: "+p)
			return
		}
		req.Paths[i] = p
	}
	lib, err := s.Store.CreateLibrary(r.Context(), req.Name, req.Kind, req.Paths)
	if err != nil {
		s.serverError(w, err)
		return
	}
	go s.Scanner.Scan(s.BackgroundCtx, lib)
	writeJSON(w, http.StatusCreated, libraryView{lib, true})
}

func (s *Server) deleteLibrary(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	if err := s.Store.DeleteLibrary(r.Context(), id); errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "library not found")
		return
	} else if err != nil {
		s.serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) scanLibrary(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	lib, err := s.Store.Library(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "library not found")
		return
	} else if err != nil {
		s.serverError(w, err)
		return
	}
	go s.Scanner.Scan(s.BackgroundCtx, lib)
	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) libraryItems(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	items, err := s.Store.TopLevel(r.Context(), currentUser(r).ID, id)
	if err != nil {
		s.serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

// browseFS lists subfolders so admins can pick library folders from the browser.
func (s *Server) browseFS(w http.ResponseWriter, r *http.Request) {
	dir := r.URL.Query().Get("path")
	if dir == "" {
		dir = "/"
	}
	dir = filepath.Clean(dir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		writeError(w, http.StatusBadRequest, "can't read that folder")
		return
	}
	dirs := []string{}
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			dirs = append(dirs, e.Name())
		}
	}
	sort.Strings(dirs)
	parent := filepath.Dir(dir)
	writeJSON(w, http.StatusOK, map[string]any{"path": dir, "parent": parent, "dirs": dirs})
}

// ---- browsing ----

func (s *Server) home(w http.ResponseWriter, r *http.Request) {
	uid := currentUser(r).ID
	cont, err := s.Store.ContinueWatching(r.Context(), uid, 20)
	if err != nil {
		s.serverError(w, err)
		return
	}
	recent, err := s.Store.RecentlyAdded(r.Context(), uid, 30)
	if err != nil {
		s.serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"continueWatching": cont, "recentlyAdded": recent})
}

func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	items, err := s.Store.Search(r.Context(), currentUser(r).ID, r.URL.Query().Get("q"), 50)
	if err != nil {
		s.serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) loadItem(w http.ResponseWriter, r *http.Request) (store.Item, bool) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad id")
		return store.Item{}, false
	}
	it, err := s.Store.Item(r.Context(), currentUser(r).ID, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "item not found")
		return it, false
	}
	if err != nil {
		s.serverError(w, err)
		return it, false
	}
	return it, true
}

type itemView struct {
	store.Item
	MimeType  string           `json:"mimeType,omitempty"`
	Subtitles []subtitle.Track `json:"subtitles,omitempty"`
	Parents   []store.Item     `json:"parents"` // root first: show, season
}

func (s *Server) getItem(w http.ResponseWriter, r *http.Request) {
	it, ok := s.loadItem(w, r)
	if !ok {
		return
	}
	v := itemView{Item: it, Parents: []store.Item{}}
	if it.Playable {
		v.MimeType = stream.MimeType(it.Path)
	}
	if it.Playable && it.VideoCodec != "" {
		v.Subtitles = s.subtitleTracks(r.Context(), it)
	}
	for p := it.ParentID; p != nil; {
		parent, err := s.Store.Item(r.Context(), currentUser(r).ID, *p)
		if err != nil {
			break
		}
		v.Parents = append([]store.Item{parent}, v.Parents...)
		p = parent.ParentID
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) itemChildren(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	items, err := s.Store.Children(r.Context(), currentUser(r).ID, id)
	if err != nil {
		s.serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) setProgress(w http.ResponseWriter, r *http.Request) {
	it, ok := s.loadItem(w, r)
	if !ok {
		return
	}
	var req struct {
		PositionMs int64 `json:"positionMs"`
		Played     bool  `json:"played"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	// Past 92% counts as watched, the way credits usually start.
	if it.DurationMs != nil && *it.DurationMs > 0 && req.PositionMs > *it.DurationMs*92/100 {
		req.Played, req.PositionMs = true, 0
	}
	if err := s.Store.SetProgress(r.Context(), currentUser(r).ID, it.ID, req.PositionMs, req.Played); err != nil {
		s.serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// itemImage serves a poster or backdrop. Seasons and tracks without their
// own poster borrow their show's or album's.
func (s *Server) itemImage(w http.ResponseWriter, r *http.Request) {
	it, ok := s.loadItem(w, r)
	if !ok {
		return
	}
	kind := r.PathValue("type")
	if kind != "poster" && kind != "backdrop" {
		http.NotFound(w, r)
		return
	}
	for range 3 {
		src := it.Poster
		if kind == "backdrop" {
			src = it.Backdrop
		}
		if src != "" {
			s.Art.Serve(w, r, src)
			return
		}
		borrow := kind == "poster" && (it.Kind == "season" || it.Kind == "track") ||
			kind == "backdrop" && (it.Kind == "season" || it.Kind == "episode")
		if !borrow || it.ParentID == nil {
			break
		}
		parent, err := s.Store.Item(r.Context(), 0, *it.ParentID)
		if err != nil {
			break
		}
		it = parent
	}
	http.NotFound(w, r)
}

// ---- playback ----

func (s *Server) directFile(w http.ResponseWriter, r *http.Request) {
	it, ok := s.loadItem(w, r)
	if !ok {
		return
	}
	if !it.Playable {
		writeError(w, http.StatusBadRequest, "not a playable item")
		return
	}
	stream.ServeDirect(w, r, it)
}

func (s *Server) startHLS(w http.ResponseWriter, r *http.Request) {
	it, ok := s.loadItem(w, r)
	if !ok {
		return
	}
	if !it.Playable {
		writeError(w, http.StatusBadRequest, "not a playable item")
		return
	}
	start, _ := strconv.ParseFloat(r.URL.Query().Get("start"), 64)
	burn := -1
	if sub := r.URL.Query().Get("subtitle"); sub != "" {
		t, ok := subtitle.Find(s.subtitleTracks(r.Context(), it), sub)
		if !ok {
			writeError(w, http.StatusBadRequest, "no such subtitle track")
			return
		}
		// Text subtitles are drawn by the player; only pictures need burning in.
		if t.BurnIn {
			burn = t.Stream
		}
	}
	id, err := s.Transcoder.Start(r.Context(), currentUser(r).ID, it, start, burn)
	if err != nil {
		s.Log.Error("transcode", "item", it.ID, "err", err)
		writeError(w, http.StatusInternalServerError, "couldn't start playback for this file")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"url": "/api/hls/" + id + "/index.m3u8", "startSec": start})
}

// subtitleTracks lists a video's subtitle tracks. Files scanned before
// Matinee looked for embedded subtitles are probed now, once.
func (s *Server) subtitleTracks(ctx context.Context, it store.Item) []subtitle.Track {
	var embedded []probe.Subtitle
	raw, ok, err := s.Store.SubtitleStreams(ctx, it.ID)
	if err != nil {
		s.Log.Warn("subtitle streams", "item", it.ID, "err", err)
	} else if ok {
		json.Unmarshal([]byte(raw), &embedded)
	} else if pr, err := s.Scanner.Prober.Probe(ctx, it.Path); err == nil {
		embedded = pr.Subtitles
		b, _ := json.Marshal(pr.SubtitleList())
		if err := s.Store.SetSubtitleStreams(ctx, it.ID, string(b)); err != nil {
			s.Log.Warn("save subtitle streams", "item", it.ID, "err", err)
		}
	}
	return subtitle.List(it.Path, embedded)
}

func (s *Server) subtitleFile(w http.ResponseWriter, r *http.Request) {
	it, ok := s.loadItem(w, r)
	if !ok {
		return
	}
	if !it.Playable || it.VideoCodec == "" {
		writeError(w, http.StatusBadRequest, "not a video")
		return
	}
	tracks := s.subtitleTracks(r.Context(), it)
	t, ok := subtitle.Find(tracks, strings.TrimSuffix(r.PathValue("track"), ".vtt"))
	if !ok {
		writeError(w, http.StatusNotFound, "no such subtitle track")
		return
	}
	s.Subtitles.Serve(w, r, it.Path, t, tracks)
}

func (s *Server) hlsFile(w http.ResponseWriter, r *http.Request) {
	s.Transcoder.ServeFile(w, r, currentUser(r).ID, r.PathValue("session"), r.PathValue("file"))
}

// ---- web player ----

func (s *Server) webHandler() http.Handler {
	if s.Web == nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write([]byte("<!doctype html><title>Matinee</title><p>The web player isn't built. Run <code>make web</code>, then rebuild the server.</p>"))
		})
	}
	files := http.FileServerFS(s.Web)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p != "" {
			if f, err := s.Web.Open(p); err == nil {
				f.Close()
				if strings.HasPrefix(p, "assets/") {
					// Vite fingerprints asset names, so they never change.
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				files.ServeHTTP(w, r)
				return
			}
		}
		// Client-side routes all load the app shell.
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFileFS(w, r, s.Web, "index.html")
	})
}
