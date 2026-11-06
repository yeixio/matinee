package api

import (
	"errors"
	"net/http"

	"github.com/yeixio/matinee/internal/update"
)

// Settings → Updates: newer versions from GitHub (internal/update), and
// installing them where this server can replace itself.

func (s *Server) needUpdates(w http.ResponseWriter) bool {
	if s.Updates == nil {
		writeError(w, http.StatusNotFound, "this server doesn't look for updates")
		return false
	}
	return true
}

func (s *Server) getUpdates(w http.ResponseWriter, r *http.Request) {
	if !s.needUpdates(w) {
		return
	}
	writeJSON(w, http.StatusOK, s.Updates.Status(r.Context()))
}

func (s *Server) putUpdateSettings(w http.ResponseWriter, r *http.Request) {
	if !s.needUpdates(w) {
		return
	}
	var in update.Settings
	if err := readJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	old := s.Updates.Settings(r.Context())
	if err := s.Updates.SaveSettings(r.Context(), in); errors.Is(err, update.ErrBadSettings) {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	} else if err != nil {
		s.serverError(w, err)
		return
	}
	// A new channel, or turning checks on, looks at once.
	if in.Mode != update.ModeOff && (in.Channel != old.Channel || old.Mode == update.ModeOff) {
		s.Updates.Check(r.Context()) // the status says why it failed
	}
	writeJSON(w, http.StatusOK, s.Updates.Status(r.Context()))
}

func (s *Server) checkUpdates(w http.ResponseWriter, r *http.Request) {
	if !s.needUpdates(w) {
		return
	}
	s.Updates.Check(r.Context()) // the status says why it failed
	writeJSON(w, http.StatusOK, s.Updates.Status(r.Context()))
}

func (s *Server) applyUpdate(w http.ResponseWriter, r *http.Request) {
	if !s.needUpdates(w) {
		return
	}
	switch err := s.Updates.Apply(); {
	case errors.Is(err, update.ErrCantApply), errors.Is(err, update.ErrNothingNew), errors.Is(err, update.ErrBusy):
		writeError(w, http.StatusConflict, err.Error())
		return
	case err != nil:
		s.serverError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, s.Updates.Status(r.Context()))
}
