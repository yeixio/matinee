package api

import (
	"net/http"

	"github.com/yeixio/matinee/internal/store"
)

// Each person can rate titles, songs, books and podcasts, from half a star
// to five stars (1 to 10). Items carry the person's own rating as myRating.

// setRating rates an item: {"rating": 1–10}.
func (s *Server) setRating(w http.ResponseWriter, r *http.Request) {
	it, ok := s.loadItem(w, r)
	if !ok {
		return
	}
	if !store.RatableKinds[it.Kind] {
		writeError(w, http.StatusBadRequest, "this can't be rated")
		return
	}
	var body struct {
		Rating int `json:"rating"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "bad request")
		return
	}
	if body.Rating < 1 || body.Rating > 10 {
		writeError(w, http.StatusBadRequest, "a rating is from 1 to 10")
		return
	}
	if err := s.Store.SetRating(r.Context(), currentUser(r).ID, it.ID, body.Rating); err != nil {
		s.serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// clearRating takes back a rating.
func (s *Server) clearRating(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	if err := s.Store.ClearRating(r.Context(), currentUser(r).ID, id); err != nil {
		s.serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
