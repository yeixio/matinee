package api

import (
	"errors"
	"net/http"

	"github.com/yeixio/matinee/internal/auth"
	"github.com/yeixio/matinee/internal/store"
)

// ---- passwords and accounts ----

// changeMyPassword changes the signed-in user's password after checking the
// current one, and signs out their other devices.
func (s *Server) changeMyPassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Current string `json:"current"`
		New     string `json:"new"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	me := currentUser(r)
	if !auth.CheckPassword(me, req.Current) {
		writeError(w, http.StatusForbidden, "your current password is wrong")
		return
	}
	if msg := validatePassword(req.New); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	if !s.setPassword(w, r, me.ID, req.New, auth.Token(r)) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// resetPassword lets an admin set someone's password without knowing the
// old one. Every device that person signed in on is signed out.
func (s *Server) resetPassword(w http.ResponseWriter, r *http.Request) {
	u, ok := s.pathUser(w, r)
	if !ok {
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if msg := validatePassword(req.Password); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	// An admin resetting their own password keeps this session.
	keep := ""
	if u.ID == currentUser(r).ID {
		keep = auth.Token(r)
	}
	if !s.setPassword(w, r, u.ID, req.Password, keep) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// setPassword saves a new password and signs out every session of the user
// but keep. It writes the error response when it fails.
func (s *Server) setPassword(w http.ResponseWriter, r *http.Request, userID int64, password, keep string) bool {
	hash, err := auth.HashPassword(password)
	if err != nil {
		s.serverError(w, err)
		return false
	}
	if err := s.Store.SetPassword(r.Context(), userID, hash); err != nil {
		s.userError(w, err)
		return false
	}
	if err := s.Store.DeleteOtherSessions(r.Context(), userID, keep); err != nil {
		s.serverError(w, err)
		return false
	}
	return true
}

// updateUser makes an account an admin or not, or a friend who sees only
// some libraries (see friends.go).
func (s *Server) updateUser(w http.ResponseWriter, r *http.Request) {
	u, ok := s.pathUser(w, r)
	if !ok {
		return
	}
	var req struct {
		IsAdmin   *bool    `json:"isAdmin"`
		Friend    *bool    `json:"friend"`
		Libraries *[]int64 `json:"libraries"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	friend, admin := u.Friend, u.IsAdmin
	if req.Friend != nil {
		friend = *req.Friend
	}
	if req.IsAdmin != nil {
		admin = *req.IsAdmin
	}
	if friend && admin {
		writeError(w, http.StatusConflict, "admins see every library; make them a regular account first")
		return
	}
	setAdmin := func() bool {
		if req.IsAdmin == nil {
			return true
		}
		if err := s.Store.SetAdmin(r.Context(), u.ID, admin); err != nil {
			s.userError(w, err)
			return false
		}
		u.IsAdmin = admin
		return true
	}
	// An admin stops being one before becoming a friend.
	if !admin && !setAdmin() {
		return
	}
	if req.Friend != nil || req.Libraries != nil {
		var libs []int64
		if req.Libraries != nil {
			libs = *req.Libraries
		} else if friend {
			all, err := s.Store.FriendLibraries(r.Context())
			if err != nil {
				s.serverError(w, err)
				return
			}
			libs = all[u.ID]
		}
		if !s.setFriend(w, r, u, friend, libs) {
			return
		}
		u.Friend = friend
	}
	if admin && !setAdmin() {
		return
	}
	writeJSON(w, http.StatusOK, u)
}

// deleteUser removes an account with its sessions, watch history and the
// rest of what's theirs. Admins can't remove themselves.
func (s *Server) deleteUser(w http.ResponseWriter, r *http.Request) {
	u, ok := s.pathUser(w, r)
	if !ok {
		return
	}
	if u.ID == currentUser(r).ID {
		writeError(w, http.StatusBadRequest, "you can't remove your own account")
		return
	}
	if err := s.Store.DeleteUser(r.Context(), u.ID); err != nil {
		s.userError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// pathUser looks up the account in the URL. It writes the error response
// when there isn't one.
func (s *Server) pathUser(w http.ResponseWriter, r *http.Request) (store.User, bool) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusNotFound, "no such user")
		return store.User{}, false
	}
	u, err := s.Store.UserByID(r.Context(), id)
	if err != nil {
		s.userError(w, err)
		return u, false
	}
	return u, true
}

// userError answers for a store error from the account routes.
func (s *Server) userError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "no such user")
	case errors.Is(err, store.ErrLastAdmin):
		writeError(w, http.StatusConflict, err.Error())
	default:
		s.serverError(w, err)
	}
}
