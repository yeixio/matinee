package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// PluginKey is one thing a plugin's player keeps for a person, such as a
// saved game.
type PluginKey struct {
	Key       string `json:"key"`
	Size      int64  `json:"size"`
	UpdatedAt int64  `json:"updatedAt"` // milliseconds
}

// PluginValue returns what a plugin keeps under key for a user.
func (s *Store) PluginValue(ctx context.Context, pluginID string, userID int64, key string) ([]byte, error) {
	var v []byte
	err := s.db.QueryRowContext(ctx,
		"SELECT value FROM plugin_storage WHERE plugin_id = ? AND user_id = ? AND key = ?", pluginID, userID, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return v, err
}

// SetPluginValue keeps value under key for a user.
func (s *Store) SetPluginValue(ctx context.Context, pluginID string, userID int64, key string, value []byte) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO plugin_storage (plugin_id, user_id, key, value, updated_at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (plugin_id, user_id, key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		pluginID, userID, key, value, time.Now().UnixMilli())
	return err
}

// DeletePluginValue forgets one key.
func (s *Store) DeletePluginValue(ctx context.Context, pluginID string, userID int64, key string) error {
	_, err := s.db.ExecContext(ctx,
		"DELETE FROM plugin_storage WHERE plugin_id = ? AND user_id = ? AND key = ?", pluginID, userID, key)
	return err
}

// PluginKeys lists a user's keys that start with prefix, newest first, and
// how much the plugin keeps for them in all.
func (s *Store) PluginKeys(ctx context.Context, pluginID string, userID int64, prefix string) (keys []PluginKey, total int64, err error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT key, LENGTH(value), updated_at FROM plugin_storage WHERE plugin_id = ? AND user_id = ?
		ORDER BY updated_at DESC`, pluginID, userID)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	keys = []PluginKey{}
	for rows.Next() {
		var k PluginKey
		if err := rows.Scan(&k.Key, &k.Size, &k.UpdatedAt); err != nil {
			return nil, 0, err
		}
		total += k.Size
		if len(k.Key) >= len(prefix) && k.Key[:len(prefix)] == prefix {
			keys = append(keys, k)
		}
	}
	return keys, total, rows.Err()
}

// DeletePluginStorage forgets everything a plugin kept, when it's removed.
func (s *Store) DeletePluginStorage(ctx context.Context, pluginID string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM plugin_storage WHERE plugin_id = ?", pluginID)
	return err
}
