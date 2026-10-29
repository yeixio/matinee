package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSignInGuard(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	g := &signInGuard{now: func() time.Time { return now }}
	user := []string{"user:sam"}

	for i := range signInFreeTries - 1 {
		g.record(user, false)
		if wait := g.locked(user); wait != 0 {
			t.Fatalf("locked after %d wrong passwords: %v", i+1, wait)
		}
	}
	// Each wrong password past the free ones locks twice as long.
	for _, want := range []time.Duration{30 * time.Second, time.Minute, 2 * time.Minute} {
		g.record(user, false)
		if wait := g.locked(user); wait != want {
			t.Fatalf("locked for %v, want %v", wait, want)
		}
		now = now.Add(want)
		if wait := g.locked(user); wait != 0 {
			t.Fatalf("still locked after %v: %v", want, wait)
		}
	}
	for range 20 {
		g.record(user, false)
	}
	if wait := g.locked(user); wait != signInMaxLock {
		t.Fatalf("locked for %v, want at most %v", wait, signInMaxLock)
	}

	// A right password starts over.
	now = now.Add(signInMaxLock)
	g.record(user, true)
	g.record(user, false)
	if wait := g.locked(user); wait != 0 {
		t.Fatalf("locked after a right password and one wrong one: %v", wait)
	}

	// So does a day without trying.
	for range signInFreeTries {
		g.record(user, false)
	}
	now = now.Add(signInForget + time.Minute)
	g.record(user, false)
	if wait := g.locked(user); wait != 0 {
		t.Fatalf("a day later, locked after one wrong password: %v", wait)
	}

	// An address gets more tries, since it may be a household's.
	addr := []string{"addr:203.0.113.9"}
	for range signInFreeAddrTries - 1 {
		g.record(addr, false)
	}
	if g.locked(addr) != 0 {
		t.Fatal("address locked too soon")
	}
	g.record(addr, false)
	if g.locked(addr) == 0 {
		t.Fatal("address never locked")
	}
}

func TestSignInLimit(t *testing.T) {
	c, _ := newTestServer(t)
	var login struct{ Token string }
	c.do("POST", "/api/v1/setup", credentials{"admin", "correct horse"}, &login)

	try := func(addr, password string, headers ...string) *httptest.ResponseRecorder {
		body, _ := json.Marshal(credentials{"admin", password})
		r := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(body))
		r.RemoteAddr = addr
		for i := 0; i+1 < len(headers); i += 2 {
			r.Header.Set(headers[i], headers[i+1])
		}
		w := httptest.NewRecorder()
		c.srv.Handler().ServeHTTP(w, r)
		return w
	}

	const outside = "203.0.113.9:4000"
	for range signInFreeTries {
		if w := try(outside, "wrong password"); w.Code != http.StatusUnauthorized {
			t.Fatalf("wrong password: %d", w.Code)
		}
	}
	// Locked now, even with the right password, and from another address.
	for _, addr := range []string{outside, "198.51.100.4:4000"} {
		w := try(addr, "correct horse")
		if w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") == "" {
			t.Fatalf("from %s while locked: %d, Retry-After %q", addr, w.Code, w.Header().Get("Retry-After"))
		}
	}
	// Through a proxy it's still the same username.
	if w := try("10.42.0.15:4000", "correct horse", "X-Forwarded-For", "198.51.100.4"); w.Code != http.StatusTooManyRequests {
		t.Fatalf("through a proxy while locked: %d", w.Code)
	}
	// At home it always works, and clears the lock.
	if w := try("192.168.1.20:4000", "correct horse"); w.Code != http.StatusOK {
		t.Fatalf("from home while locked: %d", w.Code)
	}
	for range signInFreeTries {
		try("192.168.1.20:4000", "wrong password")
	}
	if w := try(outside, "correct horse"); w.Code != http.StatusOK {
		t.Fatalf("wrong passwords at home locked the account outside: %d", w.Code)
	}
}
