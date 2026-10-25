package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// Item is anything browsable: a playable file or a container of other items.
type Item struct {
	ID          int64  `json:"id"`
	LibraryID   int64  `json:"libraryId"`
	ParentID    *int64 `json:"parentId,omitempty"`
	Kind        string `json:"kind"`
	Title       string `json:"title"`
	Year        *int   `json:"year,omitempty"`
	IndexNumber *int   `json:"index,omitempty"`
	DiscNumber  *int   `json:"disc,omitempty"`
	DurationMs  *int64 `json:"durationMs,omitempty"`
	Container   string `json:"container,omitempty"`
	VideoCodec  string `json:"videoCodec,omitempty"`
	AudioCodec  string `json:"audioCodec,omitempty"`
	Width       *int   `json:"width,omitempty"`
	Height      *int   `json:"height,omitempty"`
	AddedAt     int64  `json:"addedAt"`
	Playable    bool   `json:"playable"`
	ChildCount  int    `json:"childCount,omitempty"`
	PositionMs  int64  `json:"positionMs,omitempty"`
	Played      bool   `json:"played,omitempty"`

	Path  string `json:"-"`
	Size  int64  `json:"-"`
	Mtime int64  `json:"-"`
}

// itemCols selects an item plus the given user's progress. The user id is the first bind argument.
const itemCols = `
	i.id, i.library_id, i.parent_id, i.kind, i.title, i.year, i.index_number, i.disc_number,
	i.duration_ms, IFNULL(i.container, ''), IFNULL(i.video_codec, ''), IFNULL(i.audio_codec, ''),
	i.width, i.height, i.added_at, IFNULL(i.path, ''), IFNULL(i.size, 0), IFNULL(i.mtime, 0),
	(SELECT COUNT(*) FROM items c WHERE c.parent_id = i.id),
	IFNULL(p.position_ms, 0), IFNULL(p.played, 0)`

const itemFrom = ` FROM items i LEFT JOIN progress p ON p.item_id = i.id AND p.user_id = ?`

type scanner interface{ Scan(...any) error }

func scanItem(r scanner) (Item, error) {
	var it Item
	var parent sql.NullInt64
	var year, idx, disc, w, h sql.NullInt64
	var dur sql.NullInt64
	err := r.Scan(&it.ID, &it.LibraryID, &parent, &it.Kind, &it.Title, &year, &idx, &disc,
		&dur, &it.Container, &it.VideoCodec, &it.AudioCodec, &w, &h, &it.AddedAt, &it.Path, &it.Size, &it.Mtime,
		&it.ChildCount, &it.PositionMs, &it.Played)
	if err != nil {
		return it, err
	}
	if parent.Valid {
		it.ParentID = &parent.Int64
	}
	it.Year, it.IndexNumber, it.DiscNumber = intPtr(year), intPtr(idx), intPtr(disc)
	it.Width, it.Height = intPtr(w), intPtr(h)
	if dur.Valid {
		it.DurationMs = &dur.Int64
	}
	it.Playable = it.Path != ""
	return it, nil
}

func intPtr(n sql.NullInt64) *int {
	if !n.Valid {
		return nil
	}
	v := int(n.Int64)
	return &v
}

func (s *Store) queryItems(ctx context.Context, where, order string, args ...any) ([]Item, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+itemCols+itemFrom+" WHERE "+where+" ORDER BY "+order, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Item{}
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// Item returns one item with the user's progress.
func (s *Store) Item(ctx context.Context, userID, id int64) (Item, error) {
	it, err := scanItem(s.db.QueryRowContext(ctx, "SELECT "+itemCols+itemFrom+" WHERE i.id = ?", userID, id))
	if errors.Is(err, sql.ErrNoRows) {
		return it, ErrNotFound
	}
	return it, err
}

// TopLevel lists a library's root items: movies, shows or artists.
func (s *Store) TopLevel(ctx context.Context, userID, libraryID int64) ([]Item, error) {
	return s.queryItems(ctx, "i.library_id = ? AND i.parent_id IS NULL", "i.sort_title", userID, libraryID)
}

// Children lists seasons of a show, episodes of a season, albums of an artist or tracks of an album.
func (s *Store) Children(ctx context.Context, userID, parentID int64) ([]Item, error) {
	return s.queryItems(ctx, "i.parent_id = ?", "IFNULL(i.disc_number, 0), IFNULL(i.index_number, 0), i.sort_title", userID, parentID)
}

// Search matches titles by word prefix, e.g. "star wa" finds "Star Wars".
func (s *Store) Search(ctx context.Context, userID int64, q string, limit int) ([]Item, error) {
	var terms []string
	for _, w := range strings.Fields(q) {
		w = strings.ReplaceAll(w, `"`, "")
		if w != "" {
			terms = append(terms, `"`+w+`"*`)
		}
	}
	if len(terms) == 0 {
		return []Item{}, nil
	}
	return s.queryItems(ctx,
		"i.id IN (SELECT rowid FROM items_fts WHERE items_fts MATCH ?) AND i.kind NOT IN ('season')",
		"i.sort_title LIMIT ?", userID, strings.Join(terms, " "), limit)
}

// RecentlyAdded lists the newest movies, episodes and albums.
func (s *Store) RecentlyAdded(ctx context.Context, userID int64, limit int) ([]Item, error) {
	return s.queryItems(ctx, "i.kind IN ('movie', 'episode', 'album')", "i.added_at DESC, i.id DESC LIMIT ?", userID, limit)
}

// ContinueWatching lists videos the user started but didn't finish.
func (s *Store) ContinueWatching(ctx context.Context, userID int64, limit int) ([]Item, error) {
	return s.queryItems(ctx, "p.played = 0 AND p.position_ms > 0 AND i.kind IN ('movie', 'episode')",
		"p.updated_at DESC LIMIT ?", userID, limit)
}

func (s *Store) SetProgress(ctx context.Context, userID, itemID, positionMs int64, played bool) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO progress (user_id, item_id, position_ms, played, updated_at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (user_id, item_id) DO UPDATE SET
			position_ms = excluded.position_ms, played = excluded.played, updated_at = excluded.updated_at`,
		userID, itemID, positionMs, played, now())
	return err
}

// ---- scanner support ----

// FileStat returns the stored size and mtime for a file path, or ErrNotFound.
func (s *Store) FileStat(ctx context.Context, path string) (id, size, mtime int64, err error) {
	err = s.db.QueryRowContext(ctx, "SELECT id, IFNULL(size, 0), IFNULL(mtime, 0) FROM items WHERE path = ?", path).
		Scan(&id, &size, &mtime)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return
}

// EnsureContainer finds or creates a show, season, artist or album and returns its id.
func (s *Store) EnsureContainer(ctx context.Context, libraryID int64, parentID *int64, kind, title string, index, year *int) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `
		SELECT id FROM items
		WHERE path IS NULL AND library_id = ? AND kind = ? AND IFNULL(parent_id, 0) = IFNULL(?, 0)
		  AND title = ? AND IFNULL(index_number, -1) = IFNULL(?, -1)`,
		libraryID, kind, parentID, title, index).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO items (library_id, parent_id, kind, title, sort_title, year, index_number, added_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		libraryID, parentID, kind, title, SortTitle(title), year, index, now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// UpsertFile inserts or updates a playable item keyed by its path.
func (s *Store) UpsertFile(ctx context.Context, it Item) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO items (library_id, parent_id, kind, title, sort_title, year, index_number, disc_number,
			path, size, mtime, duration_ms, container, video_codec, audio_codec, width, height, added_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (path) DO UPDATE SET
			library_id = excluded.library_id, parent_id = excluded.parent_id, kind = excluded.kind,
			title = excluded.title, sort_title = excluded.sort_title, year = excluded.year,
			index_number = excluded.index_number, disc_number = excluded.disc_number,
			size = excluded.size, mtime = excluded.mtime, duration_ms = excluded.duration_ms,
			container = excluded.container, video_codec = excluded.video_codec, audio_codec = excluded.audio_codec,
			width = excluded.width, height = excluded.height
		RETURNING id`,
		it.LibraryID, it.ParentID, it.Kind, it.Title, SortTitle(it.Title), it.Year, it.IndexNumber, it.DiscNumber,
		it.Path, it.Size, it.Mtime, it.DurationMs, nullStr(it.Container), nullStr(it.VideoCodec), nullStr(it.AudioCodec),
		it.Width, it.Height, now()).Scan(&id)
	return id, err
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// RemoveMissing deletes a library's files that the last scan didn't see, then
// any containers left empty.
func (s *Store) RemoveMissing(ctx context.Context, libraryID int64, seen map[string]bool) (int, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id, path FROM items WHERE library_id = ? AND path IS NOT NULL", libraryID)
	if err != nil {
		return 0, err
	}
	var gone []int64
	for rows.Next() {
		var id int64
		var p string
		if err := rows.Scan(&id, &p); err != nil {
			rows.Close()
			return 0, err
		}
		if !seen[p] {
			gone = append(gone, id)
		}
	}
	rows.Close()
	for _, id := range gone {
		if _, err := s.db.ExecContext(ctx, "DELETE FROM items WHERE id = ?", id); err != nil {
			return 0, err
		}
	}
	// Empty seasons make empty shows, so repeat until nothing changes.
	for {
		res, err := s.db.ExecContext(ctx, `
			DELETE FROM items WHERE library_id = ? AND path IS NULL
			AND NOT EXISTS (SELECT 1 FROM items c WHERE c.parent_id = items.id)`, libraryID)
		if err != nil {
			return 0, err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			break
		}
	}
	return len(gone), nil
}
