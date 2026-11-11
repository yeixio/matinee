package thumbs

import (
	"context"
	"errors"
	"image"
	"image/jpeg"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/yeixio/matinee/internal/probe"
	"github.com/yeixio/matinee/internal/scan"
	"github.com/yeixio/matinee/internal/slowfs"
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
	// 10 bits, like most HEVC, whose frames FFmpeg would hand over in 16.
	if out, err := exec.Command(ffmpeg, "-v", "error", "-f", "lavfi", "-i", "testsrc2=s=640x360:r=10:d=50",
		"-c:v", "libx264", "-preset", "ultrafast", "-g", "10", "-pix_fmt", "yuv420p10le", file).CombinedOutput(); err != nil {
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

	// A video that keeps timing out, as a damaged one does, is stored as
	// unreadable after slowfs.GiveUpAfter tries and listed as such.
	oldFrame, oldDamaged := frameTimeout, damaged
	t.Cleanup(func() { frameTimeout, damaged = oldFrame, oldDamaged })
	// The re-test of a healthy file clears it: a slow moment isn't damage.
	damaged = func(context.Context, string, string, int64, func(int)) bool { return false }
	if g := (&Generator{Store: st, FFmpeg: ffmpeg, Dir: t.TempDir(), Log: log}); true {
		seekAbove, frameTimeout = 0, time.Nanosecond
		files, _ := st.ThumbnailFiles(ctx, 0, 10)
		if g.Make(ctx, files[0]) {
			t.Fatal("set a healthy video aside after one slow read")
		}
		if vids, n, _ := st.UnreadableVideos(ctx, Version, 10); n != 0 {
			t.Fatalf("unreadable: %+v", vids)
		}
	}
	damaged = func(context.Context, string, string, int64, func(int)) bool { return true }
	seekAbove, frameTimeout = 0, time.Nanosecond
	g := &Generator{Store: st, FFmpeg: ffmpeg, Dir: t.TempDir(), Log: log, LowPriority: probe.LowerPriority}
	var reported []int64
	g.OnUnreadable(func(_ context.Context, id int64) { reported = append(reported, id) })
	files, _ := st.ThumbnailFiles(ctx, 0, 10)
	for range slowfs.GiveUpAfter - 1 {
		if g.Make(ctx, files[0]) {
			t.Fatal("stored something for a video that timed out")
		}
	}
	if !g.Make(ctx, files[0]) {
		t.Fatal("didn't give up on a video that keeps timing out")
	}
	if len(reported) != 1 || reported[0] != files[0].ID {
		t.Fatalf("reported %v", reported)
	}
	if vids, n, err := st.UnreadableVideos(ctx, Version, 10); err != nil || n != 1 || len(vids) != 1 || vids[0].ID != files[0].ID {
		t.Fatalf("unreadable: %+v %d %v", vids, n, err)
	}

	// A video whose share stops answering each time it's read is set aside
	// after slowfs.StallsBeforeSetAside tries, even by a new Generator, as
	// after a restart. It isn't checked for damage: that would read it again.
	st.ClearThumbnails(ctx, files[0].ID)
	reported = nil
	damaged = func(context.Context, string, string, int64, func(int)) bool {
		t.Fatal("checked a file on a stalled share")
		return false
	}
	oldWait := slowfs.Wait
	slowfs.Wait = 20 * time.Millisecond
	release := make(chan struct{})
	t.Cleanup(func() { close(release); slowfs.Wait = oldWait })
	slowfs.Do(files[0].Path, func() (int, error) { <-release; return 0, nil })
	for i := 1; i < slowfs.StallsBeforeSetAside; i++ {
		g := &Generator{Store: st, FFmpeg: ffmpeg, Dir: t.TempDir(), Log: log}
		if g.Make(ctx, files[0]) {
			t.Fatalf("set aside after %d stalls", i)
		}
	}
	g = &Generator{Store: st, FFmpeg: ffmpeg, Dir: t.TempDir(), Log: log}
	g.OnUnreadable(func(_ context.Context, id int64) { reported = append(reported, id) })
	if !g.Make(ctx, files[0]) {
		t.Fatal("didn't set aside a video its share keeps stopping on")
	}
	if len(reported) != 1 {
		t.Fatalf("reported %v", reported)
	}
	if _, n, _ := st.UnreadableVideos(ctx, Version, 10); n != 1 {
		t.Fatalf("unreadable: %d", n)
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

// A video that can't be read is stored as unreadable only once it fails
// twice in a row: one failure may be its share reconnecting.
func TestUnreadableAfterTwoFailures(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	root := t.TempDir()
	file := filepath.Join(root, "Film (2020).mkv")
	if out, err := exec.Command(ffmpeg, "-v", "error", "-f", "lavfi", "-i", "testsrc2=s=320x180:r=10:d=5",
		"-c:v", "libx264", "-preset", "ultrafast", file).CombinedOutput(); err != nil {
		t.Fatalf("make file: %v\n%s", err, out)
	}
	st := storetest.Open(t)
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	lib, _ := st.CreateLibrary(ctx, "Movies", "movies", []string{root})
	if _, err := (&scan.Scanner{Store: st, Prober: probe.Prober{Path: "ffprobe"}, Log: log}).Scan(ctx, lib); err != nil {
		t.Fatal(err)
	}
	// Garbled since it was scanned.
	data, _ := os.ReadFile(file)
	for i := range data {
		data[i] = 0
	}
	os.WriteFile(file, data, 0o644)
	files, _ := st.ThumbnailFiles(ctx, 0, 10)
	g := &Generator{Store: st, FFmpeg: ffmpeg, Dir: t.TempDir(), Log: log}
	if g.Make(ctx, files[0]) {
		t.Fatal("set aside after one failure")
	}
	if _, n, _ := st.UnreadableVideos(ctx, Version, 10); n != 0 {
		t.Fatalf("unreadable after one failure: %d", n)
	}
	if !g.Make(ctx, files[0]) {
		t.Fatal("didn't set aside after two failures")
	}
	if _, n, _ := st.UnreadableVideos(ctx, Version, 10); n != 1 {
		t.Fatalf("unreadable: %d", n)
	}
}

// fakeFFmpeg writes a stand-in for FFmpeg that hands over a 2x2 picture for
// any jump, and hangs on jumps at or past hangFrom seconds, or exactly at
// hangAt, as FFmpeg does on a file whose end is damaged.
func fakeFFmpeg(t *testing.T, hangFrom, hangAt int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ffmpeg")
	script := `#!/bin/sh
while [ "$#" -gt 0 ]; do
	if [ "$1" = "-ss" ]; then at=${2%%.*}; fi
	shift
done
if [ "$at" -ge ` + strconv.Itoa(hangFrom) + ` ] || [ "$at" -eq ` + strconv.Itoa(hangAt) + ` ]; then exec sleep 5; fi
printf 'P6\n2 2\n255\n'
printf '\377\0\0\377\0\0\377\0\0\377\0\0'
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// A big file whose end can't be read keeps its pictures up to there, and a
// jump that's slow once is skipped; neither is taken for a damaged file.
func TestJumpingDamagedEnd(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no shell")
	}
	old := frameTimeout
	t.Cleanup(func() { frameTimeout = old })
	frameTimeout = 200 * time.Millisecond
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	jump := func(ffmpeg string, count int) (int, error) {
		g := &Generator{FFmpeg: ffmpeg, Log: log}
		n := 0
		err := g.jumping(context.Background(), "film.mkv", time.Second, count, "", func(*frame) error { n++; return nil })
		return n, err
	}

	// The last 3 of 10 points hang: the first of them is skipped, and the
	// second stops there with 8 pictures and no error (finish fills in the
	// rest).
	if n, err := jump(fakeFFmpeg(t, 7, -1), 10); err != nil || n != 8 {
		t.Fatalf("damaged end: %d pictures, err %v", n, err)
	}
	// One slow point early on is skipped: all 10, the one before standing in.
	if n, err := jump(fakeFFmpeg(t, 1000, 2), 10); err != nil || n != 10 {
		t.Fatalf("one slow jump: %d pictures, err %v", n, err)
	}
	// Hanging from before the middle is a damaged file.
	if _, err := jump(fakeFFmpeg(t, 3, -1), 10); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("hangs before the middle: err %v", err)
	}
	// So is one that hangs at its very start.
	if _, err := jump(fakeFFmpeg(t, 0, -1), 10); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("hangs at the start: err %v", err)
	}
}
