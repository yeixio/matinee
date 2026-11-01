package api

import (
	"crypto/rand"
	"net/http"
)

// bootID names this run of the server. A player that lost the server tells
// from it, once the server answers again, whether the server restarted (a
// new bootID) or only the network dropped (the same one); a new version
// besides means it was updated.
var bootID = rand.Text()[:12]

// HeaderRestarting is on every answer once the server is shutting down, so
// a player hears about it from whatever it asks next.
const HeaderRestarting = "Matinee-Restarting"

// Drain is the server starting to shut down, for an update or a restart: it
// stops counting as ready, players waiting for commands hear at once that
// it's restarting, and every answer says so too. Requests keep being served.
func (s *Server) Drain() {
	s.draining.Store(true)
	s.live().Close()
	s.together().Close()
}

// serverState is what /health says: "ok", or "restarting" while draining.
func (s *Server) serverState() string {
	if s.draining.Load() {
		return "restarting"
	}
	return "ok"
}

func (s *Server) markRestarting(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.draining.Load() {
			w.Header().Set(HeaderRestarting, "1")
		}
		h.ServeHTTP(w, r)
	})
}
