package store

import (
	"context"
	"database/sql"
	"errors"
)

// Subtitle syncs: subtitles lined up with a video's audio (see the
// subsync package). One row per video and subtitle track, which says how
// to move the track's times. file_key is the subtitle file and the video as
// they were then, so a replaced file is synced again.

// SubtitleSync is what lining up one subtitle track found.
type SubtitleSync struct {
	ItemID    int64
	Track     string
	Key       string
	State     string
	OffsetMs  int64
	Rate      float64
	Score     float64
	Reference string
	Auto      bool
	UpdatedAt int64
}

// SubtitleSyncs lists a video's subtitle syncs.
func (s *Store) SubtitleSyncs(ctx context.Context, itemID int64) ([]SubtitleSync, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT item_id, track, file_key, state, offset_ms, rate, score, reference, auto, updated_at
		FROM subtitle_syncs WHERE item_id = ?`, itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SubtitleSync
	for rows.Next() {
		var v SubtitleSync
		var auto int
		if err := rows.Scan(&v.ItemID, &v.Track, &v.Key, &v.State, &v.OffsetMs, &v.Rate, &v.Score, &v.Reference, &auto, &v.UpdatedAt); err != nil {
			return nil, err
		}
		v.Auto = auto != 0
		out = append(out, v)
	}
	return out, rows.Err()
}

// SubtitleSyncFor returns one track's sync.
func (s *Store) SubtitleSyncFor(ctx context.Context, itemID int64, track string) (SubtitleSync, error) {
	v := SubtitleSync{ItemID: itemID, Track: track}
	var auto int
	err := s.db.QueryRowContext(ctx, `SELECT file_key, state, offset_ms, rate, score, reference, auto, updated_at
		FROM subtitle_syncs WHERE item_id = ? AND track = ?`, itemID, track).
		Scan(&v.Key, &v.State, &v.OffsetMs, &v.Rate, &v.Score, &v.Reference, &auto, &v.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return v, ErrNotFound
	}
	v.Auto = auto != 0
	return v, err
}

// SetSubtitleSync keeps what lining up a track found, replacing what was
// there.
func (s *Store) SetSubtitleSync(ctx context.Context, v SubtitleSync) error {
	if v.Rate == 0 {
		v.Rate = 1
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO subtitle_syncs
		(item_id, track, file_key, state, offset_ms, rate, score, reference, auto, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (item_id, track) DO UPDATE SET file_key = excluded.file_key, state = excluded.state,
			offset_ms = excluded.offset_ms, rate = excluded.rate, score = excluded.score,
			reference = excluded.reference, auto = excluded.auto, updated_at = excluded.updated_at`,
		v.ItemID, v.Track, v.Key, v.State, v.OffsetMs, v.Rate, v.Score, v.Reference, b2i(v.Auto), now())
	return err
}

// RemoveSubtitleSync forgets a track's sync.
func (s *Store) RemoveSubtitleSync(ctx context.Context, itemID int64, track string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM subtitle_syncs WHERE item_id = ? AND track = ?", itemID, track)
	return err
}
