package api

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

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
		{"DELETE /games/play/{id}", adminOnly, s.stopGame},
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
		"playing": s.Games.Count()})
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
		Offer   string `json:"offer"`
	}
	if err := readJSON(r, &body); err != nil || body.Offer == "" {
		writeError(w, http.StatusBadRequest, "bad request")
		return
	}
	g := gamestream.Game{Owner: strconv.FormatInt(currentUser(r).ID, 10)}
	system := "lagtest"
	if body.LagTest {
		g.Core = gamestream.LagTest
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
		g.Core, g.Path, system = sys.Core, body.Path, sys.Key
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	sess, answer, err := s.Games.Start(ctx, g, body.Offer)
	if errors.Is(err, gamestream.ErrBusy) {
		writeError(w, http.StatusServiceUnavailable, "too many games are being played right now")
		return
	}
	if err != nil {
		s.Log.Warn("couldn't start game", "path", body.Path, "err", err)
		writeError(w, http.StatusBadRequest, "couldn't start the game: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": sess.ID, "answer": answer, "system": system,
		"width": sess.Info.Width, "height": sess.Info.Height, "aspect": sess.Info.Aspect, "fps": sess.Info.FPS})
}

func (s *Server) stopGame(w http.ResponseWriter, r *http.Request) {
	if s.Games == nil {
		writeError(w, http.StatusNotFound, errNoGames)
		return
	}
	if sess := s.Games.Get(r.PathValue("id")); sess != nil {
		sess.Stop()
	}
	w.WriteHeader(http.StatusNoContent)
}
