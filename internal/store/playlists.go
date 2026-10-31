package store

import (
	"context"
	"database/sql"
	"time"
)

// Playlists are songs someone puts together by hand, in their own order.
// Each belongs to one person or profile; devices at home that haven't
// signed in share the Home account's, like its watch list. The same song
// can be on a playlist more than once, so each place on it is an entry
// with its own id.

type Playlist struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Count      int    `json:"count"`      // songs the viewer can play
	DurationMs int64  `json:"durationMs"` // of those songs
	// AlbumIDs are the first few different albums on it, for its cover.
	AlbumIDs  []int64 `json:"albumIds"`
	CreatedAt int64   `json:"createdAt"` // milliseconds
	UpdatedAt int64   `json:"updatedAt"` // milliseconds
}

// PlaylistEntry is a song in its place on a playlist.
type PlaylistEntry struct {
	EntryID string `json:"entryId"`
	Item
}

// playlistCovers is how many albums a playlist's cover is made from.
const playlistCovers = 4

// Playlists lists someone's playlists, the most recently changed first.
func (s *Store) Playlists(ctx context.Context, userID int64) ([]Playlist, error) {
	return s.playlists(ctx, userID, "")
}

// Playlist returns one of someone's playlists, or ErrNotFound when they
// have none with that id.
func (s *Store) Playlist(ctx context.Context, userID int64, id string) (Playlist, error) {
	all, err := s.playlists(ctx, userID, id)
	if err != nil {
		return Playlist{}, err
	}
	if len(all) == 0 {
		return Playlist{}, ErrNotFound
	}
	return all[0], nil
}

func (s *Store) playlists(ctx context.Context, userID int64, only string) ([]Playlist, error) {
	where, args := "user_id = ?", []any{userID}
	if only != "" {
		where, args = where+" AND id = ?", append(args, only)
	}
	rows, err := s.db.QueryContext(ctx, "SELECT id, name, created_at, updated_at FROM playlists WHERE "+where+
		" ORDER BY updated_at DESC, id", args...)
	if err != nil {
		return nil, err
	}
	out := []Playlist{}
	byID := map[string]int{}
	for rows.Next() {
		p := Playlist{AlbumIDs: []int64{}}
		if err := rows.Scan(&p.ID, &p.Name, &p.CreatedAt, &p.UpdatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		byID[p.ID] = len(out)
		out = append(out, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil || len(out) == 0 {
		return out, err
	}
	// Counts, lengths and covers only go by songs the viewer may play (see
	// visibleToMe), so a kids profile's playlists match what it hears.
	q := `SELECT pi.playlist_id, i.parent_id, COALESCE(i.duration_ms, 0)
		FROM (SELECT CAST(? AS BIGINT) AS id) me CROSS JOIN items i
		JOIN playlist_items pi ON pi.item_id = i.id JOIN playlists pl ON pl.id = pi.playlist_id
		WHERE pl.user_id = ?`
	args = []any{userID, userID}
	if only != "" {
		q, args = q+" AND pl.id = ?", append(args, only)
	}
	rows, err = s.db.QueryContext(ctx, q+visibleToMe+" ORDER BY pi.playlist_id, pi.position", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var album sql.NullInt64
		var dur int64
		if err := rows.Scan(&id, &album, &dur); err != nil {
			return nil, err
		}
		p := &out[byID[id]]
		p.Count++
		p.DurationMs += dur
		if album.Valid && len(p.AlbumIDs) < playlistCovers && !containsID(p.AlbumIDs, album.Int64) {
			p.AlbumIDs = append(p.AlbumIDs, album.Int64)
		}
	}
	return out, rows.Err()
}

func containsID(ids []int64, id int64) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

// PlaylistEntries lists a playlist's songs in order, leaving out any the
// viewer may not play.
func (s *Store) PlaylistEntries(ctx context.Context, userID int64, id string) ([]PlaylistEntry, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id, item_id FROM playlist_items WHERE playlist_id = ? ORDER BY position, id", id)
	if err != nil {
		return nil, err
	}
	type place struct {
		entry string
		item  int64
	}
	var places []place
	for rows.Next() {
		var p place
		if err := rows.Scan(&p.entry, &p.item); err != nil {
			rows.Close()
			return nil, err
		}
		places = append(places, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	items, err := s.queryItems(ctx, "i.id IN (SELECT item_id FROM playlist_items WHERE playlist_id = ?)", "i.id", userID, id)
	if err != nil {
		return nil, err
	}
	byID := make(map[int64]Item, len(items))
	for _, it := range items {
		byID[it.ID] = it
	}
	out := []PlaylistEntry{}
	for _, p := range places {
		if it, ok := byID[p.item]; ok {
			out = append(out, PlaylistEntry{EntryID: p.entry, Item: it})
		}
	}
	return out, nil
}

// CreatePlaylist makes an empty playlist.
func (s *Store) CreatePlaylist(ctx context.Context, userID int64, name string) (Playlist, error) {
	p := Playlist{ID: newID(), Name: name, AlbumIDs: []int64{}, CreatedAt: time.Now().UnixMilli()}
	p.UpdatedAt = p.CreatedAt
	_, err := s.db.ExecContext(ctx, "INSERT INTO playlists (id, user_id, name, created_at, updated_at) VALUES (?, ?, ?, ?, ?)",
		p.ID, userID, p.Name, p.CreatedAt, p.UpdatedAt)
	return p, err
}

// RenamePlaylist changes a playlist's name.
func (s *Store) RenamePlaylist(ctx context.Context, userID int64, id, name string) error {
	res, err := s.db.ExecContext(ctx, "UPDATE playlists SET name = ?, updated_at = ? WHERE id = ? AND user_id = ?",
		name, time.Now().UnixMilli(), id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeletePlaylist(ctx context.Context, userID int64, id string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM playlists WHERE id = ? AND user_id = ?", id, userID)
	return err
}

// AddToPlaylist puts songs at the end of a playlist, in the order given, and
// returns their entry ids.
func (s *Store) AddToPlaylist(ctx context.Context, id string, itemIDs []int64) ([]string, error) {
	tx, err := s.db.BeginTx(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var last int
	if err := tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(position), 0) FROM playlist_items WHERE playlist_id = ?", id).Scan(&last); err != nil {
		return nil, err
	}
	now := time.Now().UnixMilli()
	entries := make([]string, len(itemIDs))
	for i, item := range itemIDs {
		entries[i] = newID()
		if _, err := tx.ExecContext(ctx, "INSERT INTO playlist_items (id, playlist_id, item_id, position, added_at) VALUES (?, ?, ?, ?, ?)",
			entries[i], id, item, last+i+1, now); err != nil {
			return nil, err
		}
	}
	if err := touchPlaylist(ctx, tx, id, now); err != nil {
		return nil, err
	}
	return entries, tx.Commit()
}

// RemoveFromPlaylist takes entries off a playlist.
func (s *Store) RemoveFromPlaylist(ctx context.Context, id string, entryIDs []string) error {
	tx, err := s.db.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, e := range entryIDs {
		if _, err := tx.ExecContext(ctx, "DELETE FROM playlist_items WHERE playlist_id = ? AND id = ?", id, e); err != nil {
			return err
		}
	}
	if err := touchPlaylist(ctx, tx, id, time.Now().UnixMilli()); err != nil {
		return err
	}
	return tx.Commit()
}

// OrderPlaylist puts a playlist's entries in the order given. Entries left
// out, such as songs the viewer can't see, follow in the order they had.
// Ids that aren't on the playlist are ignored.
func (s *Store) OrderPlaylist(ctx context.Context, id string, entryIDs []string) error {
	tx, err := s.db.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, "SELECT id FROM playlist_items WHERE playlist_id = ? ORDER BY position, id", id)
	if err != nil {
		return err
	}
	var current []string
	for rows.Next() {
		var e string
		if err := rows.Scan(&e); err != nil {
			rows.Close()
			return err
		}
		current = append(current, e)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	on := make(map[string]bool, len(current))
	for _, e := range current {
		on[e] = true
	}
	order := make([]string, 0, len(current))
	placed := map[string]bool{}
	for _, e := range append(entryIDs, current...) {
		if on[e] && !placed[e] {
			placed[e] = true
			order = append(order, e)
		}
	}
	for i, e := range order {
		if _, err := tx.ExecContext(ctx, "UPDATE playlist_items SET position = ? WHERE id = ?", i+1, e); err != nil {
			return err
		}
	}
	if err := touchPlaylist(ctx, tx, id, time.Now().UnixMilli()); err != nil {
		return err
	}
	return tx.Commit()
}

func touchPlaylist(ctx context.Context, t *tx, id string, at int64) error {
	res, err := t.ExecContext(ctx, "UPDATE playlists SET updated_at = ? WHERE id = ?", at, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
