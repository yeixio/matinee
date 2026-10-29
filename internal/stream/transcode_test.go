package stream

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/yeixio/matinee/internal/hwaccel"
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

	tr := NewTranscoder(ffmpeg, filepath.Join(dir, "cache"), nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer tr.Close()
	it := store.Item{ID: 1, Path: src, VideoCodec: "mpeg4"}

	first := make(chan error, 1)
	go func() {
		_, err := tr.Start(context.Background(), 7, it, 10, -1, 0)
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
	id, err := tr.Start(context.Background(), 7, it, 40, -1, 0)
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

func TestSignPlaylist(t *testing.T) {
	in := "#EXTM3U\n#EXT-X-TARGETDURATION:4\n#EXTINF:4.0,\nseg00000.ts\n#EXTINF:4.0,\nseg00001.ts\n"
	want := "#EXTM3U\n#EXT-X-TARGETDURATION:4\n#EXTINF:4.0,\nseg00000.ts?token=a%2Bb\n#EXTINF:4.0,\nseg00001.ts?token=a%2Bb\n"
	if got := string(signPlaylist([]byte(in), "a+b")); got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}

func TestStartAtBeginning(t *testing.T) {
	in := "#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-PLAYLIST-TYPE:EVENT\n"
	want := "#EXTM3U\n#EXT-X-START:TIME-OFFSET=0,PRECISE=YES\n#EXT-X-VERSION:3\n#EXT-X-PLAYLIST-TYPE:EVENT\n"
	if got := string(startAtBeginning([]byte(in))); got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
	if got := string(startAtBeginning([]byte(want))); got != want {
		t.Fatalf("added a second start:\n%s", got)
	}
}

type fakeAccel struct{ a *hwaccel.Accel }

func (f fakeAccel) Active() *hwaccel.Accel { return f.a }

// A GPU that can't do the job (here, one that isn't there) costs a moment,
// not the playback: the transcode starts again on the CPU, and the item's
// next transcode goes straight there.
func TestStartFallsBackToCPU(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "clip.mkv")
	gen := exec.Command(ffmpeg, "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "testsrc=size=640x360:rate=24:duration=10",
		"-c:v", "mpeg4", src)
	if out, err := gen.CombinedOutput(); err != nil {
		t.Fatalf("make clip: %v\n%s", err, out)
	}

	var logs strings.Builder
	gpu := &hwaccel.Accel{Kind: hwaccel.VAAPI, Device: filepath.Join(dir, "no-such-gpu"), Decode: true}
	tr := NewTranscoder(ffmpeg, filepath.Join(dir, "cache"), fakeAccel{gpu}, slog.New(slog.NewTextHandler(&logs, nil)))
	defer tr.Close()
	it := store.Item{ID: 1, Path: src, VideoCodec: "mpeg4"}

	if got := tr.plan(gpu, it.ID, hwaccel.Video{Codec: "mpeg4", BurnSub: -1}); !slices.Equal(got, []hwaccel.Mode{hwaccel.Encode, hwaccel.CPU}) {
		t.Fatalf("plan before failing: %v", got)
	}
	id, err := tr.Start(context.Background(), 7, it, 0, -1, 0)
	if err != nil {
		t.Fatalf("start: %v\n%s", err, logs.String())
	}
	if id == "" || !strings.Contains(logs.String(), "transcode failed on the GPU") {
		t.Fatalf("didn't fall back:\n%s", logs.String())
	}
	if got := tr.plan(gpu, it.ID, hwaccel.Video{Codec: "mpeg4", BurnSub: -1}); !slices.Equal(got, []hwaccel.Mode{hwaccel.CPU}) {
		t.Fatalf("plan after failing: %v", got)
	}
	// Another item still gets the GPU first.
	if got := tr.plan(gpu, 2, hwaccel.Video{Codec: "hevc", BurnSub: -1}); !slices.Equal(got, []hwaccel.Mode{hwaccel.Full, hwaccel.Encode, hwaccel.CPU}) {
		t.Fatalf("plan for another item: %v", got)
	}
	// Copied video never touches the GPU.
	if got := tr.plan(gpu, 3, hwaccel.Video{Codec: "h264", BurnSub: -1}); !slices.Equal(got, []hwaccel.Mode{hwaccel.CPU}) {
		t.Fatalf("plan for h264: %v", got)
	}
}
