package thumbs

import (
	"context"
	"image"
	"image/jpeg"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/yeixio/matinee/internal/probe"
	"github.com/yeixio/matinee/internal/scan"
	"github.com/yeixio/matinee/internal/store"
	"github.com/yeixio/matinee/internal/store/storetest"
)

func TestInterval(t *testing.T) {
	for _, c := range []struct {
		ms   int64
		want time.Duration
	}{
		{0, 2 * time.Second},
		{60_000, 2 * time.Second},
		{20 * 60_000, 6 * time.Second},
		{2 * 3600_000, 10 * time.Second},
	} {
		if got := Interval(c.ms); got != c.want {
			t.Errorf("Interval(%d) = %v, want %v", c.ms, got, c.want)
		}
	}
}

func TestToneMap(t *testing.T) {
	for mi, want := range map[string]bool{
		``:                            false,
		`{"video":[{}]}`:              false,
		`{"video":[{"hdr":"hdr10"}]}`: true,
		`{"video":[{"hdr":"hlg"}]}`:   true,
		`{"video":[{"hdr":"dolby-vision","hdrBase":"hdr10"}]}`: true,
		`{"video":[{"hdr":"dolby-vision"}]}`:                   false, // profile 5
	} {
		if got := toneMap(mi) != ""; got != want {
			t.Errorf("toneMap(%s) = %v, want %v", mi, got, want)
		}
	}
}

// Both ways of reading a file give a full set of sheets, in the video's
// shape, for a movie the scanner found.
func TestMakeSheets(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	root := t.TempDir()
	file := filepath.Join(root, "Film (2020).mkv")
	// 50 seconds of 16:9 video with a keyframe a second: 25 pictures 2 s apart.
	if out, err := exec.Command(ffmpeg, "-v", "error", "-f", "lavfi", "-i", "testsrc2=s=640x360:r=10:d=50",
		"-c:v", "libx264", "-preset", "ultrafast", "-g", "10", file).CombinedOutput(); err != nil {
		t.Fatalf("make file: %v\n%s", err, out)
	}
	st := storetest.Open(t)
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	lib, _ := st.CreateLibrary(ctx, "Movies", "movies", []string{root})
	if _, err := (&scan.Scanner{Store: st, Prober: probe.Prober{Path: "ffprobe"}, Log: log}).Scan(ctx, lib); err != nil {
		t.Fatal(err)
	}
	Across, Down = 4, 3 // 12 a sheet, so 3 sheets

	for _, jump := range []bool{false, true} {
		seekAbove = 1 << 40
		if jump {
			seekAbove = 0
		}
		g := &Generator{Store: st, FFmpeg: ffmpeg, Dir: t.TempDir(), Log: log, LowPriority: probe.LowerPriority}
		n, err := g.Pass(ctx, func(store.ThumbnailFile) bool { return true })
		if err != nil || n != 1 {
			t.Fatalf("jump=%v: pass made %d, err %v", jump, n, err)
		}
		files, _ := st.ThumbnailFiles(ctx, 0, 10)
		id := files[0].ID
		th, ok, err := st.ItemThumbnails(ctx, id, Version)
		if err != nil || !ok {
			t.Fatalf("jump=%v: no thumbnails: %v", jump, err)
		}
		want := store.Thumbnails{IntervalMs: 2000, Width: 240, Height: 134, Columns: 4, Rows: 3, Count: 25, MadeAt: th.MadeAt}
		if th != want {
			t.Fatalf("jump=%v: got %+v, want %+v", jump, th, want)
		}
		for s := range th.Sheets() {
			f, err := os.Open(g.SheetPath(id, s))
			if err != nil {
				t.Fatal(err)
			}
			img, err := jpeg.Decode(f)
			f.Close()
			if err != nil {
				t.Fatal(err)
			}
			if img.Bounds() != image.Rect(0, 0, 4*240, 3*134) {
				t.Fatalf("sheet %d is %v", s, img.Bounds())
			}
		}
		// Made: the next pass leaves it be.
		if n, _ := g.Pass(ctx, func(store.ThumbnailFile) bool { return true }); n != 0 {
			t.Fatalf("jump=%v: second pass made %d", jump, n)
		}
		// A data folder that lost them makes them again.
		os.RemoveAll(filepath.Dir(g.SheetPath(id, 0)))
		if n, _ := g.Pass(ctx, func(store.ThumbnailFile) bool { return true }); n != 1 {
			t.Fatalf("jump=%v: pass after losing them made %d", jump, n)
		}
		if total, done, _ := st.ThumbnailCounts(ctx, Version); total != 1 || done != 1 {
			t.Fatalf("counts %d/%d", done, total)
		}
		st.ClearThumbnails(ctx, id)
	}
}

// A file FFmpeg can't read is remembered as unreadable rather than tried
// over and over.
func TestUnreadable(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	g := &Generator{FFmpeg: ffmpeg, Dir: t.TempDir(), Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	_, err = g.make(context.Background(), store.ThumbnailFile{ID: 1, Path: filepath.Join(t.TempDir(), "missing.mkv"), DurationMs: 10_000})
	if err == nil {
		t.Fatal("want an error")
	}
}
