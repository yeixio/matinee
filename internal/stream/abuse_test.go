package stream

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yeixio/matinee/internal/store"
)

// A player being abused (seeking back and forth as fast as the remote
// allows, pausing and playing, giving up on requests halfway) must leave
// one FFmpeg running for the viewer, serve the newest position, and leave
// nothing behind: no FFmpeg, no goroutines, no files.
func TestPlayerAbuseLeavesNothingBehind(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "clip.mkv")
	gen := exec.Command(ffmpeg, "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "testsrc=size=320x180:rate=24:duration=120",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=120", "-c:v", "mpeg4", "-c:a", "mp3", src)
	if out, err := gen.CombinedOutput(); err != nil {
		t.Fatalf("make clip: %v\n%s", err, out)
	}
	goroutines := runtime.NumGoroutine()

	cache := filepath.Join(dir, "cache")
	tr := NewTranscoder(ffmpeg, cache, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	it := store.Item{ID: 1, Path: src, VideoCodec: "mpeg4", AudioCodec: "mp3"}
	const user = 7
	rnd := rand.New(rand.NewPCG(1, 2))

	// Bursts of overlapping seeks, as a held arrow key or a dragged bar
	// sends, some given up on by the player before they answer.
	rounds := 12
	if testing.Short() {
		rounds = 4
	}
	for round := range rounds {
		var wg sync.WaitGroup
		for range 4 {
			at := float64(rnd.IntN(110))
			giveUp := time.Duration(rnd.IntN(400)) * time.Millisecond
			cancelEarly := rnd.IntN(3) == 0
			wg.Go(func() {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				if cancelEarly {
					time.AfterFunc(giveUp, cancel)
				}
				_, err := tr.StartFor(ctx, user, it, at, Request{BurnSub: -1})
				if err != nil && !errors.Is(err, ErrSuperseded) && !errors.Is(err, context.Canceled) {
					t.Errorf("round %d: start at %v: %v", round, at, err)
				}
			})
		}
		wg.Wait()
		if n := tr.Active(); n > 1 {
			t.Fatalf("round %d: %d transcodes running for one viewer, want at most 1", round, n)
		}
		// Pause and play: the player fetches pieces of whatever it has,
		// sometimes giving up on one partway.
		for _, s := range tr.Sessions() {
			for seg := range 3 {
				ctx, cancel := context.WithTimeout(context.Background(), time.Duration(50+rnd.IntN(300))*time.Millisecond)
				req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/", nil)
				tr.ServeFile(httptest.NewRecorder(), req, user, s.ID, "seg0000"+strconv.Itoa(seg)+".ts")
				cancel()
			}
		}
	}

	// After all that, playing from a spot works and is the only transcode.
	id, err := tr.StartFor(context.Background(), user, it, 60, Request{BurnSub: -1})
	if err != nil {
		t.Fatalf("start after the abuse: %v", err)
	}
	for _, name := range []string{"index.m3u8", "seg00000.ts", "seg00001.ts"} {
		rec := httptest.NewRecorder()
		tr.ServeFile(rec, httptest.NewRequest(http.MethodGet, "/", nil), user, id, name)
		if rec.Code != http.StatusOK || rec.Body.Len() == 0 {
			t.Fatalf("%s after the abuse: %d (%d bytes)", name, rec.Code, rec.Body.Len())
		}
	}
	if s := tr.Sessions(); len(s) != 1 || s[0].ID != id {
		t.Fatalf("sessions after the abuse: %+v", s)
	}
	waitUntil(t, "replaced FFmpegs to exit", func() bool { _, procs := tr.Stats(); return procs == 1 })
	if superseded, _ := tr.Stats(); superseded == 0 {
		t.Error("no start was counted as replaced by a newer seek")
	}
	if _, cum, _ := tr.StartTimes.Snapshot(); cum[len(cum)-1] == 0 {
		t.Error("start times weren't recorded")
	}

	tr.Close()
	if _, procs := tr.Stats(); procs != 0 {
		t.Errorf("%d FFmpeg processes still running after Close", procs)
	}
	if kids := childProcesses(t, "ffmpeg"); len(kids) > 0 {
		t.Errorf("FFmpeg processes left behind: %v", kids)
	}
	entries, _ := os.ReadDir(cache)
	if len(entries) > 0 {
		t.Errorf("%d transcode folders left behind", len(entries))
	}
	// The idle reaper and every FFmpeg's waiter have gone.
	waitUntil(t, "goroutines to finish", func() bool { return runtime.NumGoroutine() <= goroutines })
}

// Two starts for the same viewer can overlap so that the older one's FFmpeg
// gets going after the newer one asked: the older one must stop itself.
func TestOlderStartLosesTheRace(t *testing.T) {
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("no sleep command")
	}
	tr := NewTranscoder(sleep, filepath.Join(t.TempDir(), "cache"), nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer tr.Close()
	k := sessionKey{user: 7, item: 1}
	older := tr.claim(k)
	newer := tr.claim(k)

	start := func(claim uint64) (*session, bool) {
		cmd := tr.command([]string{"30"})
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		tr.procs.Add(1)
		s := &session{id: newSessionID(), userID: 7, itemID: 1, dir: t.TempDir(), cmd: cmd, done: make(chan struct{}), lastSeen: time.Now()}
		go tr.wait(s, nil)
		return s, tr.register(s, claim)
	}
	n, ok := start(newer)
	if !ok {
		t.Fatal("the newer start was refused")
	}
	o, ok := start(older)
	if ok {
		t.Fatal("the older start registered after a newer one")
	}
	select {
	case <-o.done:
	case <-time.After(5 * time.Second):
		t.Fatal("the older start's process is still running")
	}
	if isDone(n) || tr.Active() != 1 {
		t.Fatalf("the newer start should be the one running (active %d)", tr.Active())
	}
	if superseded, _ := tr.Stats(); superseded != 1 {
		t.Errorf("superseded = %d, want 1", superseded)
	}
}

func waitUntil(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			buf := make([]byte, 1<<16)
			t.Fatalf("timed out waiting for %s\n%s", what, buf[:runtime.Stack(buf, true)])
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// childProcesses lists this process's children named name (Linux only).
func childProcesses(t *testing.T, name string) []string {
	t.Helper()
	if runtime.GOOS != "linux" {
		return nil
	}
	me := strconv.Itoa(os.Getpid())
	procs, _ := filepath.Glob("/proc/[0-9]*/stat")
	var out []string
	for _, p := range procs {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		// pid (comm) state ppid ...
		s := string(b)
		open, close := strings.IndexByte(s, '('), strings.LastIndexByte(s, ')')
		if open < 0 || close < open {
			continue
		}
		fields := strings.Fields(s[close+1:])
		if len(fields) > 1 && fields[1] == me && s[open+1:close] == name && fields[0] != "Z" {
			out = append(out, strings.TrimSpace(s[:open]))
		}
	}
	return out
}
