package store

import "context"

// Counts is what a dashboard shows of the server's contents.
type Counts struct {
	// Items is how many items there are of each kind (movie, show, episode,
	// artist, album, track, book, podcast...), leaving out extra versions
	// of the same title.
	Items     map[string]int
	Users     int // accounts, without guests and profiles
	Libraries int
}

func (s *Store) Counts(ctx context.Context) (Counts, error) {
	c := Counts{Items: map[string]int{}}
	if err := s.db.QueryRowContext(ctx, `SELECT
		(SELECT COUNT(*) FROM users WHERE guest = 0 AND parent_id IS NULL),
		(SELECT COUNT(*) FROM libraries)`).Scan(&c.Users, &c.Libraries); err != nil {
		return c, err
	}
	rows, err := s.db.QueryContext(ctx, "SELECT kind, COUNT(*) FROM items WHERE version_of IS NULL GROUP BY kind")
	if err != nil {
		return c, err
	}
	defer rows.Close()
	for rows.Next() {
		var kind string
		var n int
		if err := rows.Scan(&kind, &n); err != nil {
			return c, err
		}
		c.Items[kind] = n
	}
	return c, rows.Err()
}
