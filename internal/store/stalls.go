package store

import (
	"context"
	"time"
)

// Share stalls: how many times a file's network share stopped answering
// while a background job was reading the file (see slowfs.Timeouts). Kept
// across restarts, so that a file whose reads knock its share over is set
// aside even on a server that restarts often.

// AddShareStall counts one more stall while reading item id's file, whose
// version is key, and returns the count. A new key, or a last stall longer
// ago than within, starts the count over.
func (s *Store) AddShareStall(ctx context.Context, id int64, key string, within time.Duration) (int, error) {
	at := now()
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO share_stalls (item_id, file_key, count, last_at) VALUES (?, ?, 1, ?)
		ON CONFLICT (item_id) DO UPDATE SET
			count = CASE WHEN share_stalls.file_key = excluded.file_key AND share_stalls.last_at >= ?
				THEN share_stalls.count + 1 ELSE 1 END,
			file_key = excluded.file_key, last_at = excluded.last_at`,
		id, key, at, at-int64(within/time.Second)); err != nil {
		return 0, err
	}
	var n int
	err := s.db.QueryRowContext(ctx, "SELECT count FROM share_stalls WHERE item_id = ?", id).Scan(&n)
	return n, err
}

// ClearShareStalls forgets item id's stalls, for Analyze again.
func (s *Store) ClearShareStalls(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM share_stalls WHERE item_id = ?", id)
	return err
}
