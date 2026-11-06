package store

import (
	"context"
	"database/sql"
	"errors"
)

// Admins can fix a movie's or show's online match by hand: pick the right
// title, refresh its details, or clear the match so it goes by its file
// name. A match picked or cleared by hand is locked, so later lookups leave
// it alone.

// Match is a movie's or show's online match as it stands.
type Match struct {
	TMDBID int64 `json:"tmdbId,omitempty"`
	// Locked means an admin picked or cleared the match by hand.
	Locked bool `json:"locked"`
}

// ItemMatch returns an item's online match.
func (s *Store) ItemMatch(ctx context.Context, id int64) (Match, error) {
	var m Match
	var tmdb sql.NullInt64
	var locked int
	err := s.db.QueryRowContext(ctx, "SELECT tmdb_id, match_locked FROM items WHERE id = ?", id).Scan(&tmdb, &locked)
	if errors.Is(err, sql.ErrNoRows) {
		return m, ErrNotFound
	}
	m.TMDBID, m.Locked = tmdb.Int64, locked == 1
	return m, err
}

// SetMatch replaces a movie's or show's online match with m, or with none
// when m is nil. Everything the old match brought (description, rating,
// genres, artwork found online, cast and crew, translations) goes first,
// while local artwork and a cover an admin picked stay. A show's seasons
// and episodes are looked up again under the new match. locked says the
// match was picked by hand, so lookups leave it alone from now on.
func (s *Store) SetMatch(ctx context.Context, id int64, m *Metadata, locked bool) error {
	t, err := s.db.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer t.Rollback()
	var kind string
	if err := t.QueryRowContext(ctx, "SELECT kind FROM items WHERE id = ?", id).Scan(&kind); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	ids := []int64{id}
	if kind == "show" {
		rows, err := t.QueryContext(ctx, `
			SELECT id FROM items WHERE parent_id = ?
			UNION SELECT e.id FROM items e JOIN items se ON se.id = e.parent_id WHERE se.parent_id = ?`, id, id)
		if err != nil {
			return err
		}
		for rows.Next() {
			var c int64
			if err := rows.Scan(&c); err != nil {
				rows.Close()
				return err
			}
			ids = append(ids, c)
		}
		rows.Close()
	}
	for _, c := range ids {
		// The show itself is matched below; its seasons and episodes wait for
		// the next lookup, which uses the show's new match (NULL meta_at), or
		// have none when the show has none.
		var metaAt any
		if c == id || m == nil {
			metaAt = now()
		}
		if _, err := t.ExecContext(ctx, `
			UPDATE items SET tmdb_id = NULL,
				overview = CASE WHEN `+keptFromOnline(FieldOverview)+` THEN overview END,
				rating = CASE WHEN `+keptFromOnline(FieldRating)+` THEN rating END,
				genres = CASE WHEN `+keptFromOnline(FieldGenres)+` THEN genres END,
				details = NULL, details_at = NULL, released = NULL, rating_level = NULL, translations_at = NULL,
				poster = CASE WHEN poster LIKE 'http%' THEN NULL ELSE poster END,
				backdrop = CASE WHEN backdrop LIKE 'http%' THEN NULL ELSE backdrop END,
				meta_at = ?
			WHERE id = ?`, metaAt, c); err != nil {
			return err
		}
		for _, table := range []string{"credits", "item_terms", "item_translations"} {
			if _, err := t.ExecContext(ctx, "DELETE FROM "+table+" WHERE item_id = ?", c); err != nil {
				return err
			}
		}
	}
	if _, err := t.ExecContext(ctx, "UPDATE items SET match_locked = ? WHERE id = ?", b2i(locked), id); err != nil {
		return err
	}
	if m != nil {
		if err := applyMetadata(ctx, t, id, m); err != nil {
			return err
		}
	}
	return t.Commit()
}

// LookUpAgain makes an item wait for an online lookup again, as if it were
// new, unless an admin locked its match.
func (s *Store) LookUpAgain(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, "UPDATE items SET meta_at = NULL, details_at = NULL, translations_at = NULL WHERE id = ? AND match_locked = 0", id)
	return err
}
