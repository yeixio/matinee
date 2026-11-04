package stream

import (
	"context"
	"io"
	"log/slog"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"

	"github.com/yeixio/matinee/internal/store"
)

func TestAudioDelayFilter(t *testing.T) {
	for ms, want := range map[int]string{
		0:     "",
		250:   "adelay=delays=250:all=1",
		-125:  "atrim=start=0.125,asetpts=PTS-STARTPTS",
		9000:  "adelay=delays=2000:all=1",
		-9000: "atrim=start=2.000,asetpts=PTS-STARTPTS",
	} {
		if got := AudioDelayFilter(ms); got != want {
			t.Errorf("%d: got %q, want %q", ms, got, want)
		}
	}
}

// A stream with the sound a second later starts with a second of silence.
func TestStartDelaysAudio(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "clip.mp4")
	gen := exec.Command(ffmpeg, "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "testsrc2=size=320x180:rate=12:duration=4",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=4",
		"-c:v", "libx264", "-preset", "ultrafast", "-c:a", "aac", src)
	if out, err := gen.CombinedOutput(); err != nil {
		t.Skipf("can't make a clip here: %v\n%s", err, out)
	}
	tr := NewTranscoder(ffmpeg, filepath.Join(dir, "cache"), nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer tr.Close()
	it := store.Item{ID: 1, Path: src, VideoCodec: "h264", AudioCodec: "aac", Width: intp(320)}
	id, err := tr.StartFor(context.Background(), 7, it, 0, Request{BurnSub: -1, AudioDelayMs: 1000})
	if err != nil {
		t.Fatal(err)
	}
	seg := filepath.Join(dir, "cache", id, "seg00000.ts")
	out, _ := exec.Command(ffmpeg, "-hide_banner", "-i", seg, "-af", "silencedetect=noise=-50dB:d=0.3", "-f", "null", "-").CombinedOutput()
	m := regexp.MustCompile(`silence_end: ([0-9.]+)`).FindSubmatch(out)
	if m == nil {
		t.Fatalf("no silence at the start:\n%s", out)
	}
	if end, _ := strconv.ParseFloat(string(m[1]), 64); end < 0.8 || end > 1.3 {
		t.Errorf("silence ends at %v, want about 1s", end)
	}
}
