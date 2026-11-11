package scan

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yeixio/matinee/internal/store/storetest"
)

func TestScheduleInterval(t *testing.T) {
	ctx := context.Background()
	s := &Schedule{Scanner: &Scanner{Store: storetest.Open(t), Log: quietLog()}, Default: 15 * time.Minute}
	if d, err := s.Interval(ctx); err != nil || d != 15*time.Minute {
		t.Fatalf("default: %v %v", d, err)
	}
	for _, bad := range []time.Duration{time.Minute, 14 * time.Minute, 8 * 24 * time.Hour, -time.Hour} {
		if err := s.SetInterval(ctx, bad); err == nil {
			t.Errorf("%v accepted", bad)
		}
	}
	for _, d := range []time.Duration{6 * time.Hour, 0} {
		if err := s.SetInterval(ctx, d); err != nil {
			t.Fatal(err)
		}
		if got, _ := s.Interval(ctx); got != d {
			t.Fatalf("set %v, got %v", d, got)
		}
	}
}

// Turning rescans off stops the next one; turning them back on schedules
// one that far ahead.
func TestScheduleRun(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := &Schedule{Scanner: &Scanner{Store: storetest.Open(t), Log: quietLog()}, Default: time.Hour}
	done := make(chan struct{})
	go func() { s.Run(ctx); close(done) }()
	waitNext := func(want time.Duration) {
		t.Helper()
		for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(10 * time.Millisecond) {
			next := s.Next()
			if want == 0 && next.IsZero() || want > 0 && !next.IsZero() && time.Until(next) > want-time.Minute && time.Until(next) <= want {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("want next in %v, got %v", want, next)
			}
		}
	}
	waitNext(time.Hour)
	if err := s.SetInterval(ctx, 0); err != nil {
		t.Fatal(err)
	}
	waitNext(0)
	if err := s.SetInterval(ctx, 24*time.Hour); err != nil {
		t.Fatal(err)
	}
	waitNext(24 * time.Hour)
	cancel()
	<-done
}

// A rescan that's due waits while something plays, then runs.
func TestScheduleWaitsForPlayback(t *testing.T) {
	old := playbackCheck
	playbackCheck = 10 * time.Millisecond
	t.Cleanup(func() { playbackCheck = old })
	var playing atomic.Bool
	playing.Store(true)
	s := &Schedule{Scanner: &Scanner{Store: storetest.Open(t), Log: quietLog()}, Wait: func(context.Context) bool { return playing.Load() }}
	done := make(chan struct{})
	go func() { s.waitForPlayback(context.Background()); close(done) }()
	select {
	case <-done:
		t.Fatal("didn't wait while something played")
	case <-time.After(50 * time.Millisecond):
	}
	playing.Store(false)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("still waiting after playback ended")
	}
}
