package store

import (
	"context"
	"errors"
)

// Each person (an account or a profile) can rate titles, from half a star
// to five stars, stored as 1 to 10. Items carry the viewer's own rating as
// MyRating.

// ErrBadRating means a rating isn't a whole number from 1 to 10.
var ErrBadRating = errors.New("a rating is from 1 to 10")

// RatableKinds are what can be rated.
var RatableKinds = map[string]bool{
	"movie": true, "show": true, "season": true, "episode": true,
	"artist": true, "album": true, "track": true, "book": true,
	"podcast": true, "podcastEpisode": true,
}

// SetRating saves someone's rating of an item.
func (s *Store) SetRating(ctx context.Context, userID, itemID int64, rating int) error {
	_, err := s.MergeRating(ctx, userID, itemID, rating, now())
	return err
}

// MergeRating saves a rating made at a Unix time, unless the person rated
// the item more recently, and reports whether it did.
func (s *Store) MergeRating(ctx context.Context, userID, itemID int64, rating int, at int64) (bool, error) {
	if rating < 1 || rating > 10 {
		return false, ErrBadRating
	}
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO ratings (user_id, item_id, rating, rated_at) VALUES (?, ?, ?, ?)
		ON CONFLICT (user_id, item_id) DO UPDATE SET rating = excluded.rating, rated_at = excluded.rated_at
		WHERE ratings.rated_at <= excluded.rated_at`,
		userID, itemID, rating, at)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// ClearRating forgets someone's rating of an item.
func (s *Store) ClearRating(ctx context.Context, userID, itemID int64) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM ratings WHERE user_id = ? AND item_id = ?", userID, itemID)
	return err
}
