package api

import (
	"net/http"
)

// stats is a short summary for a dashboard, such as Homepage's custom API
// widget (GET /stats): who's watching, what the libraries hold, and what's
// waiting on an admin. It takes the same bearer token as /metrics, so it's
// off until an admin turns metrics on.
type stats struct {
	Playing    int `json:"playing"`    // players playing now
	Transcodes int `json:"transcodes"` // transcodes running now
	Users      int `json:"users"`      // accounts, without guests and profiles
	Libraries  int `json:"libraries"`
	Movies     int `json:"movies"`
	Shows      int `json:"shows"`
	Episodes   int `json:"episodes"`
	Artists    int `json:"artists"`
	Albums     int `json:"albums"`
	Tracks     int `json:"tracks"`
	Audiobooks int `json:"audiobooks"`
	Podcasts   int `json:"podcasts"`
	Requests   int `json:"requests"` // waiting for an admin
	Issues     int `json:"issues"`   // reported and not resolved
	Alerts     int `json:"alerts"`   // alerts that are true now
}

func (s *Server) stats(w http.ResponseWriter, r *http.Request) {
	if !s.metricsAllowed(w, r) {
		return
	}
	ctx := r.Context()
	c, err := s.Store.Counts(ctx)
	if err != nil {
		s.serverError(w, err)
		return
	}
	n := c.Items
	st := stats{
		Users: c.Users, Libraries: c.Libraries,
		Movies: n["movie"], Shows: n["show"], Episodes: n["episode"],
		Artists: n["artist"], Albums: n["album"], Tracks: n["track"],
		Audiobooks: n["book"], Podcasts: n["podcast"],
		Alerts: len(s.alerts().Firing()),
	}
	st.Playing, st.Transcodes = s.StreamCounts()
	st.Requests, st.Issues, _ = s.Store.Waiting(ctx)
	writeJSON(w, http.StatusOK, st)
}
