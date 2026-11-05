package api

import (
	"cmp"
	"context"
	"net/http"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/yeixio/matinee/internal/arr"
	"github.com/yeixio/matinee/internal/live"
	"github.com/yeixio/matinee/internal/store"
)

// Notifications tell people things in the apps: when Box Office (the
// server that finds and downloads titles, github.com/yeixio/boxoffice) has
// found something they asked for, when it's ready to watch, and, for
// admins, when a download fails or looks unsafe.
//
// When Matinee's Radarr or Sonarr connection goes to Box Office, Matinee
// reads Box Office's feed of notifications with the same address and API
// key (GET /api/v3/boxoffice/notifications), so there's nothing else to set
// up. Each notification goes to the people who asked for its title, and to
// admins as they choose under Settings (problems only, by default). It's
// kept in each person's list (GET /notifications), and players that are
// open get it at once, as a "notify" command (see docs/remote-control.md).
// Real Radarr and Sonarr have no such feed, and are left alone.

// Settings keys.
const (
	settingNotifyAdmins = "notifications_admins"
	settingFeedPrefix   = "boxoffice_feed:" // + Box Office's id: the last notification read from it
)

// What admins hear about.
const (
	adminsAll      = "all"      // everything Box Office says
	adminsProblems = "problems" // failures and anything unsafe (the default)
	adminsOff      = "off"      // only titles they asked for themselves
)

// notifyEvery is how often Matinee reads Box Office's feed.
const notifyEvery = 30 * time.Second

// boxOffices is what Matinee knows about the Box Offices it reads from,
// for Settings.
type boxOffices struct {
	mu  sync.Mutex
	ids map[arr.App]boxOfficeState
}

type boxOfficeState struct {
	App       arr.App `json:"app"`
	ID        string  `json:"id,omitempty"` // empty: it isn't Box Office (or doesn't send notifications)
	CheckedAt int64   `json:"checkedAt"`    // milliseconds
	Error     string  `json:"error,omitempty"`
}

func (b *boxOffices) set(app arr.App, st boxOfficeState) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.ids == nil {
		b.ids = map[arr.App]boxOfficeState{}
	}
	st.App, st.CheckedAt = app, time.Now().UnixMilli()
	b.ids[app] = st
}

// forget drops what Matinee heard from an app, when its connection changes.
func (b *boxOffices) forget(app arr.App) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.ids, app)
}

// id is the Box Office an app was last heard to be, if any.
func (b *boxOffices) id(app arr.App) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.ids[app].ID
}

func (b *boxOffices) list() []boxOfficeState {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := []boxOfficeState{}
	for _, app := range arr.Apps {
		if st, ok := b.ids[app]; ok {
			out = append(out, st)
		}
	}
	return out
}

// RunNotifications reads Box Office's notifications until ctx ends.
func (s *Server) RunNotifications(ctx context.Context) {
	t := time.NewTimer(5 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		s.ReadBoxOffice(ctx)
		t.Reset(notifyEvery)
	}
}

// ReadBoxOffice reads what's new from each Box Office that Radarr or Sonarr
// connect to, once each when both go to the same one.
func (s *Server) ReadBoxOffice(ctx context.Context) {
	done := map[string]bool{}
	for _, app := range arr.Apps {
		cl, _, err := s.arrClient(ctx, app)
		if err != nil {
			s.boxOffices.forget(app)
			continue
		}
		callCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		st, err := cl.Status(callCtx)
		cancel()
		if err != nil {
			// A Box Office that's down is still Box Office.
			s.boxOffices.set(app, boxOfficeState{ID: s.boxOffices.id(app), Error: err.Error()})
			continue
		}
		if !st.BoxOffice("notifications") {
			s.boxOffices.set(app, boxOfficeState{})
			continue
		}
		if done[st.BoxOfficeID] {
			s.boxOffices.set(app, boxOfficeState{ID: st.BoxOfficeID})
			continue
		}
		done[st.BoxOfficeID] = true
		err = s.readFeed(ctx, cl, st.BoxOfficeID)
		state := boxOfficeState{ID: st.BoxOfficeID}
		if err != nil {
			state.Error = err.Error()
			s.Log.Warn("couldn't read Box Office's notifications", "app", app, "err", err)
		}
		s.boxOffices.set(app, state)
	}
}

// readFeed passes on what one Box Office has said since last time. The
// first time, it starts from now: what happened before isn't news.
func (s *Server) readFeed(ctx context.Context, cl *arr.Client, id string) error {
	key := settingFeedPrefix + id
	v, err := s.Store.Setting(ctx, key)
	if err != nil {
		return err
	}
	after := int64(-1)
	if v != "" {
		after, _ = strconv.ParseInt(v, 10, 64)
	}
	for range 20 { // at most 4,000 at a time; the rest come next time
		callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		f, err := cl.BoxOfficeNotifications(callCtx, after)
		cancel()
		if err != nil {
			return err
		}
		if after < 0 {
			return s.Store.SetSetting(ctx, key, strconv.FormatInt(f.Last, 10))
		}
		if len(f.Notifications) == 0 {
			if f.Last < after {
				// Box Office went back to an older backup: follow it.
				return s.Store.SetSetting(ctx, key, strconv.FormatInt(f.Last, 10))
			}
			return nil
		}
		for _, n := range f.Notifications {
			if err := s.deliverBoxOffice(ctx, id, n); err != nil {
				return err
			}
			after = n.ID
		}
		if err := s.Store.SetSetting(ctx, key, strconv.FormatInt(after, 10)); err != nil {
			return err
		}
	}
	return nil
}

// deliverBoxOffice gives one of Box Office's notifications to whoever it's for.
func (s *Server) deliverBoxOffice(ctx context.Context, boxOffice string, n arr.BoxOfficeNotification) error {
	note := store.Notification{
		Source: "boxoffice:" + boxOffice + ":" + strconv.FormatInt(n.ID, 10),
		Event:  n.Event, Level: cmp.Or(n.Level, store.NotificationInfo), Title: n.Title, Body: n.Body,
		CreatedAt: n.At.Unix(),
	}
	if n.At.IsZero() {
		note.CreatedAt = time.Now().Unix()
	}
	var to []int64
	if m := n.Media; m != nil {
		note.Kind, note.TMDBID, note.TVDBID, note.Poster = "movie", m.TMDBID, m.TVDBID, m.Poster
		if m.Kind == "series" {
			note.Kind = "show"
		}
		if n.Audience == "everyone" {
			ids, err := s.Store.Requesters(ctx, note.Kind, m.TMDBID, m.TVDBID)
			if err != nil {
				return err
			}
			to = ids
		}
	}
	mode, err := s.Store.Setting(ctx, settingNotifyAdmins)
	if err != nil {
		return err
	}
	mode = cmp.Or(mode, adminsProblems)
	if mode == adminsAll || (mode == adminsProblems && note.Level != store.NotificationInfo) {
		users, err := s.Store.Users(ctx)
		if err != nil {
			return err
		}
		for _, u := range users {
			if u.IsAdmin {
				to = append(to, u.ID)
			}
		}
	}
	slices.Sort(to)
	for _, uid := range slices.Compact(to) {
		note.UserID = uid
		saved, added, err := s.Store.AddNotification(ctx, note)
		if err != nil {
			return err
		}
		if added {
			s.pushNotification(ctx, uid, s.notificationView(ctx, saved))
		}
	}
	return nil
}

// pushNotification shows a notification on the person's open players: their
// own, and, for an account, its grown-up profiles'.
func (s *Server) pushNotification(ctx context.Context, uid int64, n store.Notification) {
	kids := map[int64]bool{}
	send := map[string]bool{}
	for _, c := range s.live().Clients(func(c live.Client) bool { return c.Online && (c.User.ID == uid || c.Account() == uid) }) {
		if c.User.ID != uid {
			k, ok := kids[c.User.ID]
			if !ok {
				u, err := s.Store.UserByID(ctx, c.User.ID)
				k = err != nil || u.KidsLevel > 0
				kids[c.User.ID] = k
			}
			if k {
				continue
			}
		}
		send[c.ID] = true
	}
	if len(send) > 0 {
		s.live().Notify(func(c live.Client) bool { return send[c.ID] }, n)
	}
}

// notificationView fills in what's worked out when a notification is
// shown: the title in the library, once it's there.
func (s *Server) notificationView(ctx context.Context, n store.Notification) store.Notification {
	if n.TMDBID != 0 && n.Kind != "" {
		if id, err := s.Store.LibraryMatch(ctx, n.Kind, n.TMDBID, "", 0); err == nil {
			n.ItemID = id
		}
	}
	return n
}

// notifiedUsers are whose notifications a person sees: their own, and for
// a profile, its account's too.
func notifiedUsers(u store.User) []int64 {
	if u.AccountID != 0 {
		return []int64{u.ID, u.AccountID}
	}
	return []int64{u.ID}
}

// listNotifications is GET /notifications: the newest first, up to limit
// (50 by default, at most 200), with how many are unread.
func (s *Server) listNotifications(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	list, unread, err := s.Store.Notifications(r.Context(), notifiedUsers(currentUser(r)), limit)
	if err != nil {
		s.serverError(w, err)
		return
	}
	for i := range list {
		list[i] = s.notificationView(r.Context(), list[i])
	}
	writeJSON(w, http.StatusOK, map[string]any{"notifications": list, "unread": unread})
}

type notificationIDs struct {
	// IDs are the notifications to change; none means all of them.
	IDs []string `json:"ids"`
}

// readNotifications is POST /notifications/read.
func (s *Server) readNotifications(w http.ResponseWriter, r *http.Request) {
	var req notificationIDs
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if err := s.Store.ReadNotifications(r.Context(), notifiedUsers(currentUser(r)), req.IDs); err != nil {
		s.serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// clearNotifications is POST /notifications/clear.
func (s *Server) clearNotifications(w http.ResponseWriter, r *http.Request) {
	var req notificationIDs
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if err := s.Store.DeleteNotifications(r.Context(), notifiedUsers(currentUser(r)), req.IDs); err != nil {
		s.serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type notificationSettings struct {
	// Admins is what admins hear about: all, problems or off.
	Admins string `json:"admins"`
	// BoxOffice says, for Radarr and Sonarr as connected, whether they're
	// Box Office, which sends notifications.
	BoxOffice []boxOfficeState `json:"boxOffice"`
}

// getNotificationSettings is GET /admin/notifications.
func (s *Server) getNotificationSettings(w http.ResponseWriter, r *http.Request) {
	mode, err := s.Store.Setting(r.Context(), settingNotifyAdmins)
	if err != nil {
		s.serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, notificationSettings{Admins: cmp.Or(mode, adminsProblems), BoxOffice: s.boxOffices.list()})
}

// putNotificationSettings is PUT /admin/notifications.
func (s *Server) putNotificationSettings(w http.ResponseWriter, r *http.Request) {
	var req notificationSettings
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if !slices.Contains([]string{adminsAll, adminsProblems, adminsOff}, req.Admins) {
		writeError(w, http.StatusBadRequest, "admins must be all, problems or off")
		return
	}
	if err := s.Store.SetSetting(r.Context(), settingNotifyAdmins, req.Admins); err != nil {
		s.serverError(w, err)
		return
	}
	s.getNotificationSettings(w, r)
}
