package api

import (
	"context"
	"net/http"

	"github.com/yeixio/matinee/internal/store"
)

// Everyone has their own watch list. Devices at home that haven't signed in
// share the Home account's, the same way they share its progress.

const watchListLimit = 500

func (s *Server) getWatchList(w http.ResponseWriter, r *http.Request) {
	items, err := s.Store.WatchList(r.Context(), currentUser(r).ID, watchListLimit)
	if err != nil {
		s.serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) addToWatchList(w http.ResponseWriter, r *http.Request) {
	it, ok := s.loadItem(w, r)
	if !ok {
		return
	}
	if !store.WatchListKinds[it.Kind] {
		writeError(w, http.StatusBadRequest, "only movies and shows go on a watch list")
		return
	}
	if err := s.Store.AddToWatchList(r.Context(), currentUser(r).ID, it.ID); err != nil {
		s.serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) removeFromWatchList(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	if err := s.Store.RemoveFromWatchList(r.Context(), currentUser(r).ID, id); err != nil {
		s.serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// watched takes a movie off the watch list once it's been watched. Shows
// stay until they're removed, since there's usually more to watch.
func (s *Server) watched(ctx context.Context, userID int64, it store.Item, played bool) error {
	if !played || it.Kind != "movie" {
		return nil
	}
	return s.Store.RemoveFromWatchList(ctx, userID, it.ID)
}
