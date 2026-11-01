package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/yeixio/matinee/internal/store"
)

// Auto-play: when an episode's credits start (or near its end), players
// show an "Up next" card for the next episode with a countdown, or after a
// show's last episode a similar title. Each person picks whether that
// happens and whether to skip the countdown; see docs/autoplay.md.

type nextView struct {
	// Next is the episode to play next, crossing into the next season.
	Next *store.Item `json:"next,omitempty"`
	// Similar is offered instead after a show's last episode. It's a movie
	// or show, so players open its page rather than play it.
	Similar *store.Item `json:"similar,omitempty"`
}

func (s *Server) itemNext(w http.ResponseWriter, r *http.Request) {
	it, ok := s.loadItem(w, r)
	if !ok {
		return
	}
	ctx, user := r.Context(), currentUser(r)
	v := nextView{}
	next, err := s.Store.NextEpisode(ctx, user.ID, it)
	switch {
	case err == nil:
		v.Next = &next
	case !errors.Is(err, store.ErrNotFound):
		s.serverError(w, err)
		return
	case it.Kind == "episode":
		if show, ok := s.showOf(r, it); ok {
			similar, err := s.Store.Similar(ctx, user.ID, show)
			if err == nil {
				v.Similar = &similar
			} else if !errors.Is(err, store.ErrNotFound) {
				s.serverError(w, err)
				return
			}
		}
	}
	writeJSON(w, http.StatusOK, v)
}

// showOf finds an episode's show: its season's parent.
func (s *Server) showOf(r *http.Request, ep store.Item) (store.Item, bool) {
	ctx, user := r.Context(), currentUser(r)
	if ep.ParentID == nil {
		return store.Item{}, false
	}
	season, err := s.Store.Item(ctx, user.ID, *ep.ParentID)
	if err != nil || season.ParentID == nil {
		return store.Item{}, false
	}
	show, err := s.Store.Item(ctx, user.ID, *season.ParentID)
	return show, err == nil && show.Kind == "show"
}

// playbackPrefs are a person's auto-play choices. They follow the person to
// every device; on the home network without signing in that's the shared
// Home account (or the picked profile).
type playbackPrefs struct {
	// AutoPlay offers the next episode with a countdown. On by default.
	AutoPlay bool `json:"autoPlay"`
	// Binge skips the countdown: the next episode starts as soon as the
	// credits do (or the episode ends). Only matters with AutoPlay on.
	Binge bool `json:"binge"`
	// SkipIntros jumps past an episode's intro by itself, instead of
	// offering a Skip intro button. Off by default.
	SkipIntros bool `json:"skipIntros"`
}

var defaultPlayback = playbackPrefs{AutoPlay: true}

func userPlaybackKey(userID int64) string { return "playback:user:" + strconv.FormatInt(userID, 10) }

func (s *Server) myPlayback(r *http.Request) (playbackPrefs, error) {
	raw, err := s.Store.Setting(r.Context(), userPlaybackKey(currentUser(r).ID))
	if err != nil {
		return defaultPlayback, err
	}
	p := defaultPlayback
	if raw != "" && json.Unmarshal([]byte(raw), &p) != nil {
		p = defaultPlayback
	}
	return p, nil
}

func (s *Server) getMyPlayback(w http.ResponseWriter, r *http.Request) {
	p, err := s.myPlayback(r)
	if err != nil {
		s.serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

// putMyPlayback changes the choices sent and keeps the rest, so an app that
// doesn't know a newer one leaves it alone.
func (s *Server) putMyPlayback(w http.ResponseWriter, r *http.Request) {
	p, err := s.myPlayback(r)
	if err != nil {
		s.serverError(w, err)
		return
	}
	if err := readJSON(r, &p); err != nil {
		writeError(w, http.StatusBadRequest, "bad request")
		return
	}
	raw, _ := json.Marshal(p)
	if err := s.Store.SetSetting(r.Context(), userPlaybackKey(currentUser(r).ID), string(raw)); err != nil {
		s.serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}
