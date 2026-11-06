package stream

import (
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/yeixio/matinee/internal/store"
)

// A movie in parts reads as one video, the parts one after the other.
func TestInputJoinsParts(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe not installed")
	}
	dir := t.TempDir()
	var parts []store.FilePart
	for _, name := range []string{"It's CD1.mkv", "It's CD2.mkv"} {
		p := filepath.Join(dir, name)
		if out, err := exec.Command(ffmpeg, "-loglevel", "error", "-f", "lavfi", "-i", "testsrc=d=1:s=64x36", p).CombinedOutput(); err != nil {
			t.Fatalf("make video: %v\n%s", err, out)
		}
		parts = append(parts, store.FilePart{Path: p, DurationMs: 1000})
	}
	in := Input(store.Item{Path: parts[0].Path, Parts: parts})
	if in[0] != "-f" || in[1] != "concat" {
		t.Fatalf("input: %v", in)
	}
	out := filepath.Join(dir, "joined.mkv")
	args := append([]string{"-loglevel", "error"}, in...)
	if b, err := exec.Command(ffmpeg, append(args, "-c", "copy", out)...).CombinedOutput(); err != nil {
		t.Fatalf("join: %v\n%s", err, b)
	}
	b, err := exec.Command(ffprobe, "-v", "error", "-show_entries", "format=duration", "-of", "csv=p=0", out).Output()
	if err != nil {
		t.Fatal(err)
	}
	if d, _ := strconv.ParseFloat(strings.TrimSpace(string(b)), 64); d < 1.9 {
		t.Fatalf("joined length %v, want 2s", d)
	}
	if one := Input(store.Item{Path: parts[0].Path}); len(one) != 2 || one[1] != parts[0].Path {
		t.Fatalf("one file: %v", one)
	}
}
