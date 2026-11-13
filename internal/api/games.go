package api

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/yeixio/matinee/internal/gamehost"
	"github.com/yeixio/matinee/internal/gamelib"
	"github.com/yeixio/matinee/internal/gamestream"
)

// Game streaming, first version: an admin picks a game file (or the lag
// test) and plays it in the web player, streamed from the server. Games
// libraries, and playing in the apps, come next.

func (s *Server) gameRoutes() []route {
	return []route{
		{"GET /games/streaming", adminOnly, s.gameStreaming},
		{"GET /games/files", adminOnly, s.gameFiles},
		{"POST /games/play", adminOnly, s.playGame},
		{"GET /games/host", adminOnly, s.gameHostGames},
		{"GET /games/host/desktop/{path...}", adminOnly, s.gameHostDesktop},
		{"POST /items/{id}/game", signedIn, s.playGameItem},
		{"DELETE /games/play/{id}", signedIn, s.stopGame},
		{"GET /games/play/{id}/stream", signedIn, s.streamGame},
		{"GET /games/emulators", adminOnly, s.gameEmulators},
		{"POST /games/emulators", adminOnly, s.addGameEmulator},
		{"DELETE /games/emulators/{file}", adminOnly, s.removeGameEmulator},
	}
}

var errNoGames = "game streaming isn't set up on this server"

func (s *Server) gameStreaming(w http.ResponseWriter, r *http.Request) {
	if s.Games == nil {
		writeJSON(w, http.StatusOK, map[string]any{"available": false, "systems": []any{}, "lagTest": false})
		return
	}
	systems := s.Games.Available()
	if systems == nil {
		systems = []gamestream.System{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"available": true, "systems": systems, "lagTest": s.Games.HasLagTest(),
		"playing": s.Games.Count(), "platform": corePlatform()})
}

// gameFiles lists a folder's subfolders and the files in it that might be
// games, for picking one to play.
func (s *Server) gameFiles(w http.ResponseWriter, r *http.Request) {
	if s.Games == nil {
		writeError(w, http.StatusNotFound, errNoGames)
		return
	}
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
	type file struct {
		Name string `json:"name"`
		Path string `json:"path"`
	}
	dirs, files := []file{}, []file{}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		f := file{Name: e.Name(), Path: filepath.Join(dir, e.Name())}
		if e.IsDir() {
			dirs = append(dirs, f)
		} else if s.Games.IsGameFile(e.Name()) {
			files = append(files, f)
		}
	}
	byName := func(l []file) {
		sort.Slice(l, func(i, j int) bool { return strings.ToLower(l[i].Name) < strings.ToLower(l[j].Name) })
	}
	byName(dirs)
	byName(files)
	writeJSON(w, http.StatusOK, map[string]any{"path": dir, "parent": parentDir(dir), "dirs": dirs, "files": files})
}

func (s *Server) playGame(w http.ResponseWriter, r *http.Request) {
	if s.Games == nil {
		writeError(w, http.StatusNotFound, errNoGames)
		return
	}
	var body struct {
		Path    string `json:"path"`
		LagTest bool   `json:"lagTest"`
		// HostApp is a game on the game host, by its Steam app id.
		HostApp int `json:"hostApp"`
		// Steam plays Steam itself on the game host, in Big Picture.
		Steam  bool   `json:"steam"`
		Offer  string `json:"offer"`
		Direct bool   `json:"direct"`
	}
	if err := readJSON(r, &body); err != nil || (body.Offer == "") == !body.Direct {
		writeError(w, http.StatusBadRequest, "bad request")
		return
	}
	g := gamestream.Game{Owner: strconv.FormatInt(currentUser(r).ID, 10)}
	system, analog := "lagtest", false
	if body.HostApp > 0 || body.Steam {
		if s.GameHost == nil {
			writeError(w, http.StatusNotFound, "no game host is set up")
			return
		}
		g.Host, g.AppID, system, analog = s.GameHost, body.HostApp, "pc", true
		if body.Steam {
			g.AppID = gamehost.BigPicture
		}
	} else if body.LagTest {
		g.LagTest = true
	} else {
		st, err := os.Stat(body.Path)
		if err != nil || st.IsDir() {
			writeError(w, http.StatusBadRequest, "can't find that game")
			return
		}
		sys, ok := s.Games.SystemFor(body.Path)
		if !ok {
			writeError(w, http.StatusBadRequest, "no emulator on this server plays that game")
			return
		}
		g.System, g.Path, system, analog = sys, body.Path, sys.Key, sys.Analog
	}
	s.startGame(w, r, g, body.Offer, system, analog)
}

// gameHostGames lists the games the game host has installed.
func (s *Server) gameHostGames(w http.ResponseWriter, r *http.Request) {
	if s.GameHost == nil {
		writeError(w, http.StatusNotFound, "no game host is set up")
		return
	}
	games, err := s.GameHost.Games(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	if games == nil {
		games = []gamehost.Game{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"games": games})
}

// gameHostDesktop passes on the game host's desktop, a web page showing its
// screen where an admin signs in to Steam and installs games. It controls
// the whole computer, so it's for admins, from Matinee's own pages only.
func (s *Server) gameHostDesktop(w http.ResponseWriter, r *http.Request) {
	if s.GameHost == nil {
		writeError(w, http.StatusNotFound, "no game host is set up")
		return
	}
	if o := r.Header.Get("Origin"); o != "" {
		if u, err := url.Parse(o); err != nil || u.Host != r.Host {
			http.Error(w, "cross-site request", http.StatusForbidden)
			return
		}
	}
	s.GameHost.Desktop(r.PathValue("path")).ServeHTTP(w, r)
}

// playGameItem plays a game from a Games library, for anyone who can see
// it.
func (s *Server) playGameItem(w http.ResponseWriter, r *http.Request) {
	if s.Games == nil {
		writeError(w, http.StatusNotFound, errNoGames)
		return
	}
	it, ok := s.loadItem(w, r)
	if !ok {
		return
	}
	var body struct {
		Offer string `json:"offer"`
		// Direct asks for the WebSocket stream (streamGame) instead of
		// WebRTC, for players without it.
		Direct bool `json:"direct"`
	}
	if err := readJSON(r, &body); err != nil || (body.Offer == "") == !body.Direct {
		writeError(w, http.StatusBadRequest, "bad request")
		return
	}
	if it.Kind != "game" || it.Path == "" {
		writeError(w, http.StatusBadRequest, "not a game")
		return
	}
	if !filesAnswering(w, it) {
		return
	}
	sys, ok := s.Games.SystemForGame(it.Path, gamelib.SystemKey(it.PluginData))
	if !ok {
		writeError(w, http.StatusBadRequest, "no emulator on this server plays that game")
		return
	}
	g := gamestream.Game{Owner: strconv.FormatInt(currentUser(r).ID, 10), System: sys, Path: it.Path}
	s.startGame(w, r, g, body.Offer, sys.Key, sys.Analog)
}

func (s *Server) startGame(w http.ResponseWriter, r *http.Request, g gamestream.Game, offer, system string, analog bool) {
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	sess, answer, err := s.Games.Start(ctx, g, offer)
	if errors.Is(err, gamehost.ErrBusy) {
		writeError(w, http.StatusConflict, "the game host is already playing a game")
		return
	}
	if errors.Is(err, gamestream.ErrBusy) {
		writeError(w, http.StatusServiceUnavailable, "too many games are being played right now")
		return
	}
	if err != nil {
		s.Log.Warn("couldn't start game", "path", g.Path, "err", err)
		writeError(w, http.StatusBadRequest, "couldn't start the game: "+err.Error())
		return
	}
	res := map[string]any{"id": sess.ID, "answer": answer, "system": system, "analog": analog,
		"width": sess.Info.Width, "height": sess.Info.Height, "aspect": sess.Info.Aspect, "fps": sess.Info.FPS}
	if offer == "" {
		res["stream"] = "games/play/" + sess.ID + "/stream"
	}
	writeJSON(w, http.StatusOK, res)
}

// streamGame is the direct stream of a game started with direct set: a
// WebSocket carrying the picture, sound and buttons (gamestream/direct.go).
func (s *Server) streamGame(w http.ResponseWriter, r *http.Request) {
	if s.Games == nil {
		writeError(w, http.StatusNotFound, errNoGames)
		return
	}
	sess := s.Games.Get(r.PathValue("id"))
	if sess == nil || sess.Owner != strconv.FormatInt(currentUser(r).ID, 10) {
		writeError(w, http.StatusNotFound, "no such game")
		return
	}
	// The player is an app, not a page, so there's no origin to check.
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	defer conn.CloseNow()
	if err := sess.ServeDirect(r.Context(), conn); err != nil {
		s.Log.Debug("game stream ended", "session", sess.ID, "err", err)
	}
}

func (s *Server) stopGame(w http.ResponseWriter, r *http.Request) {
	if s.Games == nil {
		writeError(w, http.StatusNotFound, errNoGames)
		return
	}
	// Only the person playing, or an admin, can end a game.
	u := currentUser(r)
	if sess := s.Games.Get(r.PathValue("id")); sess != nil && (sess.Owner == strconv.FormatInt(u.ID, 10) || u.IsAdmin) {
		sess.Stop()
	}
	w.WriteHeader(http.StatusNoContent)
}

// corePlatform is the kind of emulator file this server runs, for telling
// an admin which to download: libretro's own names for Linux builds.
func corePlatform() string {
	switch runtime.GOARCH {
	case "amd64":
		return "Linux x86_64"
	case "arm64":
		return "Linux aarch64"
	case "arm":
		return "Linux armhf"
	}
	return "Linux " + runtime.GOARCH
}

func (s *Server) gameEmulators(w http.ResponseWriter, r *http.Request) {
	if s.Games == nil {
		writeError(w, http.StatusNotFound, errNoGames)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"emulators": s.Games.Emulators(), "platform": corePlatform()})
}

// addGameEmulator takes an emulator an admin uploaded: the file itself as
// the body, its name in ?file=.
func (s *Server) addGameEmulator(w http.ResponseWriter, r *http.Request) {
	if s.Games == nil {
		writeError(w, http.StatusNotFound, errNoGames)
		return
	}
	file := r.URL.Query().Get("file")
	if file == "" {
		writeError(w, http.StatusBadRequest, "bad request")
		return
	}
	e, err := s.Games.AddEmulator(r.Context(), file, http.MaxBytesReader(w, r.Body, gamestream.MaxEmulatorSize))
	if errors.Is(err, gamestream.ErrNotEmulator) {
		reason := strings.TrimPrefix(strings.TrimPrefix(err.Error(), gamestream.ErrNotEmulator.Error()), ": ")
		writeErrorWith(w, http.StatusBadRequest, "that file isn't an emulator this server can run: {{reason}}", map[string]string{"reason": reason})
		return
	}
	if err != nil {
		s.Log.Warn("couldn't add emulator", "file", file, "err", err)
		writeError(w, http.StatusInternalServerError, "couldn't save the emulator")
		return
	}
	s.Log.Info("emulator added", "file", e.File, "name", e.Name, "by", currentUser(r).Username)
	writeJSON(w, http.StatusOK, e)
}

func (s *Server) removeGameEmulator(w http.ResponseWriter, r *http.Request) {
	if s.Games == nil {
		writeError(w, http.StatusNotFound, errNoGames)
		return
	}
	if err := s.Games.RemoveEmulator(r.PathValue("file")); err != nil {
		writeError(w, http.StatusNotFound, "can't find that emulator")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
