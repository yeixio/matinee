package stream

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Speed comes from the pieces FFmpeg has written: 5 pieces of 4 seconds
// in 10 seconds is twice real time. Only the first stream of an adaptive
// one counts.
func TestSessionSpeed(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{"seg00000.ts", "seg00001.ts", "seg00002.ts", "seg00003.ts", "seg00004.ts", "index.m3u8", "seg_1_00000.ts"} {
		os.WriteFile(filepath.Join(dir, f), nil, 0o644)
	}
	now := time.Now()
	s := &session{dir: dir, started: now.Add(-10 * time.Second), done: make(chan struct{})}
	if got := s.speed(now); got != 2 {
		t.Fatalf("speed %v, want 2", got)
	}
	close(s.done)
	if got := s.speed(now); got != 0 {
		t.Fatalf("finished: %v", got)
	}
}

func TestReadProbe(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f")
	os.WriteFile(p, make([]byte, 6<<20), 0o644)
	ms, bps, ok := ReadProbe(p, 5<<20, time.Second)
	if !ok || ms >= 1000 || bps <= 0 {
		t.Fatalf("got %d ms %d bps %v", ms, bps, ok)
	}
	if _, _, ok := ReadProbe(filepath.Join(t.TempDir(), "missing"), 0, time.Second); ok {
		t.Fatal("missing file probed")
	}
}
