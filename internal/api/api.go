// Package api is Matinee's HTTP API and the host for the web player.
package api

import (
	"compress/gzip"
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
	"sync/atomic"
	"time"

	"github.com/yeixio/matinee/internal/analyze"
	"github.com/yeixio/matinee/internal/artwork"
	"github.com/yeixio/matinee/internal/auth"
	"github.com/yeixio/matinee/internal/download"
	"github.com/yeixio/matinee/internal/hwaccel"
	"github.com/yeixio/matinee/internal/probe"
	"github.com/yeixio/matinee/internal/remote"
	"github.com/yeixio/matinee/internal/scan"
	"github.com/yeixio/matinee/internal/store"
	"github.com/yeixio/matinee/internal/stream"
	"github.com/yeixio/matinee/internal/subtitle"
	"github.com/yeixio/matinee/internal/version"
)

type Server struct {
	Store   *store.Store
	Scanner *scan.Scanner
	Watcher *scan.Watcher // nil means libraries aren't watched for changes
	// ScanInterval is how often every library is rescanned; 0 means never.
	ScanInterval time.Duration
	Transcoder   *stream.Transcoder
	HW           *hwaccel.Manager // hardware transcoding; nil means the CPU only
	Downloads    *download.Manager
	Analyzer     *analyze.Analyzer // nil when media analysis is off for this server
	Art          *artwork.Cache
	Subtitles    *subtitle.Converter
	Web          fs.FS // built web player; nil serves a placeholder
	Log          *slog.Logger
	MediaRoot    string // where the folder picker starts; empty means /
	Name         string // what the apps call this server
	// TrailerSource lists trailers online; nil, or TMDB without a key,
	// offers none.
	TrailerSource TrailerSource
	// Keywords finds TMDB keywords for holiday rows; nil (or no TMDB key)
	// matches on words alone.
	Keywords KeywordSource
	// People has biographies; nil (or no TMDB key) shows only names and photos.
	People PersonSource
	// TMDB is the client whose key admins can change in Settings (see
	// tmdbkey.go); nil means it can't be changed here. TMDBFallback is the
	// key it goes back to when the one in Settings is cleared, and
	// TMDBFallbackSource where that one comes from (environment or built-in).
	TMDB               TMDBKeyHolder
	TMDBFallback       string
	TMDBFallbackSource string
	// OnTMDBKey runs after an admin changes the TMDB key.
	OnTMDBKey func()
	// ThemeDir holds the admin's own theme files (see themes.go); empty means
	// only the built-in themes.
	ThemeDir string

	// Remote keeps remote access going (see remote.go); nil when this
	// server doesn't offer it.
	Remote *remote.Manager
	// PublicURL is the address set with MATINEE_PUBLIC_URL, if any.
	PublicURL string
	// LookupHost resolves PublicURL's host; nil uses the system resolver.
	LookupHost  func(ctx context.Context, host string) ([]string, error)
	publicCheck publicCheck // whether PublicURL's address is public (see remote.go)

	// HomeListener means the home network has its own listener (HomeHandler),
	// so requests to the main one never count as coming from home.
	HomeListener    bool
	homeSignIn      homeSetting
	setUp           atomic.Bool     // the first account exists
	arrCache        ttlCache        // answers from Radarr and Sonarr (see arr.go)
	discoverCache   ttlCache        // answers from Overseerr (see discover.go)
	trailerCache    ttlCache        // trailer files' probes and TMDB's trailer lists (see trailers.go)
	pins            pinGuard        // wrong profile PINs (see profiles.go)
	holidayKeywords holidayKeywords // TMDB's ids for holiday keywords (see holidays.go)
	langs           languages       // profiles' languages (see languages.go)

	// Translate looks up translations for every library, when people start
	// using a language; nil without an online provider that has them.
	Translate func(context.Context)

	// BackgroundCtx scopes scans started by API calls; it outlives any one request.
	BackgroundCtx context.Context
}

// APIVersion is the version of the documented API served under /api/v1
// (openapi.yaml). Changes within a version only add things; anything that
// would break an app goes in /api/v2.
const APIVersion = 1

type access int

const (
	public access = iota
	// reader routes only look and play, so devices on the home network can
	// use them without signing in (see home.go).
	reader
	signedIn
	adminOnly
)

type route struct {
	pattern string // "METHOD /path", relative to /api/v1
	access  access
	handler http.HandlerFunc
}

func (s *Server) routes() []route {
	return []route{
		{"GET /health", public, func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "version": version.Version, "apiVersion": APIVersion})
		}},
		{"GET /server", public, s.serverInfo},
		{"GET /openapi.yaml", public, serveOpenAPI},
		{"GET /setup", public, s.getSetup},
		{"POST /setup", public, s.postSetup},
		{"POST /auth/login", public, s.login},
		{"POST /auth/logout", public, s.logout},
		{"POST /auth/device", public, s.startDeviceCode},
		{"POST /auth/device/token", public, s.pollDeviceCode},
		{"GET /auth/device/{code}", signedIn, grownUps(s.getDeviceCode)},
		{"POST /auth/device/{code}/approve", signedIn, grownUps(s.approveDeviceCode)},
		{"POST /auth/device/{code}/deny", signedIn, grownUps(s.denyDeviceCode)},

		{"GET /me", reader, s.me},
		{"GET /me/theme", reader, s.getMyTheme},
		{"PUT /me/theme", reader, s.putMyTheme},
		{"GET /me/language", reader, s.getMyLanguage},
		{"PUT /me/language", reader, s.putMyLanguage},
		{"GET /languages", public, s.listLanguages},
		{"GET /me/playback", reader, s.getMyPlayback},
		{"PUT /me/playback", reader, s.putMyPlayback},
		{"PUT /me/password", signedIn, s.changeMyPassword},
		{"GET /themes", public, s.listThemes},
		{"GET /profiles", reader, s.listProfiles},
		{"POST /profiles/select", reader, s.selectProfile},
		{"GET /profiles/manage", adminOnly, s.manageProfiles},
		{"POST /profiles", adminOnly, s.createProfile},
		{"PUT /profiles/{id}", adminOnly, s.updateProfile},
		{"DELETE /profiles/{id}", adminOnly, s.deleteProfile},
		{"GET /devices", signedIn, grownUps(s.listDevices)},
		{"DELETE /devices/{id}", signedIn, grownUps(s.deleteDevice)},
		{"GET /settings", adminOnly, s.getSettings},
		{"PUT /settings", adminOnly, s.putSettings},
		{"GET /settings/connect/plans", adminOnly, s.connectPlans},
		{"POST /settings/connect/subscribe", adminOnly, s.connectSubscribe},
		{"POST /settings/connect/manage", adminOnly, s.connectManage},
		{"GET /users", adminOnly, s.listUsers},
		{"POST /users", adminOnly, s.createUser},
		{"PATCH /users/{id}", adminOnly, s.updateUser},
		{"PUT /users/{id}/password", adminOnly, s.resetPassword},
		{"DELETE /users/{id}", adminOnly, s.deleteUser},

		{"GET /libraries", reader, s.listLibraries},
		{"POST /libraries", adminOnly, s.createLibrary},
		{"DELETE /libraries/{id}", adminOnly, s.deleteLibrary},
		{"POST /libraries/{id}/scan", adminOnly, s.scanLibrary},
		{"GET /libraries/{id}/items", reader, s.libraryItems},
		{"GET /libraries/{id}/filters", reader, s.libraryFilters},
		{"GET /libraries/{id}/people", reader, s.libraryPeople},
		{"GET /items", reader, s.filterItems},
		{"GET /fs", adminOnly, s.browseFS},
		{"GET /analysis", adminOnly, s.getAnalysis},
		{"PUT /analysis", adminOnly, s.putAnalysis},
		{"POST /items/{id}/analyze", adminOnly, s.analyzeItem},

		{"GET /home", reader, s.home},
		{"GET /rows/{id}", reader, s.rowPage},
		{"GET /search", reader, s.search},
		{"GET /items/{id}", reader, s.getItem},
		{"GET /items/{id}/children", reader, s.itemChildren},
		{"GET /items/{id}/related", reader, s.itemRelated},
		{"GET /items/{id}/next", reader, s.itemNext},
		{"GET /people/{id}", reader, s.getPerson},
		{"GET /people/{id}/image", reader, s.personImage},
		{"PUT /items/{id}/progress", reader, s.setProgress},
		{"DELETE /continue-watching/{id}", reader, s.hideFromContinueWatching},
		{"GET /items/{id}/image/{type}", reader, s.itemImage},
		{"GET /items/{id}/file", reader, s.directFile},
		{"GET /items/{id}/subtitles/{track}", reader, s.subtitleFile},
		{"GET /items/{id}/trailers", reader, s.listTrailers},
		{"GET /items/{id}/trailers/{trailer}/file", reader, s.trailerFile},
		{"POST /items/{id}/trailers/{trailer}/hls", reader, s.trailerHLS},
		{"POST /items/{id}/hls", reader, s.startHLS},
		{"GET /hls/{session}/{file}", reader, s.hlsFile},

		{"GET /collections", reader, s.listCollections},
		{"POST /collections", signedIn, s.createCollection},
		{"GET /collections/{id}", reader, s.getCollection},
		{"PUT /collections/{id}", signedIn, s.updateCollection},
		{"DELETE /collections/{id}", signedIn, s.deleteCollection},
		{"PUT /collections/{id}/items/{item}", signedIn, s.addToCollection},
		{"DELETE /collections/{id}/items/{item}", signedIn, s.removeFromCollection},
		{"GET /box-sets/{id}", reader, s.getBoxSet},
		{"GET /watchlist", reader, s.getWatchList},
		{"PUT /watchlist/{id}", reader, s.addToWatchList},
		{"DELETE /watchlist/{id}", reader, s.removeFromWatchList},

		{"GET /downloads/qualities", signedIn, s.downloadQualities},
		{"POST /downloads", signedIn, s.createDownload},
		{"GET /downloads", signedIn, s.listDownloads},
		{"GET /downloads/{id}", signedIn, s.getDownload},
		{"DELETE /downloads/{id}", signedIn, s.deleteDownload},
		{"POST /downloads/{id}/finished", signedIn, s.finishDownload},
		{"GET /downloads/{id}/file", signedIn, s.downloadFile},
		{"POST /sync/progress", signedIn, s.syncProgress},

		{"GET /integrations", adminOnly, s.getIntegrations},
		{"PUT /integrations/{app}", adminOnly, s.putIntegration},
		{"DELETE /integrations/{app}", adminOnly, s.deleteIntegration},
		{"POST /integrations/{app}/test", adminOnly, s.testIntegration},
		{"GET /integrations/overseerr", adminOnly, s.getOverseerr},
		{"PUT /integrations/overseerr", adminOnly, s.putOverseerr},
		{"DELETE /integrations/overseerr", adminOnly, s.deleteOverseerr},
		{"POST /integrations/overseerr/test", adminOnly, s.testOverseerr},
		{"GET /discover", reader, grownUps(s.discover)},
		{"GET /discover/search", reader, grownUps(s.discoverSearch)},
		{"GET /discover/rows/{id}", reader, grownUps(s.discoverRowPage)},
		{"POST /discover/requests", signedIn, grownUps(s.discoverRequest)},
		{"GET /requests/options", reader, grownUps(s.getRequestOptions)},
		{"GET /requests/search", signedIn, grownUps(s.searchTitles)},
		{"GET /requests", signedIn, grownUps(s.listRequests)},
		{"POST /requests", signedIn, grownUps(s.createRequest)},
		{"POST /requests/{id}/approve", adminOnly, s.approveRequest},
		{"POST /requests/{id}/decline", adminOnly, s.declineRequest},
		{"DELETE /requests/{id}", signedIn, grownUps(s.deleteRequest)},
		{"GET /upcoming", reader, grownUps(s.upcoming)},
		{"GET /items/{id}/quality", adminOnly, s.getItemQuality},
		{"PUT /items/{id}/quality", adminOnly, s.putItemQuality},
		{"POST /items/{id}/issues", signedIn, grownUps(s.reportIssue)},
		{"GET /issues", signedIn, grownUps(s.listIssues)},
		{"POST /issues/{id}/resolve", adminOnly, s.resolveIssue},
		{"POST /issues/{id}/replace", adminOnly, s.replaceIssue},
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

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

	for _, rt := range s.routes() {
		var h http.Handler = rt.handler
		if rt.access != public {
			h = s.requireUser(rt.handler, rt.access)
		}
		method, path, _ := strings.Cut(rt.pattern, " ")
		mux.Handle(method+" /api/v1"+path, h)
		// The same routes without a version, as served before /api/v1, for web
		// players that were open during an upgrade. New clients use /api/v1.
		mux.Handle(method+" /api"+path, h)
	}

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

// writeLargeJSON is writeJSON compressed for clients that accept gzip, for
// lists that can run to megabytes, like a library of thousands of movies.
func writeLargeJSON(w http.ResponseWriter, r *http.Request, v any) {
	w.Header().Add("Vary", "Accept-Encoding")
	if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
		writeJSON(w, http.StatusOK, v)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Encoding", "gzip")
	gz, _ := gzip.NewWriterLevel(w, gzip.BestSpeed)
	json.NewEncoder(gz).Encode(v)
	gz.Close()
}

// writeError answers with an error in English, and its code, which the web
// player and the apps show in their own language (i18n/locales/*/errors.json).
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg, "code": errorCode(msg)})
}

// writeErrorWith is writeError for a message that names something, such as a
// title: template marks each name {{like_this}}, and params has them, so a
// client can put them into the message in its own language.
func writeErrorWith(w http.ResponseWriter, status int, template string, params map[string]string) {
	msg := template
	for k, v := range params {
		msg = strings.ReplaceAll(msg, "{{"+k+"}}", v)
	}
	writeJSON(w, status, map[string]any{"error": msg, "code": errorCode(template), "params": params})
}

// errorCode names an error message for the catalog: "no such theme" is
// no_such_theme, and "{{title}} is already on its way" is
// title_is_already_on_its_way. Messages with details pasted in get codes the
// catalog doesn't have, and show in English.
func errorCode(msg string) string {
	var b strings.Builder
	under := false
	for _, r := range strings.ToLower(msg) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			if under && b.Len() > 0 {
				b.WriteByte('_')
			}
			b.WriteRune(r)
			under = false
		} else if r != '\'' {
			under = true
		}
	}
	return b.String()
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

func (s *Server) requireUser(h http.HandlerFunc, level access) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := auth.Token(r)
		if token == "" && level == reader {
			ok, err := s.homeAccess(r)
			if err != nil {
				s.serverError(w, err)
				return
			}
			if ok {
				guest, err := s.Store.HomeGuest(r.Context())
				if err != nil {
					s.serverError(w, err)
					return
				}
				h(w, r.WithContext(store.WithLanguage(auth.WithUser(r.Context(), guest), s.requestLanguage(r, guest))))
				return
			}
		}
		if token == "" {
			writeError(w, http.StatusUnauthorized, "sign in required")
			return
		}
		u, homeOnly, err := s.Store.SessionUser(r.Context(), token)
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusUnauthorized, "sign in required")
			return
		}
		if err != nil {
			s.serverError(w, err)
			return
		}
		if homeOnly {
			// A profile picked at home without signing in: it can only look
			// and play, and only from home.
			ok, err := s.homeAccess(r)
			if err != nil {
				s.serverError(w, err)
				return
			}
			if !ok || level > reader {
				writeError(w, http.StatusUnauthorized, "sign in required")
				return
			}
		}
		if level == adminOnly && !u.IsAdmin {
			writeError(w, http.StatusForbidden, "admins only")
			return
		}
		h(w, r.WithContext(store.WithLanguage(auth.WithUser(r.Context(), u), s.requestLanguage(r, u))))
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
		if strings.HasPrefix(r.URL.Path, "/api/") && !strings.HasPrefix(r.URL.Path, "/api/hls/") && !strings.HasPrefix(r.URL.Path, "/api/v1/hls/") {
			// Rejected requests are logged by default, so "it failed" shows up in the pod logs.
			level := slog.LevelDebug
			if rec.status >= 400 && rec.status != http.StatusUnauthorized {
				level = slog.LevelInfo
			}
			s.Log.Log(r.Context(), level, "http", "method", r.Method, "path", r.URL.Path, "status", rec.status, "took", time.Since(start))
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
	}
	return validatePassword(c.Password)
}

// validatePassword says what's wrong with a new password, or "" if nothing.
func validatePassword(pw string) string {
	switch {
	case len([]rune(pw)) < 8:
		return "password must be at least 8 characters"
	case len(pw) > 72: // bcrypt's limit
		return "password must be at most 72 bytes"
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
	s.login(w, r.WithContext(context.WithValue(r.Context(), credsKey{}, loginRequest{credentials: c})))
}

type credsKey struct{}

type loginRequest struct {
	credentials
	// Apps name themselves so people can tell their devices apart; browsers
	// are named from their User-Agent.
	DeviceName string `json:"deviceName"`
	Platform   string `json:"platform"`
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	req, ok := r.Context().Value(credsKey{}).(loginRequest)
	if !ok {
		if err := readJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request")
			return
		}
	}
	u, token, err := auth.Login(r.Context(), s.Store, strings.TrimSpace(req.Username), req.Password, requestDevice(r, req.DeviceName, req.Platform))
	if errors.Is(err, auth.ErrBadCredentials) {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	if err != nil {
		s.serverError(w, err)
		return
	}
	setSessionCookie(w, r, token)
	writeJSON(w, http.StatusOK, map[string]any{"user": u, "token": token})
}

func setSessionCookie(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{
		Name: auth.CookieName, Value: token, Path: "/", HttpOnly: true,
		SameSite: http.SameSiteLaxMode, Secure: r.TLS != nil, MaxAge: int(auth.SessionTTL.Seconds()),
	})
}

// requestDevice names the device a request comes from.
func requestDevice(r *http.Request, name, platform string) store.Device {
	name, platform = clip(strings.TrimSpace(name), 80), clip(strings.ToLower(strings.TrimSpace(platform)), 20)
	if name == "" {
		name = auth.BrowserName(r.UserAgent())
		if platform == "" {
			platform = "web"
		}
	}
	return store.Device{Name: name, Platform: platform}
}

func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
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
	// Watch says how each folder is watched for new files (scan.FolderWatch).
	Watch []scan.FolderWatch `json:"watch"`
}

func (s *Server) libraryView(l store.Library, scanning bool) libraryView {
	v := libraryView{l, scanning, nil}
	if s.Watcher != nil {
		v.Watch = s.Watcher.Status(l.ID)
	}
	if v.Watch == nil {
		v.Watch = []scan.FolderWatch{}
	}
	return v
}

func (s *Server) listLibraries(w http.ResponseWriter, r *http.Request) {
	libs, err := s.Store.Libraries(r.Context())
	if err != nil {
		s.serverError(w, err)
		return
	}
	out := []libraryView{}
	for _, l := range libs {
		ok, err := s.Store.LibraryVisible(r.Context(), currentUser(r), l.ID)
		if err != nil {
			s.serverError(w, err)
			return
		}
		if ok {
			out = append(out, s.libraryView(l, s.Scanner.Running(l.ID)))
		}
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
	if s.Watcher != nil {
		s.Watcher.Reload()
	}
	writeJSON(w, http.StatusCreated, s.libraryView(lib, true))
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
	if s.Watcher != nil {
		s.Watcher.Reload()
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

// browseFS lists subfolders so admins can pick library folders from the browser.
func (s *Server) browseFS(w http.ResponseWriter, r *http.Request) {
	dir := r.URL.Query().Get("path")
	if dir == "" {
		dir = s.MediaRoot
	}
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
	ctx, uid := r.Context(), currentUser(r).ID
	now := today()
	// One more than a row shows says whether it has more.
	cont, err := s.Store.ContinueWatching(ctx, uid, continueItems+1)
	if err != nil {
		s.serverError(w, err)
		return
	}
	recent, err := s.Store.RecentlyAdded(ctx, uid, homeRowItems+1)
	if err != nil {
		s.serverError(w, err)
		return
	}
	watchList, err := s.Store.WatchList(ctx, uid, homeRowItems+1)
	if err != nil {
		s.serverError(w, err)
		return
	}
	releases, err := s.Store.NewReleases(ctx, uid, now.Year()-newReleaseYrs+1, homeRowItems+1)
	if err != nil {
		s.serverError(w, err)
		return
	}
	more := map[string]bool{}
	cont, more[rowContinue] = cut(cont, continueItems)
	recent, more[rowRecent] = cut(recent, homeRowItems)
	watchList, more[rowWatchList] = cut(watchList, homeRowItems)
	releases, more[rowReleases] = cut(releases, homeRowItems)
	pinned, err := s.pinnedRows(ctx, currentUser(r))
	if err != nil {
		s.serverError(w, err)
		return
	}
	rows, err := s.genreRows(ctx, uid, now)
	if err != nil {
		s.serverError(w, err)
		return
	}
	holidayRows, err := s.holidayRows(ctx, uid, now)
	if err != nil {
		s.serverError(w, err)
		return
	}
	// A holiday coming up comes first, then pinned collections, then the
	// day's genres.
	rows = append(append(holidayRows, pinned...), rows...)
	writeJSON(w, http.StatusOK, map[string]any{
		"continueWatching": cont, "watchList": watchList, "recentlyAdded": recent,
		"newReleases": releases, "rows": rows,
		// Which of the lists above have more titles than they show (see rows.go).
		"more": more,
	})
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
	// MediaInfo is the background analysis of a file, every stream in
	// detail; absent until the file is analyzed. MediaInfoStale is true
	// while the file changed and is waiting to be analyzed again.
	MediaInfo      *probe.MediaInfo `json:"mediaInfo,omitempty"`
	MediaInfoStale bool             `json:"mediaInfoStale,omitempty"`
	// FilePath is where the file is on the server, for admins only.
	FilePath string `json:"filePath,omitempty"`
	FileSize int64  `json:"fileSize,omitempty"`
	// Details and Credits are set for movies, shows, seasons and episodes
	// once looked up; Details stays absent until then.
	Details *store.Details `json:"details,omitempty"`
	Credits *store.Credits `json:"credits,omitempty"`
}

func (s *Server) getItem(w http.ResponseWriter, r *http.Request) {
	it, ok := s.loadItem(w, r)
	if !ok {
		return
	}
	v := itemView{Item: it, Parents: []store.Item{}}
	if it.Playable {
		v.MimeType = stream.MimeType(it.Path)
		v.MediaInfo, v.MediaInfoStale = s.mediaInfo(r, it)
		v.FileSize = it.Size
		if currentUser(r).IsAdmin {
			v.FilePath = it.Path
		}
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
	if err := s.addDetails(r.Context(), &v); err != nil {
		s.serverError(w, err)
		return
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
	// So does reaching the closing credits, when a chapter says where they are.
	if it.DurationMs != nil && *it.DurationMs > 0 && req.PositionMs > *it.DurationMs*92/100 {
		req.Played, req.PositionMs = true, 0
	} else if mi, _ := s.mediaInfo(r, it); mi != nil && mi.CreditsMs > 0 && req.PositionMs >= mi.CreditsMs {
		req.Played, req.PositionMs = true, 0
	}
	if err := s.Store.SetProgress(r.Context(), currentUser(r).ID, it.ID, req.PositionMs, req.Played); err != nil {
		s.serverError(w, err)
		return
	}
	if err := s.watched(r.Context(), currentUser(r).ID, it, req.Played); err != nil {
		s.serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// hideFromContinueWatching takes an item off the Continue Watching row,
// keeping its place for when it's played again.
func (s *Server) hideFromContinueWatching(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	if err := s.Store.HideFromContinueWatching(r.Context(), currentUser(r).ID, id); err != nil {
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
			// TVs ask for size=large, falling back to the usual size.
			if r.URL.Query().Get("size") == "large" {
				if large := artwork.Large(src, kind); large != src {
					s.Art.Serve(w, r, large, src)
					return
				}
			}
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
	if errors.Is(err, stream.ErrSuperseded) {
		// The player already asked for a newer position; it ignores this answer.
		writeError(w, http.StatusConflict, "replaced by a newer seek")
		return
	}
	if err != nil {
		s.Log.Error("transcode", "item", it.ID, "err", err)
		writeError(w, http.StatusInternalServerError, "couldn't start playback for this file")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"url": "/api/v1/hls/" + id + "/index.m3u8", "startSec": start})
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
