package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/yeixio/matinee/internal/analyze"
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
		Mode analyze.Mode `json:"mode"`
	}
	if err := readJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "bad request")
		return
	}
	if in.Mode != analyze.Gentle && in.Mode != analyze.Normal && in.Mode != analyze.Off {
		writeError(w, http.StatusBadRequest, "mode must be gentle, normal or off")
		return
	}
	if err := s.Analyzer.SetMode(r.Context(), in.Mode); err != nil {
		s.serverError(w, err)
		return
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
