package scan

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/yeixio/matinee/internal/store"
)

// A scan started in the background counts as running as soon as Start
// returns, so a client that adds a library and then waits for its scan to
// finish never sees it idle before the scan has begun.
func TestStartIsRunningAtOnce(t *testing.T) {
	release := make(chan struct{})
	s := &Scanner{Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Refresh: func(ctx context.Context, lib store.Library) (int, int, error) {
			<-release
			return 0, 0, nil
		}}
	lib := store.Library{ID: 7, Name: "Shows", Kind: "podcasts"}
	if !s.Start(context.Background(), lib) {
		t.Fatal("Start refused an idle library")
	}
	if !s.Running(lib.ID) {
		t.Fatal("not running right after Start")
	}
	if s.Start(context.Background(), lib) {
		t.Fatal("Start ran a second scan of the same library")
	}
	if _, err := s.Scan(context.Background(), lib); err != ErrAlreadyRunning {
		t.Fatalf("Scan during a scan: %v", err)
	}
	close(release)
	deadline := time.Now().Add(5 * time.Second)
	for s.Running(lib.ID) {
		if time.Now().After(deadline) {
			t.Fatal("still running after the scan finished")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if _, _, last := s.Report(lib.ID); last == nil {
		t.Fatal("no report of the finished scan")
	}
}

// An admin can stop a running scan, whatever started it, and the report
// says it was stopped rather than that it failed.
func TestStopScan(t *testing.T) {
	s := &Scanner{Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Refresh: func(ctx context.Context, lib store.Library) (int, int, error) {
			<-ctx.Done()
			return 0, 0, ctx.Err()
		}}
	lib := store.Library{ID: 7, Name: "Shows", Kind: "podcasts"}
	if s.Stop(lib.ID) {
		t.Fatal("stopped a library that wasn't being scanned")
	}
	if !s.Start(WithReason(context.Background(), ReasonManual), lib) {
		t.Fatal("Start refused an idle library")
	}
	if since, why, _ := s.Report(lib.ID); since.IsZero() || why != ReasonManual || s.Reason(lib.ID) != ReasonManual {
		t.Fatalf("running: since %v, reason %q", since, why)
	}
	if !s.Stop(lib.ID) {
		t.Fatal("Stop didn't find the running scan")
	}
	deadline := time.Now().Add(5 * time.Second)
	for s.Running(lib.ID) {
		if time.Now().After(deadline) {
			t.Fatal("still running after Stop")
		}
		time.Sleep(5 * time.Millisecond)
	}
	_, why, last := s.Report(lib.ID)
	if why != "" || last == nil || !last.Stopped || last.Error != "" || last.Reason != ReasonManual {
		t.Fatalf("after Stop: reason %q, report %+v", why, last)
	}
	// The library can be scanned again.
	if !s.Start(context.Background(), lib) || s.Reason(lib.ID) != ReasonServer {
		t.Fatal("no scan after a stopped one")
	}
	s.Stop(lib.ID)
}
