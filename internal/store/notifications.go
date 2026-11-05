package store

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// ---- notifications ----
//
// Things a person is told about in the apps, such as Box Office finding or
// finishing a title they asked for (see internal/api/notifications.go).
// Each is kept per person, so each can read or clear their own.
//
// The same notification again, such as a disk that's still nearly full,
// doesn't add another: it moves the one already there to the top, unread,
// with the newest wording and a count of how many times it came (Repeats).
// Once someone clears it, the next one starts over.

// Notification levels.
const (
	NotificationInfo     = "info"
	NotificationWarning  = "warning"
	NotificationCritical = "critical"
)

type Notification struct {
	ID     string `json:"id"`
	UserID int64  `json:"-"`
	Source string `json:"-"`
	Event  string `json:"event"`
	Level  string `json:"level"`
	Title  string `json:"title"`
	Body   string `json:"body,omitempty"`
	Kind   string `json:"kind,omitempty"` // movie or show
	TMDBID int    `json:"tmdbId,omitempty"`
	TVDBID int    `json:"tvdbId,omitempty"`
	Poster string `json:"poster,omitempty"`
	// ItemID is the title in the library, once it's there. It's worked
	// out when listed, not stored.
	ItemID int64 `json:"itemId,omitempty"`
	// CreatedAt is when it last came; FirstAt, when it first did, and
	// Repeats how many times, for one that came more than once.
	CreatedAt int64 `json:"createdAt"`
	FirstAt   int64 `json:"firstAt,omitempty"`
	Repeats   int   `json:"repeats,omitempty"`
	Read      bool  `json:"read"`

	group string
}

const notificationCols = `id, user_id, source, event, level, title, body, kind, tmdb_id, tvdb_id, poster, created_at, read_at, group_key, repeats, first_at`

// numbers are figures in a sentence, such as "92% full" or "40.5 GB free".
var numbers = regexp.MustCompile(`\b\d+(?:[.,]\d+)*\b`)

// groupKey says which notifications are the same one again: the same
// event, title and wording, about the same title in the library. For one
// that isn't about a title, such as a disk filling up, figures in the
// wording can change (it's 93% full now, not 92%) and it's still the same.
func groupKey(n Notification) string {
	body := n.Body
	if n.TMDBID == 0 && n.TVDBID == 0 {
		body = numbers.ReplaceAllString(body, "#")
	}
	return fmt.Sprintf("%s\x00%s\x00%s\x00%s:%d:%d", n.Event, n.Title, body, n.Kind, n.TMDBID, n.TVDBID)
}

// levelRank orders levels, least serious first.
func levelRank(level string) int {
	switch level {
	case NotificationCritical:
		return 2
	case NotificationWarning:
		return 1
	}
	return 0
}

// Added says what AddNotification did.
type Added int

const (
	NotAdded  Added = iota // they have it from that source already
	AddedNew               // it's new
	Repeated               // it's the same as one they have, which now counts it
	Escalated              // it's a repeat, and more serious than before
)

// keepNotifications is how long a notification stays.
const keepNotifications = 30 * 24 * time.Hour

// AddNotification saves a notification for a person. It's left out when
// they have one from the same source already, so servers that read the
// same feed tell them once, and it's added to the one they have when it's
// the same again (see groupKey).
func (s *Store) AddNotification(ctx context.Context, n Notification) (Notification, Added, error) {
	if n.CreatedAt == 0 {
		n.CreatedAt = now()
	}
	if n.Level == "" {
		n.Level = NotificationInfo
	}
	var dup int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM notifications WHERE user_id = ? AND source = ?", n.UserID, n.Source).Scan(&dup); err != nil {
		return n, NotAdded, err
	}
	if dup > 0 {
		return n, NotAdded, nil
	}
	key := groupKey(n)
	rows, err := s.db.QueryContext(ctx, "SELECT "+notificationCols+" FROM notifications WHERE user_id = ? AND group_key = ? ORDER BY created_at DESC, id LIMIT 1", n.UserID, key)
	if err != nil {
		return n, NotAdded, err
	}
	have, err := scanNotifications(rows)
	if err != nil {
		return n, NotAdded, err
	}
	if len(have) > 0 {
		old := have[0]
		n.ID, n.FirstAt, n.Repeats = old.ID, cmpFirst(old), old.Repeats+1
		n.CreatedAt = max(n.CreatedAt, old.CreatedAt)
		// It takes the newest source, so a feed read again from an older
		// point doesn't count the same one twice.
		_, err := s.db.ExecContext(ctx, `UPDATE notifications SET source = ?, level = ?, body = ?, poster = ?, created_at = ?,
			read_at = 0, repeats = ?, first_at = ? WHERE id = ?`,
			n.Source, n.Level, n.Body, n.Poster, n.CreatedAt, n.Repeats, n.FirstAt, n.ID)
		if err != nil {
			return n, NotAdded, err
		}
		if levelRank(n.Level) > levelRank(old.Level) {
			return n, Escalated, nil
		}
		return n, Repeated, nil
	}
	n.ID, n.Repeats, n.FirstAt = newID(), 1, n.CreatedAt
	res, err := s.db.ExecContext(ctx, `INSERT INTO notifications (`+notificationCols+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?, 1, ?) ON CONFLICT (user_id, source) DO NOTHING`,
		n.ID, n.UserID, n.Source, n.Event, n.Level, n.Title, n.Body, n.Kind, n.TMDBID, n.TVDBID, n.Poster, n.CreatedAt, key, n.FirstAt)
	if err != nil {
		return n, NotAdded, err
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		return n, NotAdded, nil
	}
	s.db.ExecContext(ctx, "DELETE FROM notifications WHERE created_at < ?", time.Now().Add(-keepNotifications).Unix())
	n.Repeats, n.FirstAt = 0, 0 // only said for one that came more than once
	return n, AddedNew, nil
}

// cmpFirst is when a notification first came.
func cmpFirst(n Notification) int64 {
	if n.FirstAt > 0 {
		return min(n.FirstAt, n.CreatedAt)
	}
	return n.CreatedAt
}

func scanNotifications(rows *sql.Rows) ([]Notification, error) {
	defer rows.Close()
	out := []Notification{}
	for rows.Next() {
		var n Notification
		var read int64
		if err := rows.Scan(&n.ID, &n.UserID, &n.Source, &n.Event, &n.Level, &n.Title, &n.Body, &n.Kind, &n.TMDBID, &n.TVDBID,
			&n.Poster, &n.CreatedAt, &read, &n.group, &n.Repeats, &n.FirstAt); err != nil {
			return nil, err
		}
		n.Read = read > 0
		n.FirstAt = cmpFirst(n)
		n.Repeats = max(n.Repeats, 1)
		out = append(out, n)
	}
	return out, rows.Err()
}

// groupNotifications, once, folds together the notifications people got
// before repeats were counted: the same one many times becomes one.
func (s *Store) groupNotifications(ctx context.Context) error {
	const done = "backfill:notifications"
	if v, err := s.Setting(ctx, done); err != nil || v == "1" {
		return err
	}
	rows, err := s.db.QueryContext(ctx, "SELECT "+notificationCols+" FROM notifications ORDER BY user_id, created_at DESC, id")
	if err != nil {
		return err
	}
	all, err := scanNotifications(rows)
	if err != nil {
		return err
	}
	t, err := s.db.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer t.Rollback()
	type group struct {
		keep    *Notification
		repeats int
		first   int64
		unread  bool
	}
	groups := map[string]*group{}
	var order []string
	for i := range all {
		n := &all[i]
		key := fmt.Sprintf("%d\x00%s", n.UserID, groupKey(*n))
		g, ok := groups[key]
		if !ok {
			g = &group{keep: n, first: n.CreatedAt}
			groups[key] = g
			order = append(order, key)
		} else if _, err := t.ExecContext(ctx, "DELETE FROM notifications WHERE id = ?", n.ID); err != nil {
			return err
		}
		g.repeats += max(n.Repeats, 1)
		g.first = min(g.first, cmpFirst(*n))
		g.unread = g.unread || !n.Read
	}
	for _, key := range order {
		g := groups[key]
		q := "UPDATE notifications SET group_key = ?, repeats = ?, first_at = ?"
		if g.unread {
			q += ", read_at = 0"
		}
		if _, err := t.ExecContext(ctx, q+" WHERE id = ?", groupKey(*g.keep), g.repeats, g.first, g.keep.ID); err != nil {
			return err
		}
	}
	if err := t.Commit(); err != nil {
		return err
	}
	return s.SetSetting(ctx, done, "1")
}

// Notifications lists the newest notifications for the given people
// (a profile sees its own and its account's), and how many are unread.
func (s *Store) Notifications(ctx context.Context, users []int64, limit int) ([]Notification, int, error) {
	in, args := userList(users)
	rows, err := s.db.QueryContext(ctx, "SELECT "+notificationCols+" FROM notifications WHERE user_id IN ("+in+
		") ORDER BY created_at DESC, id LIMIT ?", append(args, limit)...)
	if err != nil {
		return nil, 0, err
	}
	out, err := scanNotifications(rows)
	if err != nil {
		return nil, 0, err
	}
	for i := range out {
		if out[i].Repeats <= 1 {
			out[i].Repeats, out[i].FirstAt = 0, 0 // only said for one that came more than once
		}
	}
	var unread int
	err = s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM notifications WHERE read_at = 0 AND user_id IN ("+in+")", args...).Scan(&unread)
	return out, unread, err
}

// ReadNotifications marks the given people's notifications read: those
// with the ids given, or all of them when ids is empty.
func (s *Store) ReadNotifications(ctx context.Context, users []int64, ids []string) error {
	in, args := userList(users)
	q := "UPDATE notifications SET read_at = ? WHERE read_at = 0 AND user_id IN (" + in + ")"
	args = append([]any{now()}, args...)
	if len(ids) > 0 {
		q += " AND id IN (?" + strings.Repeat(", ?", len(ids)-1) + ")"
		for _, id := range ids {
			args = append(args, id)
		}
	}
	_, err := s.db.ExecContext(ctx, q, args...)
	return err
}

// DeleteNotifications clears the given people's notifications: those with
// the ids given, or all of them when ids is empty.
func (s *Store) DeleteNotifications(ctx context.Context, users []int64, ids []string) error {
	in, args := userList(users)
	q := "DELETE FROM notifications WHERE user_id IN (" + in + ")"
	if len(ids) > 0 {
		q += " AND id IN (?" + strings.Repeat(", ?", len(ids)-1) + ")"
		for _, id := range ids {
			args = append(args, id)
		}
	}
	_, err := s.db.ExecContext(ctx, q, args...)
	return err
}

func userList(users []int64) (string, []any) {
	if len(users) == 0 {
		users = []int64{0}
	}
	args := make([]any, len(users))
	for i, u := range users {
		args[i] = u
	}
	return "?" + strings.Repeat(", ?", len(users)-1), args
}

// Requesters lists who asked for a title that's been approved (or is
// waiting, in case an admin added it in Box Office themselves): by TMDB id
// for movies, by TVDB or TMDB id for shows.
func (s *Store) Requesters(ctx context.Context, kind string, tmdbID, tvdbID int) ([]int64, error) {
	if tmdbID == 0 && tvdbID == 0 {
		return nil, nil
	}
	where := "r.tmdb_id = ?"
	args := []any{kind, tmdbID}
	if kind == "show" {
		where = "((r.tvdb_id = ? AND r.tvdb_id <> 0) OR (r.tmdb_id = ? AND r.tmdb_id <> 0))"
		args = []any{kind, tvdbID, tmdbID}
	} else if tmdbID == 0 {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, "SELECT DISTINCT r.user_id FROM requests r WHERE r.kind = ? AND "+where+
		" AND r.state IN ('pending', 'approved')", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
