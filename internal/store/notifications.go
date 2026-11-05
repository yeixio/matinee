package store

import (
	"context"
	"strings"
	"time"
)

// ---- notifications ----
//
// Things a person is told about in the apps, such as Box Office finding or
// finishing a title they asked for (see internal/api/notifications.go).
// Each is kept per person, so each can read or clear their own.

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
	ItemID    int64 `json:"itemId,omitempty"`
	CreatedAt int64 `json:"createdAt"`
	Read      bool  `json:"read"`
}

const notificationCols = `id, user_id, source, event, level, title, body, kind, tmdb_id, tvdb_id, poster, created_at, read_at`

// keepNotifications is how long a notification stays.
const keepNotifications = 30 * 24 * time.Hour

// AddNotification saves a notification for a person. It's left out, and
// added is false, when they have one from the same source already, so
// servers that read the same feed tell them once.
func (s *Store) AddNotification(ctx context.Context, n Notification) (_ Notification, added bool, err error) {
	n.ID = newID()
	if n.CreatedAt == 0 {
		n.CreatedAt = now()
	}
	if n.Level == "" {
		n.Level = NotificationInfo
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO notifications (`+notificationCols+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0) ON CONFLICT (user_id, source) DO NOTHING`,
		n.ID, n.UserID, n.Source, n.Event, n.Level, n.Title, n.Body, n.Kind, n.TMDBID, n.TVDBID, n.Poster, n.CreatedAt)
	if err != nil {
		return n, false, err
	}
	rows, _ := res.RowsAffected()
	if rows > 0 {
		s.db.ExecContext(ctx, "DELETE FROM notifications WHERE created_at < ?", time.Now().Add(-keepNotifications).Unix())
	}
	return n, rows > 0, nil
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
	out := []Notification{}
	for rows.Next() {
		var n Notification
		var read int64
		if err := rows.Scan(&n.ID, &n.UserID, &n.Source, &n.Event, &n.Level, &n.Title, &n.Body, &n.Kind, &n.TMDBID, &n.TVDBID,
			&n.Poster, &n.CreatedAt, &read); err != nil {
			rows.Close()
			return nil, 0, err
		}
		n.Read = read > 0
		out = append(out, n)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, 0, err
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
