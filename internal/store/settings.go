package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
)

// Setting returns a server-wide setting, or "" when it was never set.
func (s *Store) Setting(ctx context.Context, key string) (string, error) {
	var v string
	err := s.db.QueryRowContext(ctx, "SELECT value FROM settings WHERE key = ?", key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return v, err
}

func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT (key) DO UPDATE SET value = excluded.value", key, value)
	return err
}

// HomeGuest is the account devices on the home network use when they
// haven't signed in, created the first time one does. Everyone at home
// shares it, so they share its watch progress and watch list too. It can't sign in, isn't
// listed with the other accounts, and isn't an admin.
func (s *Store) HomeGuest(ctx context.Context) (User, error) {
	u, err := s.scanUser(s.db.QueryRowContext(ctx, "SELECT "+userCols+" FROM users u WHERE u.guest = 1 ORDER BY u.id LIMIT 1"))
	if !errors.Is(err, ErrNotFound) {
		return u, err
	}
	// The stored name only has to be unique; people see "Home".
	b := make([]byte, 4)
	rand.Read(b)
	var id int64
	err = s.db.QueryRowContext(ctx,
		"INSERT INTO users (username, password_hash, is_admin, created_at, guest) VALUES (?, '', 0, ?, 1) RETURNING id",
		"home-guest-"+hex.EncodeToString(b), now()).Scan(&id)
	return User{ID: id, Username: "Home", Guest: true}, err
}
