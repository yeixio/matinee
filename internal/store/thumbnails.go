package store

import (
	"context"
	"database/sql"
	"errors"
)

// Thumbnails: the seek bar's preview pictures (internal/thumbs). The
// pictures themselves are JPEG sheets in the data folder, a grid of small
// frames each; a row here says how a video's sheets are laid out. file_key
// is MediaKey for the generator's version and the file's size and mtime,
// so a replaced file gets new pictures.

// ThumbnailFile is a video the thumbnail generator may make pictures of.
type ThumbnailFile struct {
	ID         int64
	Title      string
	Path       string
	Size       int64
	Mtime      int64
	DurationMs int64
	// MediaInfo is the media analysis' JSON, which says whether the video
	// is HDR (see probe.MediaInfo); "" before it's analyzed.
	MediaInfo string
	Key       string // file_key as stored, "" when never made
	Count     int    // pictures made as stored; 0 when none or unreadable
}

// Thumbnails is how a video's preview pictures are laid out: Count
// pictures, one every IntervalMs from the start, Width by Height each, in
// sheets of Columns by Rows, left to right and then down.
type Thumbnails struct {
	IntervalMs int64 `json:"intervalMs"`
	Width      int   `json:"width"`
	Height     int   `json:"height"`
	Columns    int   `json:"columns"`
	Rows       int   `json:"rows"`
	Count      int   `json:"count"`
	MadeAt     int64 `json:"-"` // Unix seconds
}

// Sheets is how many sheet images there are.
func (t Thumbnails) Sheets() int {
	per := t.Columns * t.Rows
	if per <= 0 {
		return 0
	}
	return (t.Count + per - 1) / per
}

const thumbnailVideos = "i.kind IN ('movie', 'episode') AND i.path IS NOT NULL AND COALESCE(i.video_codec, '') <> ''"

// ThumbnailFiles lists videos with ids above afterID, in id order, with
// what was made of them before.
func (s *Store) ThumbnailFiles(ctx context.Context, afterID int64, limit int) ([]ThumbnailFile, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT i.id, i.title, i.path, COALESCE(i.size, 0), COALESCE(i.mtime, 0), COALESCE(i.duration_ms, 0),
			COALESCE(i.media_info, ''), COALESCE(t.file_key, ''), COALESCE(t.count, 0)
		FROM items i LEFT JOIN thumbnails t ON t.item_id = i.id
		WHERE `+thumbnailVideos+` AND i.id > ? ORDER BY i.id LIMIT ?`, afterID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ThumbnailFile
	for rows.Next() {
		var f ThumbnailFile
		if err := rows.Scan(&f.ID, &f.Title, &f.Path, &f.Size, &f.Mtime, &f.DurationMs, &f.MediaInfo, &f.Key, &f.Count); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// ThumbnailFile returns one video, for making its pictures on demand.
func (s *Store) ThumbnailFile(ctx context.Context, id int64) (ThumbnailFile, error) {
	var f ThumbnailFile
	err := s.db.QueryRowContext(ctx, `
		SELECT i.id, i.title, i.path, COALESCE(i.size, 0), COALESCE(i.mtime, 0), COALESCE(i.duration_ms, 0),
			COALESCE(i.media_info, ''), COALESCE(t.file_key, ''), COALESCE(t.count, 0)
		FROM items i LEFT JOIN thumbnails t ON t.item_id = i.id
		WHERE `+thumbnailVideos+` AND i.id = ?`, id).
		Scan(&f.ID, &f.Title, &f.Path, &f.Size, &f.Mtime, &f.DurationMs, &f.MediaInfo, &f.Key, &f.Count)
	if errors.Is(err, sql.ErrNoRows) {
		return f, ErrNotFound
	}
	return f, err
}

// SetThumbnails records the pictures made of a video; a zero Count says
// the file couldn't be read, so it isn't tried again until it changes.
func (s *Store) SetThumbnails(ctx context.Context, itemID int64, t Thumbnails, key string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO thumbnails (item_id, file_key, interval_ms, width, height, across, down, count, made_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (item_id) DO UPDATE SET file_key = excluded.file_key, interval_ms = excluded.interval_ms,
			width = excluded.width, height = excluded.height, across = excluded.across, down = excluded.down,
			count = excluded.count, made_at = excluded.made_at`,
		itemID, key, t.IntervalMs, t.Width, t.Height, t.Columns, t.Rows, t.Count, now())
	return err
}

// ItemThumbnails returns a video's pictures, made of the file as it is now
// by this version of the generator. ok is false when there are none.
func (s *Store) ItemThumbnails(ctx context.Context, itemID int64, version int) (t Thumbnails, ok bool, err error) {
	var key string
	var size, mtime int64
	err = s.db.QueryRowContext(ctx, `
		SELECT t.interval_ms, t.width, t.height, t.across, t.down, t.count, t.made_at, t.file_key, COALESCE(i.size, 0), COALESCE(i.mtime, 0)
		FROM thumbnails t JOIN items i ON i.id = t.item_id WHERE t.item_id = ?`, itemID).
		Scan(&t.IntervalMs, &t.Width, &t.Height, &t.Columns, &t.Rows, &t.Count, &t.MadeAt, &key, &size, &mtime)
	if errors.Is(err, sql.ErrNoRows) {
		return Thumbnails{}, false, nil
	}
	if err != nil || key != MediaKey(version, size, mtime) || t.Count == 0 {
		return Thumbnails{}, false, err
	}
	return t, true, nil
}

// ClearThumbnails has a video's pictures made again.
func (s *Store) ClearThumbnails(ctx context.Context, itemID int64) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM thumbnails WHERE item_id = ?", itemID)
	return err
}

// ThumbnailIDs lists the videos pictures were made of, to clear away the
// files of videos that are gone.
func (s *Store) ThumbnailIDs(ctx context.Context) (map[int64]bool, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT item_id FROM thumbnails")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids[id] = true
	}
	return ids, rows.Err()
}

// ThumbnailCounts says how many videos there are, and how many have
// pictures made of them as they are now (or couldn't be read).
func (s *Store) ThumbnailCounts(ctx context.Context, version int) (total, done int, err error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT COALESCE(i.size, 0), COALESCE(i.mtime, 0), COALESCE(t.file_key, '')
		FROM items i LEFT JOIN thumbnails t ON t.item_id = i.id
		WHERE `+thumbnailVideos)
	if err != nil {
		return 0, 0, err
	}
	defer rows.Close()
	for rows.Next() {
		var size, mtime int64
		var key string
		if err := rows.Scan(&size, &mtime, &key); err != nil {
			return 0, 0, err
		}
		total++
		if key == MediaKey(version, size, mtime) {
			done++
		}
	}
	return total, done, rows.Err()
}
