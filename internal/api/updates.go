package api

import (
	"errors"
	"net/http"

	"github.com/yeixio/matinee/internal/update"
)

// Updates (internal/update), under Settings → Server health → Updates.

func (s *Server) writeUpdates(w http.ResponseWriter, r *http.Request) {
	if s.Updates == nil {
		writeJSON(w, http.StatusOK, update.Status{Install: update.Install{Kind: update.KindSource}, Settings: update.DefaultSettings})
		return
	}
	writeJSON(w, http.StatusOK, s.Updates.Status(r.Context()))
}

func (s *Server) getUpdates(w http.ResponseWriter, r *http.Request) { s.writeUpdates(w, r) }

func (s *Server) putUpdates(w http.ResponseWriter, r *http.Request) {
	var in update.Settings
	if err := readJSON(r, &in); err != nil || in.Channel != update.ChannelRelease && in.Channel != update.ChannelMain {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if s.Updates != nil {
		if _, err := s.Updates.SaveSettings(r.Context(), in); err != nil {
			s.serverError(w, err)
			return
		}
		s.Log.Info("update settings changed", "channel", in.Channel, "auto", in.Auto, "by", currentUser(r).Username)
	}
	s.writeUpdates(w, r)
}

func (s *Server) checkUpdates(w http.ResponseWriter, r *http.Request) {
	if s.Updates != nil {
		s.Updates.Check(r.Context())
	}
	s.writeUpdates(w, r)
}

// applyUpdate installs the update that was found. How it went is in the
// answer: applying while it's on its way, lastError if it failed.
func (s *Server) applyUpdate(w http.ResponseWriter, r *http.Request) {
	if s.Updates != nil {
		s.Log.Info("installing an update", "by", currentUser(r).Username)
		if err := s.Updates.Apply(r.Context()); err != nil && !errors.Is(err, update.ErrNothing) {
			s.Updates.Failed(err)
		}
	}
	s.writeUpdates(w, r)
}
