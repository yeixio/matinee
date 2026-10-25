package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
	"time"
)

// ---- signed-in devices ----

// Session is a signed-in device as its owner sees it. ID is derived from the
// token, so the token itself never leaves the device that holds it.
type Session struct {
	ID         string `json:"id"`
	DeviceName string `json:"deviceName"`
	Platform   string `json:"platform"`
	CreatedAt  int64  `json:"createdAt"`
	LastSeenAt int64  `json:"lastSeenAt"`
	Current    bool   `json:"current"`
}

// SessionID is the public id of a session token.
func SessionID(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:8])
}

// Sessions lists a user's signed-in devices, most recently used first.
// current is the token of the request asking, which is marked.
func (s *Store) Sessions(ctx context.Context, userID int64, current string) ([]Session, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT token, device_name, platform, created_at, last_seen_at FROM sessions
		WHERE user_id = ? AND expires_at > ? ORDER BY last_seen_at DESC`, userID, now())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Session{}
	for rows.Next() {
		var token string
		var v Session
		if err := rows.Scan(&token, &v.DeviceName, &v.Platform, &v.CreatedAt, &v.LastSeenAt); err != nil {
			return nil, err
		}
		v.ID, v.Current = SessionID(token), token == current
		out = append(out, v)
	}
	return out, rows.Err()
}

// DeleteSessionByID signs one of a user's devices out.
func (s *Store) DeleteSessionByID(ctx context.Context, userID int64, id string) error {
	rows, err := s.db.QueryContext(ctx, "SELECT token FROM sessions WHERE user_id = ?", userID)
	if err != nil {
		return err
	}
	var match string
	for rows.Next() {
		var token string
		if err := rows.Scan(&token); err != nil {
			rows.Close()
			return err
		}
		if SessionID(token) == id {
			match = token
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if match == "" {
		return ErrNotFound
	}
	return s.DeleteSession(ctx, match)
}

// ---- sign-in with a code ----
//
// A device without a comfortable keyboard (a phone, a TV) asks for a code and
// shows it. Someone already signed in enters the code at /link, and the device,
// which has been polling with its secret device code, gets a session. This is
// the OAuth device authorization grant (RFC 8628), without the OAuth.

// userCodeAlphabet leaves out 0, O, 1 and I, which are easy to misread.
const userCodeAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

const userCodeLength = 8

var (
	ErrCodeExpired = errors.New("code expired")
	ErrCodeDenied  = errors.New("sign-in was declined")
	ErrCodePending = errors.New("waiting for approval")
)

type DeviceCode struct {
	DeviceCode string // secret; only the device has it
	UserCode   string // what the person types, without the dash
	Device     Device
	ExpiresAt  time.Time
}

// FormatUserCode shows a user code as ABCD-EFGH.
func FormatUserCode(code string) string {
	if len(code) != userCodeLength {
		return code
	}
	return code[:4] + "-" + code[4:]
}

// NormalizeUserCode accepts a typed code in any case, with or without the dash or spaces.
func NormalizeUserCode(code string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' {
			r -= 'a' - 'A'
		}
		if strings.ContainsRune(userCodeAlphabet, r) {
			return r
		}
		return -1
	}, code)
}

func randomUserCode() (string, error) {
	b := make([]byte, userCodeLength)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	for i := range b {
		// 256 is a multiple of the alphabet's 32 letters, so every letter is equally likely.
		b[i] = userCodeAlphabet[int(b[i])%len(userCodeAlphabet)]
	}
	return string(b), nil
}

// StartDeviceCode makes a code for a device to show.
func (s *Store) StartDeviceCode(ctx context.Context, dev Device, ttl time.Duration) (DeviceCode, error) {
	// Old codes are dropped here rather than by a background job.
	if _, err := s.db.ExecContext(ctx, "DELETE FROM device_codes WHERE expires_at <= ?", now()); err != nil {
		return DeviceCode{}, err
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return DeviceCode{}, err
	}
	dc := DeviceCode{DeviceCode: hex.EncodeToString(secret), Device: dev, ExpiresAt: time.Now().Add(ttl)}
	for range 5 { // a clash among live codes is very unlikely; try again if it happens
		code, err := randomUserCode()
		if err != nil {
			return dc, err
		}
		_, err = s.db.ExecContext(ctx, `INSERT INTO device_codes (device_code, user_code, device_name, platform, expires_at)
			VALUES (?, ?, ?, ?, ?)`, dc.DeviceCode, code, dev.Name, dev.Platform, dc.ExpiresAt.Unix())
		if err == nil {
			dc.UserCode = code
			return dc, nil
		}
		if !isUniqueViolation(err) {
			return dc, err
		}
	}
	return dc, errors.New("couldn't make a unique code")
}

// PendingDeviceCode looks up a code someone typed, for the "sign in this device?" screen.
func (s *Store) PendingDeviceCode(ctx context.Context, userCode string) (DeviceCode, error) {
	dc := DeviceCode{UserCode: NormalizeUserCode(userCode)}
	var expires int64
	err := s.db.QueryRowContext(ctx, `SELECT device_name, platform, expires_at FROM device_codes
		WHERE user_code = ? AND expires_at > ? AND user_id IS NULL AND denied = 0`, dc.UserCode, now()).
		Scan(&dc.Device.Name, &dc.Device.Platform, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return dc, ErrNotFound
	}
	dc.ExpiresAt = time.Unix(expires, 0)
	return dc, err
}

// DecideDeviceCode approves a code for a user, or declines it.
func (s *Store) DecideDeviceCode(ctx context.Context, userCode string, userID int64, approve bool) error {
	q := "UPDATE device_codes SET user_id = ? WHERE user_code = ? AND expires_at > ? AND user_id IS NULL AND denied = 0"
	args := []any{userID, NormalizeUserCode(userCode), now()}
	if !approve {
		q = "UPDATE device_codes SET denied = 1 WHERE user_code = ? AND expires_at > ? AND user_id IS NULL AND denied = 0"
		args = args[1:]
	}
	res, err := s.db.ExecContext(ctx, q, args...)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// RedeemDeviceCode is the device's poll. Once the code is approved it returns
// the user and device, exactly once; the caller starts the session.
// Otherwise it returns ErrCodePending, ErrCodeDenied or ErrCodeExpired.
func (s *Store) RedeemDeviceCode(ctx context.Context, deviceCode string) (int64, Device, error) {
	var userID int64
	var dev Device
	// Deleting and reading in one statement means two polls can't both get a session.
	err := s.db.QueryRowContext(ctx, `DELETE FROM device_codes
		WHERE device_code = ? AND user_id IS NOT NULL AND expires_at > ?
		RETURNING user_id, device_name, platform`, deviceCode, now()).Scan(&userID, &dev.Name, &dev.Platform)
	if err == nil {
		return userID, dev, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, dev, err
	}
	var denied int
	var expires int64
	err = s.db.QueryRowContext(ctx, "SELECT denied, expires_at FROM device_codes WHERE device_code = ?", deviceCode).Scan(&denied, &expires)
	switch {
	case errors.Is(err, sql.ErrNoRows), err == nil && expires <= now():
		return 0, dev, ErrCodeExpired
	case err != nil:
		return 0, dev, err
	case denied != 0:
		s.db.ExecContext(ctx, "DELETE FROM device_codes WHERE device_code = ?", deviceCode)
		return 0, dev, ErrCodeDenied
	}
	return 0, dev, ErrCodePending
}

func isUniqueViolation(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE") || strings.Contains(msg, "unique") || strings.Contains(msg, "23505")
}
