package stream

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/yeixio/matinee/internal/store"
)

func TestOffsets(t *testing.T) {
	ms := func(n int64) *int64 { return &n }
	tracks := []store.Item{{DurationMs: ms(10_000)}, {DurationMs: ms(5_500)}, {DurationMs: ms(1_000)}}
	if got := Offsets(tracks, 0); !slices.Equal(got, []float64{0, 10, 15.5}) {
		t.Errorf("from the top: %v", got)
	}
	if got := Offsets(tracks, 4); !slices.Equal(got, []float64{-4, 6, 11.5}) {
		t.Errorf("from 4s in: %v", got)
	}
}

// TestGaplessJoins cuts a continuous tone into MP3 songs, joins them as a
// gapless stream does, and listens for silence where they meet: MP3
// encoders pad each file's start and end, which a plain join would play.
func TestGaplessJoins(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("no ffmpeg")
	}
	dir := t.TempDir()
	run := func(args ...string) string {
		t.Helper()
		out, err := exec.Command(ffmpeg, append([]string{"-hide_banner", "-nostats", "-y"}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("ffmpeg %v: %v\n%s", args, err, out)
		}
		return string(out)
	}
	full := filepath.Join(dir, "full.wav")
	run("-v", "error", "-f", "lavfi", "-i", "sine=frequency=440:duration=12", full)
	var tracks []store.Item
	for i := range 4 {
		p := filepath.Join(dir, fmt.Sprintf("%d.mp3", i))
		run("-v", "error", "-ss", strconv.Itoa(i*3), "-t", "3", "-i", full, "-c:a", "libmp3lame", p)
		ms := int64(3000)
		tracks = append(tracks, store.Item{ID: int64(i + 1), Path: p, DurationMs: &ms})
	}
	args := gaplessArgs(tracks, 1, 11)
	if f := args[slices.Index(args, "-filter_complex")+1]; !strings.HasPrefix(f, "[0:a:0]atrim=start=1.000,") {
		t.Fatalf("the first song should start partway: %s", f)
	}
	out := filepath.Join(dir, "out.wav")
	args = append(args[:len(args)-4], "-c:a", "pcm_s16le", out)
	run(append([]string{"-v", "error"}, args...)...)
	// 11 seconds of tone, and not a moment of quiet in it.
	log := run("-i", out, "-af", "silencedetect=noise=-50dB:d=0.005", "-f", "null", "-")
	if strings.Contains(log, "silence_start") {
		t.Errorf("a gap where songs meet:\n%s", log)
	}
	if !strings.Contains(log, "Duration: 00:00:11.0") {
		t.Errorf("expected 11 seconds:\n%s", log)
	}
}

func TestVODPlaylist(t *testing.T) {
	for _, c := range []struct {
		length float64
		pieces int
	}{{11, 3}, {12, 4}, {11.95, 4}, {0.2, 1}, {4.5, 2}} {
		pl, padTo := vodPlaylist(c.length)
		if n := strings.Count(string(pl), ".ts"); n != c.pieces {
			t.Errorf("%v seconds: %d pieces, want %d", c.length, n, c.pieces)
		}
		if padTo < c.length || padTo > float64(c.pieces*segmentSeconds)-0.1 || padTo < float64((c.pieces-1)*segmentSeconds)+0.5 {
			t.Errorf("%v seconds: padded to %v", c.length, padTo)
		}
		if !strings.HasSuffix(string(pl), "#EXT-X-ENDLIST\n") || !strings.Contains(string(pl), "#EXT-X-PLAYLIST-TYPE:VOD") {
			t.Errorf("not a whole playlist:\n%s", pl)
		}
	}
}
