package api

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/yeixio/matinee/internal/auth"
	"github.com/yeixio/matinee/internal/rating"
	"github.com/yeixio/matinee/internal/store"
)

// Profiles let people who share a sign-in, or the home network's guest
// account, each keep their own progress, watch list and theme (see
// store.Profile). Devices ask "Who's watching?" from GET /profiles and
// switch with POST /profiles/select. Admins manage them.
//
// A device at home that hasn't signed in has no session to remember its
// pick, so picking a profile there starts one that only works from home
// and only for reader routes.

// Avatars are colours the apps draw a profile's initial on.
var avatars = []string{"coral", "amber", "lime", "teal", "sky", "violet", "pink", "slate"}

// How many wrong PINs lock a profile, and for how long.
const (
	pinTries    = 5
	pinLockTime = time.Minute
)

// pinGuard counts wrong PINs per profile.
type pinGuard struct {
	mu    sync.Mutex
	fails map[int64]int
	until map[int64]time.Time
}

// locked reports how long a profile is still locked for.
func (g *pinGuard) locked(id int64) time.Duration {
	g.mu.Lock()
	defer g.mu.Unlock()
	return time.Until(g.until[id])
}

func (g *pinGuard) record(id int64, ok bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.fails == nil {
		g.fails, g.until = map[int64]int{}, map[int64]time.Time{}
	}
	if ok {
		delete(g.fails, id)
		return
	}
	g.fails[id]++
	if g.fails[id] >= pinTries {
		g.fails[id] = 0
		g.until[id] = time.Now().Add(pinLockTime)
	}
}

// profileChoice is one entry in "Who's watching?".
type profileChoice struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Avatar    string `json:"avatar,omitempty"`
	KidsLevel int    `json:"kidsLevel,omitempty"`
	HasPIN    bool   `json:"hasPin"`
	// Account is true for the account itself, which comes first.
	Account bool `json:"account,omitempty"`
}

func choice(u store.User) profileChoice {
	return profileChoice{ID: u.ID, Name: u.Username, Avatar: u.Avatar, KidsLevel: u.KidsLevel, HasPIN: u.HasPIN, Account: u.AccountID == 0}
}

// account returns the account the current user is, or belongs to.
func (s *Server) account(ctx context.Context, u store.User) (store.User, error) {
	if u.AccountID == 0 {
		return u, nil
	}
	return s.Store.UserByID(ctx, u.AccountID)
}

// listProfiles answers "Who's watching?": the account and its profiles. An
// account without profiles has just itself, and the apps skip the question.
func (s *Server) listProfiles(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	acct, err := s.account(r.Context(), u)
	if err != nil {
		s.serverError(w, err)
		return
	}
	profiles, err := s.Store.Profiles(r.Context(), acct.ID)
	if err != nil {
		s.serverError(w, err)
		return
	}
	out := []profileChoice{choice(acct)}
	for _, p := range profiles {
		out = append(out, choice(p.User))
	}
	writeJSON(w, http.StatusOK, map[string]any{"current": u.ID, "profiles": out})
}

// selectProfile switches the device to another profile of the same account,
// checking its PIN when it has one.
func (s *Server) selectProfile(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID         int64  `json:"id"`
		PIN        string `json:"pin"`
		DeviceName string `json:"deviceName"`
		Platform   string `json:"platform"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	u := currentUser(r)
	target, err := s.Store.UserByID(r.Context(), req.ID)
	if errors.Is(err, store.ErrNotFound) || err == nil && target.Account() != u.Account() {
		writeError(w, http.StatusNotFound, "no such profile")
		return
	}
	if err != nil {
		s.serverError(w, err)
		return
	}
	if target.HasPIN && target.ID != u.ID {
		if wait := s.pins.locked(target.ID); wait > 0 {
			w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
			writeError(w, http.StatusTooManyRequests, "too many wrong PINs; try again in a minute")
			return
		}
		ok := auth.CheckPIN(target.PINHash(), req.PIN)
		s.pins.record(target.ID, ok)
		if !ok {
			writeError(w, http.StatusForbidden, "wrong PIN")
			return
		}
	}
	profileID := target.ID
	if target.AccountID == 0 {
		profileID = 0
	}
	token := auth.Token(r)
	if token == "" {
		// At home without signing in (requireUser let a reader route through
		// only for that). The guest account itself needs no session.
		if profileID == 0 {
			writeJSON(w, http.StatusOK, map[string]any{"user": target})
			return
		}
		token, err = s.Store.CreateHomeSession(r.Context(), u.Account(), profileID, auth.SessionTTL, requestDevice(r, req.DeviceName, req.Platform))
		if err != nil {
			s.serverError(w, err)
			return
		}
		setSessionCookie(w, r, token)
		writeJSON(w, http.StatusOK, map[string]any{"user": target, "token": token})
		return
	}
	if err := s.Store.SetSessionProfile(r.Context(), token, profileID); err != nil {
		s.serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": target})
}

// ---- managing profiles (admins) ----

type accountProfiles struct {
	Account  store.User      `json:"account"`
	Profiles []store.Profile `json:"profiles"`
}

// manageProfiles lists every account with its profiles, the home network's
// guest account first.
func (s *Server) manageProfiles(w http.ResponseWriter, r *http.Request) {
	guest, err := s.Store.HomeGuest(r.Context())
	if err != nil {
		s.serverError(w, err)
		return
	}
	users, err := s.Store.Users(r.Context())
	if err != nil {
		s.serverError(w, err)
		return
	}
	out := []accountProfiles{}
	for _, a := range append([]store.User{guest}, users...) {
		p, err := s.Store.Profiles(r.Context(), a.ID)
		if err != nil {
			s.serverError(w, err)
			return
		}
		if p == nil {
			p = []store.Profile{}
		}
		out = append(out, accountProfiles{a, p})
	}
	writeJSON(w, http.StatusOK, map[string]any{"accounts": out, "avatars": avatars, "levels": kidsLevels()})
}

type kidsLevel struct {
	Level int    `json:"level"`
	Name  string `json:"name"`
}

func kidsLevels() []kidsLevel {
	return []kidsLevel{{rating.LittleKids, rating.Name(rating.LittleKids)}, {rating.Kids, rating.Name(rating.Kids)}, {rating.Teens, rating.Name(rating.Teens)}}
}

type profileRequest struct {
	AccountID int64   `json:"accountId"`
	Name      *string `json:"name"`
	Avatar    *string `json:"avatar"`
	KidsLevel *int    `json:"kidsLevel"`
	// PIN is four digits; "" removes it.
	PIN *string `json:"pin"`
	// Libraries maps library ids to "all" or "rated"; libraries left out are hidden.
	Libraries map[string]string `json:"libraries"`
}

// changes checks a request and turns it into store changes.
func (req profileRequest) changes() (store.ProfileChanges, string) {
	var c store.ProfileChanges
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" || len([]rune(name)) > 30 {
			return c, "a name of 1 to 30 characters is required"
		}
		c.Name = &name
	}
	if req.Avatar != nil {
		if *req.Avatar != "" && !slices.Contains(avatars, *req.Avatar) {
			return c, "avatar must be one of: " + strings.Join(avatars, ", ")
		}
		c.Avatar = req.Avatar
	}
	if req.KidsLevel != nil {
		if *req.KidsLevel < 0 || *req.KidsLevel > rating.Teens {
			return c, "kidsLevel must be 0 (not a kids profile) to 3"
		}
		c.KidsLevel = req.KidsLevel
	}
	if req.PIN != nil {
		hash := ""
		if *req.PIN != "" {
			if !auth.ValidPIN(*req.PIN) {
				return c, "a PIN is four digits"
			}
			var err error
			if hash, err = auth.HashPassword(*req.PIN); err != nil {
				return c, "couldn't save that PIN"
			}
		}
		c.PINHash = &hash
	}
	if req.Libraries != nil {
		c.Libraries = map[int64]string{}
		for k, mode := range req.Libraries {
			id, err := strconv.ParseInt(k, 10, 64)
			if err != nil || mode != store.LibraryAll && mode != store.LibraryRated {
				return c, `libraries maps library ids to "all" or "rated"`
			}
			c.Libraries[id] = mode
		}
	}
	return c, ""
}

func (s *Server) createProfile(w http.ResponseWriter, r *http.Request) {
	var req profileRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if req.Name == nil {
		writeError(w, http.StatusBadRequest, "a name of 1 to 30 characters is required")
		return
	}
	c, msg := req.changes()
	if msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	acct, err := s.Store.UserByID(r.Context(), req.AccountID)
	if errors.Is(err, store.ErrNotFound) || err == nil && acct.AccountID != 0 {
		writeError(w, http.StatusBadRequest, "no such account")
		return
	}
	if err != nil {
		s.serverError(w, err)
		return
	}
	if c.Avatar == nil {
		// The next colour along, so new profiles don't all look alike.
		existing, err := s.Store.Profiles(r.Context(), acct.ID)
		if err != nil {
			s.serverError(w, err)
			return
		}
		c.Avatar = &avatars[(len(existing)+1)%len(avatars)]
	}
	if c.KidsLevel != nil && *c.KidsLevel > 0 && c.Libraries == nil {
		// Movies and TV, by rating, until an admin says otherwise.
		libs, err := s.Store.Libraries(r.Context())
		if err != nil {
			s.serverError(w, err)
			return
		}
		c.Libraries = map[int64]string{}
		for _, l := range libs {
			if l.Kind == "movies" || l.Kind == "tv" {
				c.Libraries[l.ID] = store.LibraryRated
			}
		}
	}
	p, err := s.Store.CreateProfile(r.Context(), acct.ID, c)
	if err != nil {
		s.serverError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, p)
}

// updateProfile changes a profile. For an account, only its PIN and avatar
// can be changed here, which guard and mark it in "Who's watching?".
func (s *Server) updateProfile(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	var req profileRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	c, msg := req.changes()
	if msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	u, err := s.Store.UserByID(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "no such profile")
		return
	}
	if err != nil {
		s.serverError(w, err)
		return
	}
	if u.AccountID == 0 && (c.Name != nil || c.KidsLevel != nil || c.Libraries != nil) {
		writeError(w, http.StatusBadRequest, "an account's name and access are changed under Users")
		return
	}
	if err := s.Store.UpdateProfile(r.Context(), id, c); err != nil {
		s.serverError(w, err)
		return
	}
	if u.AccountID == 0 {
		u, err = s.Store.UserByID(r.Context(), id)
		if err != nil {
			s.serverError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, store.Profile{User: u, Libraries: map[int64]string{}})
		return
	}
	p, err := s.Store.Profile(r.Context(), id)
	if err != nil {
		s.serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) deleteProfile(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	if err := s.Store.DeleteProfile(r.Context(), id); errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "no such profile")
		return
	} else if err != nil {
		s.serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// grownUps keeps kids profiles out of a route: requests, problem reports,
// coming soon and managing devices.
func grownUps(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if currentUser(r).KidsLevel > 0 {
			writeError(w, http.StatusForbidden, "not available on a kids profile")
			return
		}
		h(w, r)
	}
}
