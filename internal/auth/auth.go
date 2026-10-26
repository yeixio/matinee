// Package auth handles passwords and turns request credentials into a user.
package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/yeixio/matinee/internal/store"
)

const (
	CookieName = "matinee_session"
	SessionTTL = 90 * 24 * time.Hour
)

var ErrBadCredentials = errors.New("wrong username or password")

var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("matinee"), bcrypt.DefaultCost)

func HashPassword(pw string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	return string(b), err
}

// Login checks a password and starts a session for a device.
func Login(ctx context.Context, s *store.Store, username, password string, dev store.Device) (store.User, string, error) {
	u, err := s.UserByName(ctx, username)
	if errors.Is(err, store.ErrNotFound) {
		// Spend the same time as a real check so usernames can't be probed by timing.
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
		return u, "", ErrBadCredentials
	}
	if err != nil {
		return u, "", err
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash()), []byte(password)) != nil {
		return u, "", ErrBadCredentials
	}
	token, err := s.CreateSession(ctx, u.ID, SessionTTL, dev)
	return u, token, err
}

// Token pulls a session token from the Authorization header, the session
// cookie, or a ?token= query parameter (for <video> and <audio> sources in apps).
func Token(r *http.Request) string {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimPrefix(h, "Bearer ")
	}
	if c, err := r.Cookie(CookieName); err == nil {
		return c.Value
	}
	return r.URL.Query().Get("token")
}

type ctxKey struct{}

func WithUser(ctx context.Context, u store.User) context.Context {
	return context.WithValue(ctx, ctxKey{}, u)
}

func UserFrom(ctx context.Context) (store.User, bool) {
	u, ok := ctx.Value(ctxKey{}).(store.User)
	return u, ok
}

// BrowserName describes a browser from its User-Agent, such as "Firefox on macOS",
// for the list of signed-in devices.
func BrowserName(userAgent string) string {
	browser := ""
	for _, b := range []struct{ token, name string }{
		{"Edg/", "Edge"}, {"OPR/", "Opera"}, {"Firefox/", "Firefox"}, {"Chrome/", "Chrome"}, {"Safari/", "Safari"},
	} {
		if strings.Contains(userAgent, b.token) {
			browser = b.name
			break
		}
	}
	os := ""
	for _, o := range []struct{ token, name string }{
		{"iPhone", "iPhone"}, {"iPad", "iPad"}, {"Android", "Android"}, {"Mac OS X", "macOS"},
		{"Windows", "Windows"}, {"CrOS", "ChromeOS"}, {"Linux", "Linux"},
	} {
		if strings.Contains(userAgent, o.token) {
			os = o.name
			break
		}
	}
	switch {
	case browser != "" && os != "":
		return browser + " on " + os
	case browser != "":
		return browser
	case os != "":
		return os
	}
	return "Web browser"
}

// ValidPIN reports whether a profile PIN is four digits.
func ValidPIN(pin string) bool {
	if len(pin) != 4 {
		return false
	}
	for _, r := range pin {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// CheckPIN compares a PIN with a profile's stored hash.
func CheckPIN(hash, pin string) bool {
	return hash != "" && ValidPIN(pin) && bcrypt.CompareHashAndPassword([]byte(hash), []byte(pin)) == nil
}
