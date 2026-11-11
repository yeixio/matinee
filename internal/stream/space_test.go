package stream

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yeixio/matinee/internal/store"
)

func quietLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// A transcode that can't fit even a few minutes doesn't start FFmpeg: it
// says so in words a player shows, tells the admins once, and the player's
// "why" knows it until one starts again.
func TestNoRoomRefusesToStart(t *testing.T) {
	tr := NewTranscoder("/nonexistent/ffmpeg", filepath.Join(t.TempDir(), "cache"), nil, quietLog())
	defer tr.Close()
	tr.freeSpace = func() (int64, bool) { return 50 << 20, true }
	told := make(chan string, 2)
	tr.OnNoSpace = func(detail string) { told <- detail }
	ms := int64(2 * 60 * 60 * 1000)
	it := store.Item{ID: 3, Path: "/films/big.mkv", Size: 100 << 30, DurationMs: &ms, VideoCodec: "hevc", AudioCodec: "truehd"}

	_, err := tr.StartFor(context.Background(), 7, it, 0, Request{BurnSub: -1})
	if !errors.Is(err, ErrNoSpace) {
		t.Fatalf("start = %v, want ErrNoSpace", err)
	}
	if !tr.OutOfSpace(7, 3) || tr.OutOfSpace(8, 3) {
		t.Error("OutOfSpace should be true for the viewer who ran out, and only them")
	}
	select {
	case d := <-told:
		if !strings.Contains(d, "GB free") {
			t.Errorf("admins were told %q, want how much was free", d)
		}
	case <-time.After(time.Second):
		t.Fatal("admins weren't told")
	}
	// Again within the hour: no second alert.
	tr.StartFor(context.Background(), 7, it, 0, Request{BurnSub: -1})
	select {
	case d := <-told:
		t.Errorf("told again: %q", d)
	case <-time.After(100 * time.Millisecond):
	}
	if _, failed := tr.Counts(); failed != 0 {
		t.Errorf("counted %d transcode failures; a full folder has its own alert", failed)
	}
	if entries, _ := os.ReadDir(tr.Dir); len(entries) != 0 {
		t.Errorf("left %d folders behind", len(entries))
	}
	sp, ok := tr.Space()
	if !ok || sp.OutOfSpace == 0 {
		t.Errorf("Space() = %+v, %v; want when it ran out", sp, ok)
	}
}

func TestStreamRate(t *testing.T) {
	ms := int64(100 * 1000)
	film := store.Item{Size: 1_000_000_000, DurationMs: &ms, VideoCodec: "hevc"}
	if r := streamRate(film, true, nil); r < 10e6 || r > 10.1e6 {
		t.Errorf("repackaged: %v bytes/s, want about the file's 10 MB/s", r)
	}
	if r := streamRate(film, false, []Step{{VideoBPS: 4_000_000, AudioBPS: 128_000}}); r != 516_000 {
		t.Errorf("720p: %v bytes/s, want 516000", r)
	}
	if r := streamRate(store.Item{}, false, nil); r != 32_000 {
		t.Errorf("audio: %v bytes/s, want 32000", r)
	}
}

// A transcode whose whole video won't fit keeps only the minutes around
// its player: FFmpeg pauses far enough ahead and goes on as the player
// catches up, and pieces well behind are deleted; asking for one of those
// says so (410), so the player starts again there.
func TestSavingSpaceKeepsAWindow(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	defer func(a, r, b, m int, tick time.Duration) {
		aheadSeconds, resumeSeconds, behindSeconds, marginSeconds, spaceTick = a, r, b, m, tick
	}(aheadSeconds, resumeSeconds, behindSeconds, marginSeconds, spaceTick)
	aheadSeconds, resumeSeconds, behindSeconds, marginSeconds, spaceTick = 24, 12, 8, 0, 50*time.Millisecond

	dir := t.TempDir()
	src := filepath.Join(dir, "clip.mkv")
	gen := exec.Command(ffmpeg, "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "testsrc=size=320x180:rate=24:duration=120",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=120", "-c:v", "mpeg4", "-c:a", "mp3", src)
	if out, err := gen.CombinedOutput(); err != nil {
		t.Fatalf("make clip: %v\n%s", err, out)
	}
	tr := NewTranscoder(ffmpeg, filepath.Join(dir, "cache"), nil, quietLog())
	defer tr.Close()
	ms := int64(120_000)
	it := store.Item{ID: 1, Path: src, DurationMs: &ms, VideoCodec: "mpeg4", AudioCodec: "mp3"}
	// Room for the window (32 s at up to 10 Mbps, about 41 MB) but not
	// the whole two minutes (about 154 MB).
	var free atomic.Int64
	free.Store(80 << 20)
	tr.freeSpace = func() (int64, bool) { return free.Load(), true }

	id, err := tr.StartFor(context.Background(), 7, it, 0, Request{BurnSub: -1})
	if err != nil {
		t.Fatal(err)
	}
	info, _ := tr.SessionFor(7, 1)
	if !info.Saving {
		t.Fatal("transcode doesn't save space though the video won't fit")
	}
	get := func(n int) int {
		req := httptest.NewRequest("GET", "/", nil)
		ctx, cancel := context.WithTimeout(req.Context(), 10*time.Second)
		defer cancel()
		w := httptest.NewRecorder()
		tr.ServeFile(w, req.WithContext(ctx), 7, id, fmt.Sprintf("seg%05d.ts", n))
		return w.Code
	}
	segment := func(n int) string { return filepath.Join(tr.Dir, id, fmt.Sprintf("seg%05d.ts", n)) }
	waitUntil := func(what string, ok func() bool) {
		t.Helper()
		for deadline := time.Now().Add(15 * time.Second); !ok(); {
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting until %s", what)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}

	// The player has the first piece: FFmpeg stops 24 s (6 pieces) ahead.
	if code := get(0); code != http.StatusOK {
		t.Fatalf("piece 0: %d", code)
	}
	waitUntil("FFmpeg pauses ahead of the player", func() bool { s, _ := tr.SessionFor(7, 1); return s.Waiting })
	time.Sleep(300 * time.Millisecond)
	if fileExists(segment(10)) {
		t.Error("FFmpeg ran on past the window while paused")
	}
	// The player plays on: FFmpeg goes on, and pieces well behind go.
	for n := 1; n <= 12; n++ {
		if code := get(n); code != http.StatusOK {
			t.Fatalf("piece %d: %d", n, code)
		}
	}
	waitUntil("old pieces are cleared", func() bool { return !fileExists(segment(9)) && fileExists(segment(10)) })
	if code := get(3); code != http.StatusGone {
		t.Errorf("a cleared piece answers %d, want 410", code)
	}
	if s, _ := tr.SessionFor(7, 1); s.Speed == 0 {
		t.Error("speed is unknown once pieces are cleared")
	}
}

// Transcodes that started with room switch to saving space when the room
// runs short while they play (another stream, or something else filling
// the disk).
func TestShortOfRoomWhilePlaying(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	defer func(tick time.Duration) { spaceTick = tick }(spaceTick)
	spaceTick = 50 * time.Millisecond
	dir := t.TempDir()
	src := filepath.Join(dir, "clip.mkv")
	gen := exec.Command(ffmpeg, "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "testsrc=size=320x180:rate=24:duration=60",
		"-c:v", "mpeg4", src)
	if out, err := gen.CombinedOutput(); err != nil {
		t.Fatalf("make clip: %v\n%s", err, out)
	}
	tr := NewTranscoder(ffmpeg, filepath.Join(dir, "cache"), nil, quietLog())
	defer tr.Close()
	ms := int64(60_000)
	it := store.Item{ID: 1, Path: src, DurationMs: &ms, VideoCodec: "mpeg4"}
	var free atomic.Int64
	free.Store(100 << 30)
	tr.freeSpace = func() (int64, bool) { return free.Load(), true }
	if _, err := tr.StartFor(context.Background(), 7, it, 0, Request{BurnSub: -1}); err != nil {
		t.Fatal(err)
	}
	if s, _ := tr.SessionFor(7, 1); s.Saving {
		t.Fatal("saving space with 100 GB free")
	}
	free.Store(100 << 20)
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		if s, _ := tr.SessionFor(7, 1); s.Saving {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("still not saving space after the room ran short")
		}
	}
	if sp, _ := tr.Space(); !sp.Short {
		t.Error("Space() doesn't say transcodes are short of room")
	}
}

func TestDiskFullStopsWithPlainWords(t *testing.T) {
	dir := t.TempDir()
	tr := NewTranscoder("sleep", filepath.Join(dir, "cache"), nil, quietLog())
	defer tr.Close()
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Skip("no sleep command")
	}
	s := &session{id: newSessionID(), userID: 7, itemID: 1, dir: t.TempDir(), cmd: cmd, done: make(chan struct{}), lastSeen: time.Now(), rate: 1}
	go tr.wait(s, nil)
	tr.register(s, tr.claim(s.key()))
	w := &logWriter{log: quietLog(), session: s.id, full: func() { tr.diskFull(s) }}
	io.WriteString(w, "[hls @ 0x1] Failed to open file 'seg00042.ts': No space left on device\n")
	<-s.done
	if !tr.OutOfSpace(7, 1) {
		t.Error("not remembered as out of room")
	}
	rec := httptest.NewRecorder()
	tr.ServeFile(rec, httptest.NewRequest("GET", "/", nil), 7, s.id, "seg00042.ts")
	if rec.Code != http.StatusInsufficientStorage || !strings.Contains(rec.Body.String(), "out of room") {
		t.Errorf("piece after a full disk: %d %q, want 507 saying the server is out of room", rec.Code, rec.Body.String())
	}
}

func TestDirSizeSkips(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "images"), 0o755)
	os.MkdirAll(filepath.Join(dir, "transcode"), 0o755)
	os.WriteFile(filepath.Join(dir, "images", "a"), make([]byte, 1000), 0o644)
	os.WriteFile(filepath.Join(dir, "transcode", "b"), make([]byte, 5000), 0o644)
	if n := DirSize(dir, filepath.Join(dir, "transcode")); n != 1000 {
		t.Errorf("DirSize = %d, want 1000 (transcodes left out)", n)
	}
}
