package analyze

import (
	"context"
	"io"
	"log/slog"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yeixio/matinee/internal/probe"
	"github.com/yeixio/matinee/internal/scan"
	"github.com/yeixio/matinee/internal/store/storetest"
)

// Gentle analysis waits while someone is watching, then carries on.
func TestGentleWaitsForPlayback(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	gentlePause, busyCheck = 10*time.Millisecond, 10*time.Millisecond
	root := t.TempDir()
	file := filepath.Join(root, "Film (2020).mkv")
	if out, err := exec.Command(ffmpeg, "-v", "error", "-f", "lavfi", "-i", "testsrc2=s=640x360:d=0.2", "-c:v", "libx264", "-preset", "ultrafast", file).CombinedOutput(); err != nil {
		t.Fatalf("make file: %v\n%s", err, out)
	}
	st := storetest.Open(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	lib, _ := st.CreateLibrary(ctx, "Movies", "movies", []string{root})
	if _, err := (&scan.Scanner{Store: st, Prober: probe.Prober{Path: "ffprobe"}, Log: log}).Scan(ctx, lib); err != nil {
		t.Fatal(err)
	}

	var busy atomic.Bool
	busy.Store(true)
	a := &Analyzer{Store: st, Prober: probe.Prober{Path: "ffprobe", LowPriority: true}, Log: log, Busy: busy.Load}
	stopped := make(chan struct{})
	go func() { a.Run(ctx); close(stopped) }()
	defer func() { cancel(); <-stopped }()

	wait := func(state string, done int) Status {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for {
			s, err := a.Status(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if s.State == state && s.Done == done {
				return s
			}
			if time.Now().After(deadline) {
				t.Fatalf("want %s with %d done, got %+v", state, done, s)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	if s := wait(StateWaiting, 0); s.Mode != Gentle || s.Total != 1 {
		t.Fatalf("status: %+v", s)
	}
	busy.Store(false)
	wait(StateIdle, 1)
	top, _ := st.TopLevel(ctx, 0, lib.ID)
	if len(top) != 1 || len(top[0].MediaTags) == 0 || top[0].MediaTags[0] != "sd" {
		t.Fatalf("tags: %+v", top)
	}
}
