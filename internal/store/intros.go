package store

import (
	"context"
	"database/sql"
	"errors"
)

// Intros: the intro detector (internal/intro) keeps one row per episode it
// looked at, with where the intro starts and ends, or zeros when it found
// none. file_key is MediaKey for the detector's version and the file's size
// and mtime, so a replaced file is looked at again.

// IntroEpisode is an episode the intro detector may look at.
type IntroEpisode struct {
	ID         int64
	SeasonID   int64
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
	// titled Intro is, if any (see probe.MediaInfo).
	MediaInfo string
}

// IntroEpisodes lists the video episodes of seasons with ids above
// afterSeason, grouped by season in id order and by episode number within
// one, with whatever the detector found before. It stops after the season
// that takes it past limit episodes, so a season is never split.
func (s *Store) IntroEpisodes(ctx context.Context, afterSeason int64, limit int) ([]IntroEpisode, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT e.id, e.parent_id, e.title, e.path, COALESCE(e.size, 0), COALESCE(e.mtime, 0), COALESCE(e.duration_ms, 0),
			COALESCE(e.index_number, 0), COALESCE(i.file_key, ''), COALESCE(i.start_ms, 0), COALESCE(i.end_ms, 0), COALESCE(e.media_info, '')
		FROM items e LEFT JOIN intros i ON i.item_id = e.id
		WHERE e.kind = 'episode' AND e.path IS NOT NULL AND e.parent_id > ?
			AND COALESCE(e.video_codec, '') <> ''
		ORDER BY e.parent_id, e.index_number, e.sort_title, e.id`, afterSeason)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []IntroEpisode
	for rows.Next() {
		var e IntroEpisode
		if err := rows.Scan(&e.ID, &e.SeasonID, &e.Title, &e.Path, &e.Size, &e.Mtime, &e.DurationMs, &e.Index, &e.Key, &e.StartMs, &e.EndMs, &e.MediaInfo); err != nil {
			return nil, err
		}
		if len(out) >= limit && out[len(out)-1].SeasonID != e.SeasonID {
			break
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// SetIntro records what the detector found in one episode: where its intro
// starts and ends, or zeros for none.
func (s *Store) SetIntro(ctx context.Context, itemID, startMs, endMs int64, key string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO intros (item_id, start_ms, end_ms, file_key, looked_at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (item_id) DO UPDATE SET start_ms = excluded.start_ms, end_ms = excluded.end_ms,
			file_key = excluded.file_key, looked_at = excluded.looked_at`,
		itemID, startMs, endMs, key, now())
	return err
}

// Intro returns where an episode's intro is, as the detector found it in
// the file as it is now. ok is false when it found none or hasn't looked.
func (s *Store) Intro(ctx context.Context, itemID int64, version int) (startMs, endMs int64, ok bool, err error) {
	var key string
	var size, mtime int64
	err = s.db.QueryRowContext(ctx, `
		SELECT i.start_ms, i.end_ms, i.file_key, COALESCE(e.size, 0), COALESCE(e.mtime, 0)
		FROM intros i JOIN items e ON e.id = i.item_id WHERE i.item_id = ?`, itemID).
		Scan(&startMs, &endMs, &key, &size, &mtime)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, 0, false, nil
	}
	if err != nil || key != MediaKey(version, size, mtime) || endMs <= startMs {
		return 0, 0, false, err
	}
	return startMs, endMs, true, nil
}

// ClearIntro has the detector look at an episode, and its season, again.
func (s *Store) ClearIntro(ctx context.Context, itemID int64) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM intros WHERE item_id = ?", itemID)
	return err
}

// IntroCounts says how many video episodes there are, how many of them the
// detector has looked at as they are now, and in how many it found an intro.
func (s *Store) IntroCounts(ctx context.Context, version int) (total, done, found int, err error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT COALESCE(e.size, 0), COALESCE(e.mtime, 0), COALESCE(i.file_key, ''), CASE WHEN COALESCE(i.end_ms, 0) > COALESCE(i.start_ms, 0) THEN 1 ELSE 0 END
		FROM items e LEFT JOIN intros i ON i.item_id = e.id
		WHERE e.kind = 'episode' AND e.path IS NOT NULL AND e.parent_id IS NOT NULL AND COALESCE(e.video_codec, '') <> ''`)
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
