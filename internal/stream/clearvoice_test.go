package stream

import (
	"context"
	"io"
	"log/slog"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/yeixio/matinee/internal/store"
)

func TestAudioFilters(t *testing.T) {
	if got := audioFilters(0, false); got != "" {
		t.Errorf("none: %q", got)
	}
	if got := audioFilters(100, true); !strings.HasPrefix(got, "adelay=delays=100:all=1,aformat=channel_layouts=5.1,pan=stereo") {
		t.Errorf("both: %q", got)
	}
}

// TestStartClearVoice plays a 5.1 clip whose dialogue (a quiet tone in the
// centre) sits under loud sound on the sides, and checks Clear voice brings
// the dialogue closer to the rest.
func TestStartClearVoice(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "film.mkv")
	gen := exec.Command(ffmpeg, "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "testsrc2=size=320x180:rate=12:duration=4",
		"-f", "lavfi", "-i", "aevalsrc=0.5*sin(300*2*PI*t)|0.5*sin(300*2*PI*t)|0.05*sin(1000*2*PI*t)|0.5*sin(60*2*PI*t)|0|0:c=5.1:d=4",
		"-c:v", "libx264", "-preset", "ultrafast", "-c:a", "flac", src)
	if out, err := gen.CombinedOutput(); err != nil {
		t.Skipf("can't make a clip here: %v\n%s", err, out)
	}
	tr := NewTranscoder(ffmpeg, filepath.Join(dir, "cache"), nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer tr.Close()
	it := store.Item{ID: 1, Path: src, VideoCodec: "h264", AudioCodec: "flac", Width: intp(320)}
	// gap is how far below the side sound the dialogue is, in dB.
	gap := func(user int64, voice bool) float64 {
		t.Helper()
		id, err := tr.StartFor(context.Background(), user, it, 0, Request{BurnSub: -1, ClearVoice: voice})
		if err != nil {
			t.Fatal(err)
		}
		seg := filepath.Join(dir, "cache", id, "seg00000.ts")
		level := func(hz string) float64 {
			out, _ := exec.Command(ffmpeg, "-hide_banner", "-i", seg, "-af",
				"bandpass=f="+hz+":width_type=q:w=8,atrim=start=1,astats=measure_overall=RMS_level:measure_perchannel=none", "-f", "null", "-").CombinedOutput()
			m := regexp.MustCompile(`RMS level dB: (-?[0-9.]+)`).FindSubmatch(out)
			if m == nil {
				t.Fatalf("no level for %s Hz:\n%s", hz, out)
			}
			v, _ := strconv.ParseFloat(string(m[1]), 64)
			return v
		}
		return level("300") - level("1000")
	}
	plain, clear := gap(1, false), gap(2, true)
	if clear > plain-4 {
		t.Errorf("dialogue is %.1f dB under the rest with Clear voice, %.1f dB without; want at least 4 dB closer", clear, plain)
	}
}
