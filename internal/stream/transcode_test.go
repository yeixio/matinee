package stream

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/yeixio/matinee/internal/store"
)

// Dragging the seek bar sends overlapping starts; the replaced ones must say so
// instead of failing like a broken file.
func TestStartSupersededByNewerSeek(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "clip.mkv")
	gen := exec.Command(ffmpeg, "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "testsrc=size=640x360:rate=24:duration=60",
		"-c:v", "mpeg4", src)
	if out, err := gen.CombinedOutput(); err != nil {
		t.Fatalf("make clip: %v\n%s", err, out)
	}

	tr := NewTranscoder(ffmpeg, filepath.Join(dir, "cache"), slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer tr.Close()
	it := store.Item{ID: 1, Path: src, VideoCodec: "mpeg4"}

	first := make(chan error, 1)
	go func() {
		_, err := tr.Start(context.Background(), 7, it, 10, -1)
		first <- err
	}()
	// Let the first FFmpeg get going, then replace it.
	for {
		tr.mu.Lock()
		n := len(tr.sessions)
		tr.mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	id, err := tr.Start(context.Background(), 7, it, 40, -1)
	if err != nil {
		t.Fatalf("newer start: %v", err)
	}
	if id == "" {
		t.Fatal("newer start returned no session")
	}
	if err := <-first; !errors.Is(err, ErrSuperseded) {
		t.Fatalf("replaced start returned %v, want ErrSuperseded", err)
	}
}
