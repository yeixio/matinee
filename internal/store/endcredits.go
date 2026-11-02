package store

import (
	"context"
	"database/sql"
	"errors"
)

// End credits: the credits detector (internal/credits) keeps one row per
// episode or movie it looked at, with where the closing credits start and
// end, or a zero start when it found none. file_key is MediaKey for the
// detector's version and the file's size and mtime, so a replaced file is
// looked at again.

// CreditsVideo is an episode or movie the credits detector may look at.
type CreditsVideo struct {
	ID int64
	// Group is the season for an episode, whose episodes are compared with
	// each other, and the movie itself for a movie.
	Group      int64
	Kind       string // episode or movie
	Title      string
	Path       string
	Size       int64
	Mtime      int64
	DurationMs int64
	Index      int    // episode number, 0 when unknown
	Key        string // file_key as stored, "" when never looked at
	StartMs    int64
	EndMs      int64
	// MediaInfo is the media analysis' JSON, which says where a chapter
	// titled Credits starts, if any (see probe.MediaInfo).
	MediaInfo string
}

// CreditsVideos lists video episodes and movies of groups with ids above
// afterGroup, by group and by episode number within one, with whatever the
// detector found before. It stops after the group that takes it past limit
// videos, so a season is never split.
func (s *Store) CreditsVideos(ctx context.Context, afterGroup int64, limit int) ([]CreditsVideo, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT * FROM (
			SELECT e.id, CASE WHEN e.kind = 'episode' THEN e.parent_id ELSE e.id END AS grp, e.kind, e.title, e.path,
				COALESCE(e.size, 0), COALESCE(e.mtime, 0), COALESCE(e.duration_ms, 0), COALESCE(e.index_number, 0) AS idx,
				COALESCE(c.file_key, ''), COALESCE(c.start_ms, 0), COALESCE(c.end_ms, 0), COALESCE(e.media_info, ''), e.sort_title
			FROM items e LEFT JOIN end_credits c ON c.item_id = e.id
			WHERE ((e.kind = 'episode' AND e.parent_id IS NOT NULL) OR e.kind = 'movie')
				AND e.path IS NOT NULL AND COALESCE(e.video_codec, '') <> ''
		) v WHERE grp > ?
		ORDER BY grp, idx, sort_title, id`, afterGroup)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CreditsVideo
	for rows.Next() {
		var v CreditsVideo
		var sortTitle sql.NullString
		if err := rows.Scan(&v.ID, &v.Group, &v.Kind, &v.Title, &v.Path, &v.Size, &v.Mtime, &v.DurationMs, &v.Index, &v.Key, &v.StartMs, &v.EndMs, &v.MediaInfo, &sortTitle); err != nil {
			return nil, err
		}
		if len(out) >= limit && out[len(out)-1].Group != v.Group {
			break
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// SetEndCredits records what the detector found in one video: where its
// credits start and end (0 for the end of the file) and how it knows, or
// zeros for none.
func (s *Store) SetEndCredits(ctx context.Context, itemID, startMs, endMs int64, source, key string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO end_credits (item_id, start_ms, end_ms, source, file_key, looked_at) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (item_id) DO UPDATE SET start_ms = excluded.start_ms, end_ms = excluded.end_ms,
			source = excluded.source, file_key = excluded.file_key, looked_at = excluded.looked_at`,
		itemID, startMs, endMs, source, key, now())
	return err
}

// EndCredits returns where a video's end credits are, as the detector found
// them in the file as it is now. endMs is 0 when they run to the end. ok is
// false when it found none or hasn't looked.
func (s *Store) EndCredits(ctx context.Context, itemID int64, version int) (startMs, endMs int64, source string, ok bool, err error) {
	var key string
	var size, mtime int64
	err = s.db.QueryRowContext(ctx, `
		SELECT c.start_ms, c.end_ms, c.source, c.file_key, COALESCE(e.size, 0), COALESCE(e.mtime, 0)
		FROM end_credits c JOIN items e ON e.id = c.item_id WHERE c.item_id = ?`, itemID).
		Scan(&startMs, &endMs, &source, &key, &size, &mtime)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, 0, "", false, nil
	}
	if err != nil || key != MediaKey(version, size, mtime) || startMs <= 0 {
		return 0, 0, "", false, err
	}
	return startMs, endMs, source, true, nil
}

// ClearEndCredits has the detector look at a video, and its season, again.
func (s *Store) ClearEndCredits(ctx context.Context, itemID int64) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM end_credits WHERE item_id = ?", itemID)
	return err
}

// EndCreditsCounts says how many video episodes and movies there are, how
// many of them the detector has looked at as they are now, and in how many
// it found end credits.
func (s *Store) EndCreditsCounts(ctx context.Context, version int) (total, done, found int, err error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT COALESCE(e.size, 0), COALESCE(e.mtime, 0), COALESCE(c.file_key, ''), CASE WHEN COALESCE(c.start_ms, 0) > 0 THEN 1 ELSE 0 END
		FROM items e LEFT JOIN end_credits c ON c.item_id = e.id
		WHERE ((e.kind = 'episode' AND e.parent_id IS NOT NULL) OR e.kind = 'movie')
			AND e.path IS NOT NULL AND COALESCE(e.video_codec, '') <> ''`)
	if err != nil {
		return 0, 0, 0, err
	}
	defer rows.Close()
	for rows.Next() {
		var size, mtime int64
		var key string
		var has int
		if err := rows.Scan(&size, &mtime, &key, &has); err != nil {
			return 0, 0, 0, err
		}
		total++
		if key == MediaKey(version, size, mtime) {
			done++
			if has == 1 {
				found++
			}
		}
	}
	return total, done, found, rows.Err()
}
