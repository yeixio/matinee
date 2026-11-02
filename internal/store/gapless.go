package store

import (
	"context"
	"strconv"
)

// Some albums are meant to play without a gap between songs: DJ mixes,
// live albums, concept albums where one song runs into the next. Matinee
// listens for them (internal/gapless), and the household can turn it on or
// off for any album; players then play the album's songs as one stream.

// SetGapless saves the household's choice for an album: on, off, or nil to
// leave it to Matinee.
func (s *Store) SetGapless(ctx context.Context, albumID int64, on *bool) error {
	var v any
	if on != nil {
		v = boolInt(*on)
	}
	res, err := s.db.ExecContext(ctx, "UPDATE items SET gapless = ? WHERE id = ? AND kind = 'album'", v, albumID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// GaplessAlbum is an album to listen to, with its songs in playing order.
type GaplessAlbum struct {
	ID     int64
	Title  string
	Artist string
	Tracks []GaplessTrack
	// Key is what the detector last listened to (see GaplessKey).
	Key string
}

type GaplessTrack struct {
	ID         int64
	Path       string
	DurationMs int64
	Disc       int
	Size       int64
	Mtime      int64
}

// GaplessKey names an album's files as they are now, so the detector
// listens again when songs are added, removed or changed.
func GaplessKey(version int, tracks []GaplessTrack) string {
	var size, mtime int64
	for _, t := range tracks {
		size += t.Size
		mtime = max(mtime, t.Mtime)
	}
	return MediaKey(version, size, mtime) + ":" + strconv.Itoa(len(tracks))
}

// GaplessAlbums lists the albums with ids above after, with their songs,
// stopping once it has limit albums.
func (s *Store) GaplessAlbums(ctx context.Context, after int64, limit int) ([]GaplessAlbum, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT al.id, al.title, COALESCE(ar.title, ''), COALESCE(al.gapless_key, ''),
			t.id, t.path, COALESCE(t.duration_ms, 0), COALESCE(t.disc_number, 0), COALESCE(t.size, 0), COALESCE(t.mtime, 0)
		FROM items al JOIN items t ON t.parent_id = al.id LEFT JOIN items ar ON ar.id = al.parent_id
		WHERE al.kind = 'album' AND al.id > ? AND t.kind = 'track' AND t.path IS NOT NULL
		ORDER BY al.id, COALESCE(t.disc_number, 0), COALESCE(t.index_number, 0), t.sort_title, t.id`, after)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GaplessAlbum
	for rows.Next() {
		var a GaplessAlbum
		var t GaplessTrack
		if err := rows.Scan(&a.ID, &a.Title, &a.Artist, &a.Key, &t.ID, &t.Path, &t.DurationMs, &t.Disc, &t.Size, &t.Mtime); err != nil {
			return nil, err
		}
		if n := len(out); n == 0 || out[n-1].ID != a.ID {
			if n >= limit {
				break
			}
			out = append(out, a)
		}
		out[len(out)-1].Tracks = append(out[len(out)-1].Tracks, t)
	}
	return out, rows.Err()
}

// SetGaplessFound records what the detector found for an album's files.
func (s *Store) SetGaplessFound(ctx context.Context, albumID int64, found bool, key string) error {
	_, err := s.db.ExecContext(ctx, "UPDATE items SET gapless_found = ?, gapless_key = ? WHERE id = ? AND kind = 'album'",
		boolInt(found), key, albumID)
	return err
}

// GaplessTracks returns the songs to play as one stream, in the order
// given, as the user may see them. Every id must be a song on the same
// album; ok is false otherwise.
func (s *Store) GaplessTracks(ctx context.Context, userID int64, ids []int64) (tracks []Item, ok bool, err error) {
	for _, id := range ids {
		it, err := s.Item(ctx, userID, id)
		if err == ErrNotFound {
			return nil, false, nil
		}
		if err != nil {
			return nil, false, err
		}
		if it.Kind != "track" || it.Path == "" || it.ParentID == nil || (len(tracks) > 0 && *tracks[0].ParentID != *it.ParentID) {
			return nil, false, nil
		}
		tracks = append(tracks, it)
	}
	return tracks, len(tracks) > 0, nil
}
