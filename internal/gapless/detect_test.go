package gapless

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/yeixio/matinee/internal/probe"
	"github.com/yeixio/matinee/internal/store"
)

func TestTagged(t *testing.T) {
	for _, c := range []struct {
		tags  map[string]string
		album string
		want  bool
	}{
		{map[string]string{"gapless_playback": "1"}, "Blue", true},
		{map[string]string{"itunesgapless": "1"}, "Blue", true},
		{map[string]string{"itunesgapless": "0"}, "Blue", false},
		{map[string]string{"releasetype": "album; dj-mix"}, "Tranceport", true},
		{map[string]string{"musicbrainz album type": "album/live"}, "Alive", true},
		{map[string]string{"genre": "DJ Mix"}, "Anything", true},
		{map[string]string{"genre": "Trance"}, "Global Underground 007 (Mixed by Paul Oakenfold)", true},
		{map[string]string{"genre": "Rock"}, "Nevermind", false},
		{map[string]string{}, "Back to Mine (Continuous Mix)", true},
		{map[string]string{}, "Remixed", false},
	} {
		if got := Tagged(c.tags, c.album); got != c.want {
			t.Errorf("Tagged(%v, %q) = %v, want %v", c.tags, c.album, got, c.want)
		}
	}
}

func TestJoined(t *testing.T) {
	if Joined(nil) {
		t.Error("no joins counted as joined")
	}
	if !Joined([]bool{true, true, false}) {
		t.Error("two of three should do")
	}
	if Joined([]bool{true, false, false}) {
		t.Error("one of three shouldn't")
	}
}

func TestParseMean(t *testing.T) {
	if v, err := parseMean("[Parsed_volumedetect_0 @ 0x1] mean_volume: -27.6 dB\n"); err != nil || v != -27.6 {
		t.Errorf("got %v, %v", v, err)
	}
	if v, err := parseMean("mean_volume: -inf dB"); err != nil || v > -100 {
		t.Errorf("got %v, %v", v, err)
	}
	if _, err := parseMean("nothing"); err == nil {
		t.Error("expected an error")
	}
}

// TestListens makes two albums with FFmpeg: a continuous tone cut into
// songs, which runs on where they meet, and songs that fade out and start
// from quiet.
func TestListens(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("no ffmpeg")
	}
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("no ffprobe")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		if out, err := exec.Command(ffmpeg, append([]string{"-v", "error", "-y"}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("ffmpeg %v: %v\n%s", args, err, out)
		}
	}
	full := filepath.Join(dir, "full.wav")
	run("-f", "lavfi", "-i", "sine=frequency=440:duration=18", full)
	var mix, normal []store.GaplessTrack
	for i := range 3 {
		p := filepath.Join(dir, fmt.Sprintf("mix%d.mp3", i))
		run("-ss", strconv.Itoa(i*6), "-t", "6", "-i", full, "-c:a", "libmp3lame", p)
		mix = append(mix, store.GaplessTrack{Path: p})
		q := filepath.Join(dir, fmt.Sprintf("song%d.mp3", i))
		run("-f", "lavfi", "-i", fmt.Sprintf("sine=frequency=%d:duration=5", 300+100*i),
			"-af", "adelay=300|300,afade=t=in:st=0.3:d=0.5,afade=t=out:st=3.5:d=1.5,apad=pad_dur=0.5", "-c:a", "libmp3lame", q)
		normal = append(normal, store.GaplessTrack{Path: q})
	}
	d := &Detector{FFmpeg: ffmpeg, Prober: probe.Prober{Path: ffprobe}, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	ctx := context.Background()
	if found, err := d.Album(ctx, store.GaplessAlbum{Title: "Mix", Tracks: mix}); err != nil || !found {
		t.Errorf("continuous album: found %v, %v", found, err)
	}
	if found, err := d.Album(ctx, store.GaplessAlbum{Title: "Songs", Tracks: normal}); err != nil || found {
		t.Errorf("album with gaps: found %v, %v", found, err)
	}
	// One song is no album to join.
	if found, _ := d.Album(ctx, store.GaplessAlbum{Title: "Mix", Tracks: mix[:1]}); found {
		t.Error("one song found gapless")
	}
	// The end of a disc isn't a place songs meet.
	split := []store.GaplessTrack{normal[0], {Path: mix[1].Path, Disc: 2}, {Path: mix[2].Path, Disc: 2}}
	if found, _ := d.Album(ctx, store.GaplessAlbum{Title: "Two discs", Tracks: split}); !found {
		t.Error("the join across discs counted")
	}
}
