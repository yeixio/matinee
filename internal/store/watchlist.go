package store

import (
	"context"
	"time"
)

// WatchListKinds are what can go on a watch list: things you sit down to
// watch, not their seasons, episodes or music.
var WatchListKinds = map[string]bool{"movie": true, "show": true}

// WatchList lists the items a user saved to watch later, newest first.
func (s *Store) WatchList(ctx context.Context, userID int64, limit int) ([]Item, error) {
	return s.queryItems(ctx, "i.id IN (SELECT item_id FROM watchlist WHERE user_id = me.id)", `
		(SELECT added_at FROM watchlist WHERE user_id = me.id AND item_id = i.id) DESC, i.id DESC LIMIT ?`, userID, limit)
}

// AddToWatchList saves an item to a user's watch list. Adding it again keeps
// its place.
func (s *Store) AddToWatchList(ctx context.Context, userID, itemID int64) error {
	// added_at is in milliseconds and always after the list's last one, so
	// the order holds even for several adds in the same instant.
	var last int64
	if err := s.db.QueryRowContext(ctx, "SELECT COALESCE(MAX(added_at), 0) FROM watchlist WHERE user_id = ?", userID).Scan(&last); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO watchlist (user_id, item_id, added_at) VALUES (?, ?, ?) ON CONFLICT DO NOTHING",
		userID, itemID, max(time.Now().UnixMilli(), last+1))
	return err
}

func (s *Store) RemoveFromWatchList(ctx context.Context, userID, itemID int64) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM watchlist WHERE user_id = ? AND item_id = ?", userID, itemID)
	return err
}
