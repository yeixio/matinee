package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/yeixio/matinee/internal/analyze"
	"github.com/yeixio/matinee/internal/intro"
	"github.com/yeixio/matinee/internal/probe"
	"github.com/yeixio/matinee/internal/store"
)

// getAnalysis shows admins how far the background media analysis is.
func (s *Server) getAnalysis(w http.ResponseWriter, r *http.Request) {
	if s.Analyzer == nil {
		writeError(w, http.StatusNotFound, "media analysis isn't running")
		return
	}
	st, err := s.Analyzer.Status(r.Context())
	if err != nil {
		s.serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// putAnalysis sets how hard the analysis works: gentle, normal or off.
func (s *Server) putAnalysis(w http.ResponseWriter, r *http.Request) {
	if s.Analyzer == nil {
		writeError(w, http.StatusNotFound, "media analysis isn't running")
		return
	}
	var in struct {
		Mode   analyze.Mode `json:"mode"`
		Intros *bool        `json:"intros"` // look for TV episodes' intros
	}
	if err := readJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "bad request")
		return
	}
	if (in.Mode != "" || in.Intros == nil) && in.Mode != analyze.Gentle && in.Mode != analyze.Normal && in.Mode != analyze.Off {
		writeError(w, http.StatusBadRequest, "mode must be gentle, normal or off")
		return
	}
	if in.Mode != "" {
		if err := s.Analyzer.SetMode(r.Context(), in.Mode); err != nil {
			s.serverError(w, err)
			return
		}
	}
	if in.Intros != nil {
		if err := s.Analyzer.SetIntrosOn(r.Context(), *in.Intros); err != nil {
			s.serverError(w, err)
			return
		}
	}
	s.getAnalysis(w, r)
}

// analyzeItem has one file analyzed again soon, for an admin who replaced
// it or doubts what it says.
func (s *Server) analyzeItem(w http.ResponseWriter, r *http.Request) {
	it, ok := s.loadItem(w, r)
	if !ok {
		return
	}
	if !it.Playable {
		writeError(w, http.StatusBadRequest, "not a file")
		return
	}
	if s.Analyzer == nil {
		writeError(w, http.StatusNotFound, "media analysis isn't running")
		return
	}
	if err := s.Analyzer.Reanalyze(r.Context(), it.ID); err != nil {
		s.serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// mediaInfo returns a file's analysis, nil until it's analyzed. stale is
// true when the file changed since and a new analysis is on its way.
func (s *Server) mediaInfo(r *http.Request, it store.Item) (mi *probe.MediaInfo, stale bool) {
	raw, current, ok, err := s.Store.MediaInfo(r.Context(), it.ID, probe.MediaInfoVersion)
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			s.Log.Warn("media info", "item", it.ID, "err", err)
		}
		return nil, false
	}
	if !ok {
		return nil, false
	}
	var v probe.MediaInfo
	if json.Unmarshal([]byte(raw), &v) != nil {
		return nil, false
	}
	return &v, !current
}

// introView is where an episode's intro plays.
type introView struct {
	StartMs int64 `json:"startMs"`
	EndMs   int64 `json:"endMs"`
	// Source is chapters when the file's chapters say so, audio when it was
	// found by matching the season's episodes.
	Source string `json:"source"`
}

// intro says where an episode's intro is: its chapters win, then what the
// intro detector found. nil when neither knows.
func (s *Server) intro(ctx context.Context, it store.Item, mi *probe.MediaInfo) *introView {
	if it.Kind != "episode" {
		return nil
	}
	if mi != nil && mi.IntroEndMs > mi.IntroStartMs {
		return &introView{StartMs: mi.IntroStartMs, EndMs: mi.IntroEndMs, Source: "chapters"}
	}
	start, end, ok, err := s.Store.Intro(ctx, it.ID, intro.Version)
	if err != nil {
		s.Log.Warn("intro", "item", it.ID, "err", err)
	}
	if !ok {
		return nil
	}
	return &introView{StartMs: start, EndMs: end, Source: "audio"}
}
