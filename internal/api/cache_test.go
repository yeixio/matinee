package api

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// A page never waits long on another service: an answer it doesn't have
// yet gives up after cacheWait and arrives for the next page, and a stale
// one comes back at once while a fresh one is fetched.
func TestCachedNeverWaitsLong(t *testing.T) {
	old := cacheWait
	cacheWait = 50 * time.Millisecond
	t.Cleanup(func() { cacheWait = old })
	var c ttlCache
	release := make(chan struct{})
	var calls atomic.Int32
	slow := func(ctx context.Context) (int, error) {
		calls.Add(1)
		select {
		case <-release:
			return int(calls.Load()), nil
		case <-ctx.Done():
			return 0, ctx.Err()
		}
	}
	start := time.Now()
	if _, err := cached(&c, "k", time.Hour, slow); !errors.Is(err, errSlow) || time.Since(start) > time.Second {
		t.Fatalf("first ask: %v after %v", err, time.Since(start))
	}
	// A second page while it's still being fetched doesn't start another fetch.
	if _, err := cached(&c, "k", time.Hour, slow); !errors.Is(err, errSlow) || calls.Load() != 1 {
		t.Fatalf("second ask: %v, %d fetches", err, calls.Load())
	}
	close(release)
	for deadline := time.Now().Add(time.Second); ; time.Sleep(5 * time.Millisecond) {
		if v, err := cached(&c, "k", time.Hour, slow); err == nil {
			if v != 1 {
				t.Fatalf("got %d", v)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the answer never arrived")
		}
	}

	// Stale: answered at once, refreshed behind the page's back.
	c.mu.Lock()
	c.entries["k"] = cacheEntry{time.Now().Add(-time.Minute), 7}
	c.mu.Unlock()
	hang := make(chan struct{})
	defer close(hang)
	start = time.Now()
	v, err := cached(&c, "k", time.Hour, func(ctx context.Context) (int, error) { <-hang; return 8, nil })
	if err != nil || v != 7 || time.Since(start) > 20*time.Millisecond {
		t.Fatalf("stale: %d %v after %v", v, err, time.Since(start))
	}
}

// A fetch that fails is reported to whoever waited, and isn't kept.
func TestCachedReportsErrors(t *testing.T) {
	var c ttlCache
	boom := errors.New("boom")
	if _, err := cached(&c, "k", time.Hour, func(ctx context.Context) (int, error) { return 0, boom }); !errors.Is(err, boom) {
		t.Fatalf("got %v", err)
	}
	if v, err := cached(&c, "k", time.Hour, func(ctx context.Context) (int, error) { return 3, nil }); err != nil || v != 3 {
		t.Fatalf("after an error: %d %v", v, err)
	}
}
