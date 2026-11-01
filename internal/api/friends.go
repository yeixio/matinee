package api

import (
	"cmp"
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/yeixio/matinee/internal/auth"
	"github.com/yeixio/matinee/internal/store"
)

// Friends and invites (see store/friends.go and docs/guide friends page).
//
// An admin makes an invite with POST /invites, choosing the libraries the
// friend will see, and sends its link. The link opens /join/{code} in the
// web player, which asks GET /join/{code} what the invite is and makes the
// account with POST /join/{code}. The friend is signed in straight away.

const (
	inviteDefaultDays = 7
	inviteMaxDays     = 30
)

type inviteView struct {
	store.Invite
	URL string `json:"url"`
	// State is "open", "used" or "expired".
	State string `json:"state"`
}

func (s *Server) inviteView(r *http.Request, inv store.Invite) inviteView {
	v := inviteView{Invite: inv, URL: s.inviteBase(r) + "/join/" + inv.Code, State: "open"}
	switch {
	case inv.UsedAt != 0:
		v.State = "used"
	case inv.ExpiresAt <= time.Now().Unix():
		v.State = "expired"
	}
	return v
}

// inviteBase is the address an invite link points at: the one that works
// from outside when there is one, since friends are rarely at home.
func (s *Server) inviteBase(r *http.Request) string {
	return strings.TrimRight(cmp.Or(s.remoteURL(r.Context()), baseURL(r)), "/")
}

func (s *Server) listInvites(w http.ResponseWriter, r *http.Request) {
	invites, err := s.Store.Invites(r.Context())
	if err != nil {
		s.serverError(w, err)
		return
	}
	out := make([]inviteView, 0, len(invites))
	for _, inv := range invites {
		out = append(out, s.inviteView(r, inv))
	}
	writeJSON(w, http.StatusOK, map[string]any{"invites": out, "remoteReady": s.remoteReady(r.Context())})
}

func (s *Server) createInvite(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name      string  `json:"name"`
		Libraries []int64 `json:"libraries"`
		Days      int     `json:"days"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if len(req.Libraries) == 0 {
		writeError(w, http.StatusBadRequest, "pick at least one library to share")
		return
	}
	libs, err := s.Store.Libraries(r.Context())
	if err != nil {
		s.serverError(w, err)
		return
	}
	known := map[int64]bool{}
	for _, l := range libs {
		known[l.ID] = true
	}
	for _, id := range req.Libraries {
		if !known[id] {
			writeError(w, http.StatusBadRequest, "no such library")
			return
		}
	}
	days := cmp.Or(req.Days, inviteDefaultDays)
	if days < 1 || days > inviteMaxDays {
		writeError(w, http.StatusBadRequest, "invites last 1 to 30 days")
		return
	}
	expires := time.Now().Add(time.Duration(days) * 24 * time.Hour).Unix()
	inv, err := s.Store.CreateInvite(r.Context(), currentUser(r).ID, clip(strings.TrimSpace(req.Name), 60), req.Libraries, expires)
	if err != nil {
		s.serverError(w, err)
		return
	}
	inv.CreatedBy = currentUser(r).Username
	writeJSON(w, http.StatusCreated, s.inviteView(r, inv))
}

func (s *Server) deleteInvite(w http.ResponseWriter, r *http.Request) {
	if err := s.Store.DeleteInvite(r.Context(), r.PathValue("code")); errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "no such invite")
		return
	} else if err != nil {
		s.serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// inviteKeys is what a guess at an invite code counts against: the same
// guard as wrong passwords, so codes can't be tried one after another.
func (s *Server) inviteKeys(r *http.Request) ([]string, bool) {
	keys, home := s.signInKeys(r, "invite")
	return keys[1:], home // the address only; everyone shares "invite"
}

// lookUpInvite finds the invite in the URL, writing the error response when
// it can't be used.
func (s *Server) lookUpInvite(w http.ResponseWriter, r *http.Request) (store.Invite, bool) {
	keys, home := s.inviteKeys(r)
	if wait := s.signIns.locked(keys); wait > 0 && !home {
		tooManySignIns(w, wait)
		return store.Invite{}, false
	}
	inv, err := s.Store.InviteByCode(r.Context(), r.PathValue("code"))
	switch {
	case errors.Is(err, store.ErrNotFound):
		if !home {
			s.signIns.record(keys, false)
		}
		writeError(w, http.StatusNotFound, "this invite link doesn't work; ask for another")
	case errors.Is(err, store.ErrInviteUsed):
		writeError(w, http.StatusGone, "this invite was already used")
	case errors.Is(err, store.ErrInviteExpired):
		writeError(w, http.StatusGone, "this invite has expired; ask for another")
	case err != nil:
		s.serverError(w, err)
	default:
		return inv, true
	}
	return inv, false
}

// joinInfo tells the invite page whose server it is and what's shared.
func (s *Server) joinInfo(w http.ResponseWriter, r *http.Request) {
	inv, ok := s.lookUpInvite(w, r)
	if !ok {
		return
	}
	libs, err := s.Store.Libraries(r.Context())
	if err != nil {
		s.serverError(w, err)
		return
	}
	shared := []map[string]any{}
	for _, l := range libs {
		for _, id := range inv.Libraries {
			if id == l.ID {
				shared = append(shared, map[string]any{"id": l.ID, "name": l.Name, "kind": l.Kind})
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"server":    cmp.Or(s.Name, "Matinee"),
		"invitedBy": inv.CreatedBy,
		"name":      inv.Name,
		"libraries": shared,
		"expiresAt": inv.ExpiresAt,
		// Where the apps reach this server; see joinApps in the guide.
		"remoteURL": s.remoteURL(r.Context()),
	})
}

// acceptInvite makes the friend's account and signs them in.
func (s *Server) acceptInvite(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if msg := req.validate(); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	if len([]rune(req.Username)) > 40 {
		writeError(w, http.StatusBadRequest, "usernames are at most 40 characters")
		return
	}
	if _, ok := s.lookUpInvite(w, r); !ok {
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		s.serverError(w, err)
		return
	}
	u, err := s.Store.AcceptInvite(r.Context(), r.PathValue("code"), req.Username, hash)
	switch {
	case errors.Is(err, store.ErrUsernameTaken):
		writeError(w, http.StatusConflict, "that username is taken")
		return
	case errors.Is(err, store.ErrInviteUsed):
		writeError(w, http.StatusGone, "this invite was already used")
		return
	case errors.Is(err, store.ErrInviteExpired):
		writeError(w, http.StatusGone, "this invite has expired; ask for another")
		return
	case err != nil:
		s.serverError(w, err)
		return
	}
	s.Log.Info("friend joined", "user", u.Username)
	s.login(w, r.WithContext(context.WithValue(r.Context(), credsKey{}, req)))
}

// friendView is an account as the People settings show it.
type friendView struct {
	store.User
	// Libraries are what a friend sees; empty for everyone else.
	Libraries []int64 `json:"libraries,omitempty"`
}

// setFriend limits an account to some libraries, or lifts the limit. It's
// part of PATCH /users/{id}.
func (s *Server) setFriend(w http.ResponseWriter, r *http.Request, u store.User, friend bool, libraries []int64) bool {
	if friend && len(libraries) == 0 {
		writeError(w, http.StatusBadRequest, "pick at least one library to share")
		return false
	}
	err := s.Store.SetFriend(r.Context(), u.ID, friend, libraries)
	if errors.Is(err, store.ErrAdminFriend) {
		writeError(w, http.StatusConflict, "admins see every library; make them a regular account first")
		return false
	}
	if err != nil {
		s.userError(w, err)
		return false
	}
	return true
}
