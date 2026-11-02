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
	if _, last := s.Report(lib.ID); last == nil {
		t.Fatal("no report of the finished scan")
	}
}
