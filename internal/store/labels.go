package store

import (
	"context"
	"database/sql"
	"sort"
	"strings"
)

// SetAlbumLabel keeps the record label an album's files name, and notes
// that its files were read for one. An empty label leaves any label the
// album has. A label already in the library under another spelling, such
// as ANJUNADEEP for Anjunadeep, keeps the library's spelling, so both
// albums land on one label page.
func (s *Store) SetAlbumLabel(ctx context.Context, id int64, label string) error {
	label = strings.TrimSpace(label)
	if label == "" {
		_, err := s.db.ExecContext(ctx, "UPDATE items SET label_read = 1 WHERE id = ? AND label_read = 0", id)
		return err
	}
	var same sql.NullString
	err := s.db.QueryRowContext(ctx, `
		SELECT MIN(o.label) FROM items a JOIN items o ON o.library_id = a.library_id
		WHERE a.id = ? AND o.kind = 'album' AND o.id <> a.id AND LOWER(o.label) = LOWER(?)`, id, label).Scan(&same)
	if err != nil {
		return err
	}
	if same.Valid {
		label = same.String
	}
	_, err = s.db.ExecContext(ctx, "UPDATE items SET label = ?, label_read = 1 WHERE id = ? AND (label IS NULL OR label <> ? OR label_read = 0)", label, id, label)
	return err
}

// AlbumToRead is an album and one of its files.
type AlbumToRead struct {
	ID   int64
	Path string
}

// AlbumsToReadLabel lists a library's albums whose files haven't been read
// for a record label, such as albums scanned before Matinee kept labels,
// with one file of each.
func (s *Store) AlbumsToReadLabel(ctx context.Context, libraryID int64) ([]AlbumToRead, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT c.id, MIN(f.path) FROM items c JOIN items f ON f.parent_id = c.id
		WHERE c.library_id = ? AND c.kind = 'album' AND c.label_read = 0 AND f.path IS NOT NULL
		GROUP BY c.id ORDER BY c.id`, libraryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AlbumToRead
	for rows.Next() {
		var a AlbumToRead
		if err := rows.Scan(&a.ID, &a.Path); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// RecordLabel is a label albums in a music library came out on.
type RecordLabel struct {
	Name    string `json:"name"`
	Albums  int    `json:"albums"`
	Artists int    `json:"artists"`
	// Covers are up to four of its albums with covers, newest first, for
	// a label's tile.
	Covers []Item `json:"covers"`
}

// labelCovers is how many album covers a label's tile shows.
const labelCovers = 4

// Labels lists the record labels of a music library's albums, by name.
func (s *Store) Labels(ctx context.Context, userID, libraryID int64) ([]RecordLabel, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT label, COUNT(*), COUNT(DISTINCT parent_id) FROM items
		WHERE library_id = ? AND kind = 'album' AND label IS NOT NULL AND label <> ''
		GROUP BY label`, libraryID)
	if err != nil {
		return nil, err
	}
	out := []RecordLabel{}
	at := map[string]int{}
	for rows.Next() {
		var l RecordLabel
		if err := rows.Scan(&l.Name, &l.Albums, &l.Artists); err != nil {
			rows.Close()
			return nil, err
		}
		l.Covers = []Item{}
		at[l.Name] = len(out)
		out = append(out, l)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return out, nil
	}
	albums, err := s.queryItems(ctx, "i.library_id = ? AND i.kind = 'album' AND i.label IS NOT NULL AND i.label <> '' AND i.poster IS NOT NULL",
		"i.year DESC NULLS LAST, i.added_at DESC, i.id", userID, libraryID)
	if err != nil {
		return nil, err
	}
	for _, a := range albums {
		if n, ok := at[a.RecordLabel]; ok && len(out[n].Covers) < labelCovers {
			out[n].Covers = append(out[n].Covers, a)
		}
	}
	sort.SliceStable(out, func(a, b int) bool { return SortTitle(out[a].Name) < SortTitle(out[b].Name) })
	return out, nil
}
