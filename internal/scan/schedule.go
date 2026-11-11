package scan

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"
)

// SettingScanInterval is how often every library is rescanned in full, in
// seconds; "0" turns full rescans off. Unset, the server's
// MATINEE_SCAN_INTERVAL applies.
const SettingScanInterval = "scan_interval"

// The intervals an admin can pick from, besides off. Shorter than
// MinScanInterval would rescan a big library on a network share before
// the last rescan finished.
const (
	MinScanInterval = 15 * time.Minute
	MaxScanInterval = 7 * 24 * time.Hour
)

// ValidScanInterval says whether d is an interval an admin can pick.
func ValidScanInterval(d time.Duration) bool {
	return d == 0 || d >= MinScanInterval && d <= MaxScanInterval
}

// Schedule rescans every library in full every so often, to catch what
// the watcher can't, such as a file replaced in place on a share. The
// admin sets how often in Settings.
type Schedule struct {
	Scanner *Scanner
	// Default is how often until the admin changes it: MATINEE_SCAN_INTERVAL.
	Default time.Duration

	mu     sync.Mutex
	next   time.Time // when the next rescan starts; zero when they're off
	change chan struct{}
}

func (s *Schedule) init() {
	if s.change == nil {
		s.change = make(chan struct{}, 1)
	}
}

// Interval returns how often every library is rescanned; 0 means never.
func (s *Schedule) Interval(ctx context.Context) (time.Duration, error) {
	v, err := s.Scanner.Store.Setting(ctx, SettingScanInterval)
	if err != nil || v == "" {
		return s.Default, err
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n < 0 {
		return s.Default, nil
	}
	return time.Duration(n) * time.Second, nil
}

// SetInterval changes how often every library is rescanned; 0 turns full
// rescans off. The next rescan is that long from now.
func (s *Schedule) SetInterval(ctx context.Context, d time.Duration) error {
	if !ValidScanInterval(d) {
		return fmt.Errorf("a full rescan can run every %v to %v, or never", MinScanInterval, MaxScanInterval)
	}
	if err := s.Scanner.Store.SetSetting(ctx, SettingScanInterval, strconv.FormatInt(int64(d/time.Second), 10)); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.init()
	s.next = time.Time{}
	if d > 0 {
		s.next = time.Now().Add(d)
	}
	select {
	case s.change <- struct{}{}:
	default:
	}
	return nil
}

// Next says when the next full rescan starts; zero when they're off.
func (s *Schedule) Next() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.next
}

// Run rescans every library once at startup, when rescans are on, and then
// every interval until ctx ends.
func (s *Schedule) Run(ctx context.Context) {
	s.mu.Lock()
	s.init()
	s.mu.Unlock()
	ctx = WithReason(ctx, ReasonSchedule)
	if d, _ := s.Interval(ctx); d > 0 {
		s.Scanner.ScanAll(ctx)
	}
	for {
		d, err := s.Interval(ctx)
		if err != nil {
			s.Scanner.Log.Warn("scan schedule", "err", err)
		}
		var t *time.Timer
		var wait <-chan time.Time
		next := time.Time{}
		if d > 0 {
			t = time.NewTimer(d)
			wait, next = t.C, time.Now().Add(d)
		}
		s.mu.Lock()
		if len(s.change) == 0 { // else a newer interval came in meanwhile, and set its own
			s.next = next
		}
		s.mu.Unlock()
		select {
		case <-ctx.Done():
		case <-s.change:
		case <-wait:
			s.Scanner.ScanAll(ctx)
		}
		if t != nil {
			t.Stop()
		}
		if ctx.Err() != nil {
			return
		}
	}
}
