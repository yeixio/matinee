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

// Login checks a password and starts a session.
func Login(ctx context.Context, s *store.Store, username, password string) (store.User, string, error) {
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
	token, err := s.CreateSession(ctx, u.ID, SessionTTL)
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
