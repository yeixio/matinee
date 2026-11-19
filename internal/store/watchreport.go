package store

import (
	"context"
	"slices"
)

// WatchedTitle is how much one movie or show has been watched, by
// everyone together: no names, only counts.
type WatchedTitle struct {
	TMDBID int
	// LastPlayed is when anyone last played any of it, in Unix seconds.
	LastPlayed int64
	// Viewers is how many people have played any of it, and FinishedBy how
	// many have played all of it: the movie, or every episode there is
	// here, leaving specials out.
	Viewers    int
	FinishedBy int
	// FinishedAt is when the last of those people finished, or 0.
	FinishedAt int64
}

// WatchReport is what has been watched of every movie (kind "movie") or
// show (kind "show") that has a TMDB id, by TMDB id, and how many people
// there are to watch: accounts and profiles, without guests. Titles nobody
// has played are left out.
func (s *Store) WatchReport(ctx context.Context, kind string) (people int, titles []WatchedTitle, err error) {
	if err = s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM users WHERE guest = 0").Scan(&people); err != nil {
		return 0, nil, err
	}
	// How many parts each title has: 1 for a movie, a show's episodes.
	parts := map[int]int{}
	q := `SELECT p.user_id, m.tmdb_id, MAX(p.updated_at), MAX(p.played), 1,
			MAX(CASE WHEN p.played = 1 THEN p.updated_at ELSE 0 END)
		FROM progress p
		JOIN users u ON u.id = p.user_id AND u.guest = 0
		JOIN items m ON m.id = p.item_id AND m.kind = 'movie' AND m.tmdb_id > 0
		WHERE p.played = 1 OR p.position_ms > 0
		GROUP BY p.user_id, m.tmdb_id`
	if kind == "show" {
		const episode = "(COALESCE(se.index_number, 0) * 100000 + COALESCE(e.index_number, 0))"
		rows, err := s.db.QueryContext(ctx, `SELECT sh.tmdb_id, COUNT(DISTINCT `+episode+`)
			FROM items e
			JOIN items se ON se.id = e.parent_id AND se.kind = 'season' AND COALESCE(se.index_number, -1) <> 0
			JOIN items sh ON sh.id = se.parent_id AND sh.kind = 'show' AND sh.tmdb_id > 0
			WHERE e.kind = 'episode' AND e.path IS NOT NULL AND e.version_of IS NULL AND e.file_part_of IS NULL
			GROUP BY sh.tmdb_id`)
		if err != nil {
			return 0, nil, err
		}
		for rows.Next() {
			var id, n int
			if err := rows.Scan(&id, &n); err != nil {
				rows.Close()
				return 0, nil, err
			}
			parts[id] = n
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return 0, nil, err
		}
		// Specials count as watching the show, but not towards finishing it.
		const regular = "p.played = 1 AND COALESCE(se.index_number, -1) <> 0"
		q = `SELECT p.user_id, sh.tmdb_id, MAX(p.updated_at), 0,
				COUNT(DISTINCT CASE WHEN ` + regular + ` THEN ` + episode + ` END),
				MAX(CASE WHEN ` + regular + ` THEN p.updated_at ELSE 0 END)
			FROM progress p
			JOIN users u ON u.id = p.user_id AND u.guest = 0
			JOIN items e ON e.id = p.item_id AND e.kind = 'episode'
			JOIN items se ON se.id = e.parent_id AND se.kind = 'season'
			JOIN items sh ON sh.id = se.parent_id AND sh.kind = 'show' AND sh.tmdb_id > 0
			WHERE p.played = 1 OR p.position_ms > 0
			GROUP BY p.user_id, sh.tmdb_id`
	}
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return 0, nil, err
	}
	defer rows.Close()
	by := map[int]*WatchedTitle{}
	for rows.Next() {
		var user int64
		var id, played, done int
		var last, doneAt int64
		if err := rows.Scan(&user, &id, &last, &played, &done, &doneAt); err != nil {
			return 0, nil, err
		}
		t := by[id]
		if t == nil {
			t = &WatchedTitle{TMDBID: id}
			by[id] = t
		}
		t.Viewers++
		t.LastPlayed = max(t.LastPlayed, last)
		finished := played == 1
		if kind == "show" {
			finished = parts[id] > 0 && done >= parts[id]
		}
		if finished {
			t.FinishedBy++
			t.FinishedAt = max(t.FinishedAt, doneAt)
		}
	}
	if err := rows.Err(); err != nil {
		return 0, nil, err
	}
	for _, t := range by {
		titles = append(titles, *t)
	}
	slices.SortFunc(titles, func(a, b WatchedTitle) int { return a.TMDBID - b.TMDBID })
	return people, titles, nil
}
