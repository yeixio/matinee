package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/yeixio/matinee/internal/store"
	"github.com/yeixio/matinee/internal/together"
)

// Watch together (see internal/together and docs/watch-together.md).
//
// A player starts a session with POST /together and shows its code. Others
// join with POST /together/{code}/join, then everyone follows the session
// with GET /together/{code}/state?client=…&since=<version>, which waits for
// the next change. Playing, pausing, seeking and switching titles go to
// POST /together/{code}/actions. Everything goes through this server.

type togetherRequest struct {
	ClientID   string `json:"clientId"`
	Device     string `json:"device"`
	Platform   string `json:"platform"`
	ItemID     int64  `json:"itemId"`
	PositionMs int64  `json:"positionMs"`
	Paused     bool   `json:"paused"`
	Action     string `json:"action"`
	// Resume starts a session again under its old code, after the server
	// restarted and lost it.
	Resume string `json:"resume"`
}

func (s *Server) togetherPerson(w http.ResponseWriter, r *http.Request, req togetherRequest) (together.Person, bool) {
	if !clientIDPattern.MatchString(req.ClientID) {
		writeError(w, http.StatusBadRequest, "bad client id")
		return together.Person{}, false
	}
	u := currentUser(r)
	return together.Person{
		UserID: u.ID, Name: u.Username, ClientID: req.ClientID,
		Device: clip(strings.TrimSpace(req.Device), 80), Platform: clip(strings.ToLower(strings.TrimSpace(req.Platform)), 20),
	}, true
}

// togetherItem checks the signed-in person may watch the item.
func (s *Server) togetherItem(w http.ResponseWriter, r *http.Request, id int64) bool {
	it, err := s.Store.Item(r.Context(), currentUser(r).ID, id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusForbidden, "that isn't in a library shared with you")
		return false
	case err != nil:
		s.serverError(w, err)
		return false
	case !it.Playable:
		writeError(w, http.StatusBadRequest, "not a playable item")
		return false
	}
	return true
}

func (s *Server) togetherError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, together.ErrNoSession):
		writeError(w, http.StatusNotFound, "that Watch together code doesn't work; it may have ended")
	case errors.Is(err, together.ErrNotMember):
		writeError(w, http.StatusForbidden, "join the session first")
	case errors.Is(err, together.ErrFull):
		writeError(w, http.StatusConflict, "this Watch together session is full")
	case errors.Is(err, together.ErrExists):
		writeError(w, http.StatusConflict, "that session is still going; join it")
	case errors.Is(err, together.ErrTooMany):
		writeError(w, http.StatusServiceUnavailable, "too many Watch together sessions right now")
	case errors.Is(err, together.ErrAction):
		writeError(w, http.StatusBadRequest, "unknown action")
	default:
		s.serverError(w, err)
	}
}

func (s *Server) startTogether(w http.ResponseWriter, r *http.Request) {
	var req togetherRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	p, ok := s.togetherPerson(w, r, req)
	if !ok || !s.togetherItem(w, r, req.ItemID) {
		return
	}
	st, err := s.together().Start(p, req.ItemID, req.PositionMs, req.Paused, req.Resume)
	if err != nil {
		s.togetherError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, st)
}

// peekTogether is a session as someone about to join sees it.
func (s *Server) peekTogether(w http.ResponseWriter, r *http.Request) {
	st, err := s.together().Peek(r.PathValue("code"))
	if err != nil {
		s.togetherError(w, err)
		return
	}
	// Say whether this person can watch it, so the join screen can tell them.
	_, err = s.Store.Item(r.Context(), currentUser(r).ID, st.ItemID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		s.serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		together.State
		CanWatch bool `json:"canWatch"`
	}{st, err == nil})
}

func (s *Server) joinTogether(w http.ResponseWriter, r *http.Request) {
	var req togetherRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	p, ok := s.togetherPerson(w, r, req)
	if !ok {
		return
	}
	st, err := s.together().Peek(r.PathValue("code"))
	if err != nil {
		s.togetherError(w, err)
		return
	}
	if !s.togetherItem(w, r, st.ItemID) {
		return
	}
	if st, err = s.together().Join(r.PathValue("code"), p); err != nil {
		s.togetherError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) actTogether(w http.ResponseWriter, r *http.Request) {
	var req togetherRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	p, ok := s.togetherPerson(w, r, req)
	if !ok {
		return
	}
	if req.Action == together.ActionItem && !s.togetherItem(w, r, req.ItemID) {
		return
	}
	st, err := s.together().Act(r.PathValue("code"), p, req.Action, req.PositionMs, req.ItemID)
	if err != nil {
		s.togetherError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// togetherState waits for the session to change past ?since=, up to
// ?wait= seconds (25 at most; 0 answers at once).
func (s *Server) togetherState(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	p, ok := s.togetherPerson(w, r, togetherRequest{ClientID: q.Get("client")})
	if !ok {
		return
	}
	since, _ := strconv.ParseInt(q.Get("since"), 10, 64)
	wait := together.MaxWait
	if v := q.Get("wait"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			writeError(w, http.StatusBadRequest, "bad wait")
			return
		}
		wait = time.Duration(n) * time.Second
	}
	st, err := s.together().Wait(r.Context(), r.PathValue("code"), p, since, wait)
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		s.togetherError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) leaveTogether(w http.ResponseWriter, r *http.Request) {
	p, ok := s.togetherPerson(w, r, togetherRequest{ClientID: r.PathValue("client")})
	if !ok {
		return
	}
	if err := s.together().Leave(r.PathValue("code"), p); err != nil && !errors.Is(err, together.ErrNoSession) {
		s.togetherError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) endTogether(w http.ResponseWriter, r *http.Request) {
	var req togetherRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	p, ok := s.togetherPerson(w, r, req)
	if !ok {
		return
	}
	if err := s.together().End(r.PathValue("code"), p); err != nil {
		s.togetherError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) together() *together.Hub {
	s.togetherOnce.Do(func() {
		if s.Together == nil {
			s.Together = together.New()
		}
	})
	return s.Together
}
