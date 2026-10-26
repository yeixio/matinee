package scan

import (
	"context"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/yeixio/matinee/internal/probe"
	"github.com/yeixio/matinee/internal/store"
	"github.com/yeixio/matinee/internal/store/storetest"
)

func quietLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func makeVideo(t *testing.T, path string) {
	t.Helper()
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe not installed")
	}
	os.MkdirAll(filepath.Dir(path), 0o755)
	if out, err := exec.Command(ffmpeg, "-loglevel", "error", "-y", "-f", "lavfi", "-i", "testsrc=d=1:s=64x36", path).CombinedOutput(); err != nil {
		t.Fatalf("make video: %v\n%s", err, out)
	}
}

func titles(t *testing.T, st *store.Store, lib store.Library) []string {
	t.Helper()
	top, err := st.TopLevel(context.Background(), 0, lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, it := range top {
		out = append(out, it.Title)
	}
	slices.Sort(out)
	return out
}

// Scanning a few paths adds what's new there and removes what's gone from
// there, and leaves the rest of the library alone, even files that would be
// removed by a full scan.
func TestScanPathsOnlyTouchesThosePaths(t *testing.T) {
	root := t.TempDir()
	makeVideo(t, filepath.Join(root, "Heat (1995)", "Heat (1995).mkv"))
	st := storetest.Open(t)
	ctx := context.Background()
	lib, _ := st.CreateLibrary(ctx, "Movies", "movies", []string{root})
	s := &Scanner{Store: st, Prober: probe.Prober{Path: "ffprobe"}, Log: quietLog()}
	if _, err := s.Scan(ctx, lib); err != nil {
		t.Fatal(err)
	}
	// Listed but not on disk; only a full scan may drop it.
	st.UpsertFile(ctx, store.Item{LibraryID: lib.ID, Kind: "movie", Title: "Ghost", Path: filepath.Join(root, "Ghost.mkv"), VideoCodec: "h264"})

	makeVideo(t, filepath.Join(root, "Ronin (1998)", "Ronin (1998).mkv"))
	stats, err := s.ScanPaths(ctx, lib, []string{filepath.Join(root, "Ronin (1998)")})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Added != 1 || stats.Removed != 0 {
		t.Fatalf("stats: %+v", stats)
	}
	if got := titles(t, st, lib); !slices.Equal(got, []string{"Ghost", "Heat", "Ronin"}) {
		t.Fatalf("titles: %v", got)
	}

	os.RemoveAll(filepath.Join(root, "Heat (1995)"))
	stats, err = s.ScanPaths(ctx, lib, []string{filepath.Join(root, "Heat (1995)")})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Removed != 1 {
		t.Fatalf("stats: %+v", stats)
	}
	if got := titles(t, st, lib); !slices.Equal(got, []string{"Ghost", "Ronin"}) {
		t.Fatalf("titles: %v", got)
	}
}

// An unmounted share must not look like a deleted folder.
func TestScanPathsKeepsItemsWhenShareIsGone(t *testing.T) {
	root := filepath.Join(t.TempDir(), "share")
	st := storetest.Open(t)
	ctx := context.Background()
	lib, _ := st.CreateLibrary(ctx, "Movies", "movies", []string{root})
	st.UpsertFile(ctx, store.Item{LibraryID: lib.ID, Kind: "movie", Title: "Heat", Path: filepath.Join(root, "Heat", "Heat.mkv"), VideoCodec: "h264"})
	s := &Scanner{Store: st, Prober: probe.Prober{Path: "ffprobe"}, Log: quietLog()}
	stats, err := s.ScanPaths(ctx, lib, []string{filepath.Join(root, "Heat")})
	if err != nil {
		t.Fatal(err)
	}
	if got := titles(t, st, lib); stats.Removed != 0 || len(got) != 1 {
		t.Fatalf("removed %d, left %v", stats.Removed, got)
	}
}

func TestTargetsIn(t *testing.T) {
	roots := []string{"/media/movies", "/media/movies-4k"}
	got := targetsIn(roots, []string{
		"/media/movies/Heat/Heat.mkv",
		"/media/movies/Heat",
		"/media/movies-4k/Ronin/Ronin.mkv",
		"/media/movies/.recycle/Old.mkv",
		"/media/movies/Heat (1995)/Trailers/Teaser.mkv",
		"/elsewhere/x.mkv",
	}, true)
	want := []target{{"/media/movies", "/media/movies/Heat"}, {"/media/movies-4k", "/media/movies-4k/Ronin/Ronin.mkv"}}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// receive collects what arrives on changes until want is all there.
func receive(t *testing.T, changes <-chan string, want ...string) {
	t.Helper()
	got := map[string]bool{}
	deadline := time.After(5 * time.Second)
	for {
		missing := false
		for _, w := range want {
			if !got[w] {
				missing = true
			}
		}
		if !missing {
			return
		}
		select {
		case p := <-changes:
			got[p] = true
		case <-deadline:
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

// Polling is what network shares get: it must notice new files, new
// folders, files in new folders and removals.
func TestPollNoticesChanges(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "Show", "Season 1"), 0o755)
	os.WriteFile(filepath.Join(root, "Show", "Season 1", "e1.mkv"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(root, "Old.mkv"), []byte("x"), 0o644)
	w := NewWatcher(nil, quietLog())
	w.PollInterval = 50 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	changes := make(chan string, 64)
	go w.poll(ctx, []string{root}, changes)
	time.Sleep(100 * time.Millisecond) // first look, which reports nothing

	e2 := filepath.Join(root, "Show", "Season 1", "e2.mkv")
	os.WriteFile(e2, []byte("x"), 0o644)
	receive(t, changes, e2)

	s2 := filepath.Join(root, "Show", "Season 2")
	os.MkdirAll(s2, 0o755)
	os.WriteFile(filepath.Join(s2, "e1.mkv"), []byte("x"), 0o644)
	receive(t, changes, s2)
	// The new folder is polled from now on too.
	e3 := filepath.Join(s2, "e3.mkv")
	os.WriteFile(e3, []byte("x"), 0o644)
	receive(t, changes, e3)

	os.Remove(filepath.Join(root, "Old.mkv"))
	os.RemoveAll(filepath.Join(root, "Show", "Season 1"))
	receive(t, changes, filepath.Join(root, "Old.mkv"), filepath.Join(root, "Show", "Season 1"))

	// Hidden files, like a copy in progress under a temporary name, aren't reported.
	os.WriteFile(filepath.Join(root, ".Heat.mkv.partial"), []byte("x"), 0o644)
	select {
	case p := <-changes:
		t.Fatalf("reported %s", p)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestEventsNoticeChanges(t *testing.T) {
	root := t.TempDir()
	fw, err := watchTree(root)
	if err != nil {
		t.Skipf("no change notifications here: %v", err)
	}
	w := NewWatcher(nil, quietLog())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	changes := make(chan string, 64)
	go w.events(ctx, fw, root, changes)

	dir := filepath.Join(root, "Heat (1995)")
	os.Mkdir(dir, 0o755)
	receive(t, changes, dir)
	// Files in a new folder are seen once it's watched.
	file := filepath.Join(dir, "Heat (1995).mkv")
	os.WriteFile(file, []byte("x"), 0o644)
	receive(t, changes, file)
}

// The whole way through: a file copied into a watched library shows up
// within seconds, both with change notifications and with polling.
func TestWatcherPicksUpNewFiles(t *testing.T) {
	for _, mode := range []WatchMode{WatchAuto, WatchPoll} {
		t.Run(string(mode), func(t *testing.T) {
			root := t.TempDir()
			makeVideo(t, filepath.Join(root, "Heat (1995)", "Heat (1995).mkv"))
			st := storetest.Open(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			lib, _ := st.CreateLibrary(ctx, "Movies", "movies", []string{root})
			s := &Scanner{Store: st, Prober: probe.Prober{Path: "ffprobe"}, Log: quietLog()}
			if _, err := s.Scan(ctx, lib); err != nil {
				t.Fatal(err)
			}
			w := NewWatcher(s, quietLog())
			w.PollInterval, w.Quiet = 100*time.Millisecond, 300*time.Millisecond
			if err := w.SetMode(ctx, mode); err != nil {
				t.Fatal(err)
			}
			done := make(chan struct{})
			go func() { w.Run(ctx); close(done) }()
			defer func() { cancel(); <-done }()
			// Wait for the watch to be set up before changing anything.
			for deadline := time.Now().Add(5 * time.Second); len(w.Status(lib.ID)) == 0; time.Sleep(20 * time.Millisecond) {
				if time.Now().After(deadline) {
					t.Fatal("watch never set up")
				}
			}
			if mode == WatchPoll {
				if got := w.Status(lib.ID)[0].Method; got != MethodPolling {
					t.Fatalf("method %s", got)
				}
				time.Sleep(200 * time.Millisecond) // the first look
			}

			// Made elsewhere and moved in, the way downloaders hand files over.
			staging := t.TempDir()
			makeVideo(t, filepath.Join(staging, "Ronin (1998).mkv"))
			os.Mkdir(filepath.Join(root, "Ronin (1998)"), 0o755)
			os.Rename(filepath.Join(staging, "Ronin (1998).mkv"), filepath.Join(root, "Ronin (1998)", "Ronin (1998).mkv"))
			for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(50 * time.Millisecond) {
				if got := titles(t, st, lib); slices.Equal(got, []string{"Heat", "Ronin"}) {
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("titles: %v", titles(t, st, lib))
				}
			}

			os.RemoveAll(filepath.Join(root, "Heat (1995)"))
			for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(50 * time.Millisecond) {
				if got := titles(t, st, lib); slices.Equal(got, []string{"Ronin"}) {
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("titles: %v", titles(t, st, lib))
				}
			}
		})
	}
}
