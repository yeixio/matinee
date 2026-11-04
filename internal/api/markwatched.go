package api

import (
	"context"
	"net/http"

	"github.com/yeixio/matinee/internal/store"
)

// Marking watched by hand: a movie or an episode, or every episode of a
// season or a show, without playing it to the end.

// watchable are the episodes (or the movie) an item stands for: itself for
// a movie or episode, every episode under a season or show.
func (s *Server) watchable(ctx context.Context, userID int64, it store.Item) ([]store.Item, error) {
	switch it.Kind {
	case "movie", "episode":
		return []store.Item{it}, nil
	case "season", "show":
		kids, err := s.Store.Children(ctx, userID, it.ID)
		if err != nil {
			return nil, err
		}
		var out []store.Item
		for _, k := range kids {
			more, err := s.watchable(ctx, userID, k)
			if err != nil {
				return nil, err
			}
			out = append(out, more...)
		}
		return out, nil
	}
	return nil, nil
}

type watchedCount struct {
	// Total is how many there are to watch, and Played how many are watched.
	Total  int `json:"total"`
	Played int `json:"played"`
}

func countWatched(items []store.Item) watchedCount {
	c := watchedCount{Total: len(items)}
	for _, it := range items {
		if it.Played {
			c.Played++
		}
	}
	return c
}

func (s *Server) loadWatchable(w http.ResponseWriter, r *http.Request) ([]store.Item, bool) {
	it, ok := s.loadItem(w, r)
	if !ok {
		return nil, false
	}
	if it.Kind != "movie" && it.Kind != "episode" && it.Kind != "season" && it.Kind != "show" {
		writeError(w, http.StatusBadRequest, "only movies, episodes, seasons and shows can be marked watched")
		return nil, false
	}
	items, err := s.watchable(r.Context(), currentUser(r).ID, it)
	if err != nil {
		s.serverError(w, err)
		return nil, false
	}
	return items, true
}

// getWatched says how much of an item is watched.
func (s *Server) getWatched(w http.ResponseWriter, r *http.Request) {
	items, ok := s.loadWatchable(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, countWatched(items))
}

// setWatched marks an item, or everything in it, watched or not. Either
// way it starts again from the beginning.
func (s *Server) setWatched(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Played bool `json:"played"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	items, ok := s.loadWatchable(w, r)
	if !ok {
		return
	}
	u := currentUser(r)
	for i, it := range items {
		if err := s.Store.SetProgress(r.Context(), u.ID, it.ID, 0, req.Played); err != nil {
			s.serverError(w, err)
			return
		}
		if err := s.watched(r.Context(), u.ID, it, req.Played); err != nil {
			s.serverError(w, err)
			return
		}
		s.Plugins.Played(u, it, 0, req.Played)
		items[i].Played = req.Played
	}
	writeJSON(w, http.StatusOK, countWatched(items))
}
