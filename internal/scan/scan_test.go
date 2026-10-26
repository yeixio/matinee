package scan

import (
	"context"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/yeixio/matinee/internal/probe"
	"github.com/yeixio/matinee/internal/store"
	"github.com/yeixio/matinee/internal/store/storetest"
)

// Folders show up only when they hold something that plays: an empty folder,
// a zero-byte file or one ffprobe can't read leaves no artist behind.
func TestScanLeavesOutUnplayableFiles(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe not installed")
	}
	root := t.TempDir()
	mk := func(rel string) string {
		p := filepath.Join(root, rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		return p
	}
	song := mk("Real Artist/Album/01 Song.mp3")
	if out, err := exec.Command(ffmpeg, "-loglevel", "error", "-f", "lavfi", "-i", "sine=d=1", song).CombinedOutput(); err != nil {
		t.Fatalf("make song: %v\n%s", err, out)
	}
	os.MkdirAll(mk("Empty Artist/x"), 0o755)
	os.WriteFile(mk("Broken Artist/Album/01 junk.mp3"), []byte("not audio"), 0o644)
	os.WriteFile(mk("Zero Artist/Album/01 zero.mp3"), nil, 0o644)

	st := storetest.Open(t)
	ctx := context.Background()
	lib, _ := st.CreateLibrary(ctx, "Music", "music", []string{root})
	s := &Scanner{Store: st, Prober: probe.Prober{Path: ffprobe}, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	stats, err := s.Scan(ctx, lib)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Added != 1 || stats.Unplayable != 2 {
		t.Fatalf("stats: %+v", stats)
	}
	top, _ := st.TopLevel(ctx, 0, lib.ID)
	if len(top) != 1 || top[0].Title != "Real Artist" {
		t.Fatalf("artists: %+v", top)
	}
}

// An unmounted share looks like an empty or missing folder; its items stay.
func TestScanKeepsItemsWhenFolderIsGone(t *testing.T) {
	root := filepath.Join(t.TempDir(), "share")
	st := storetest.Open(t)
	ctx := context.Background()
	lib, _ := st.CreateLibrary(ctx, "Movies", "movies", []string{root})
	if _, err := st.UpsertFile(ctx, store.Item{LibraryID: lib.ID, Kind: "movie", Title: "Heat", Path: filepath.Join(root, "Heat.mkv"), VideoCodec: "h264"}); err != nil {
		t.Fatal(err)
	}
	s := &Scanner{Store: st, Prober: probe.Prober{Path: "ffprobe"}, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	stats, err := s.Scan(ctx, lib)
	if err != nil {
		t.Fatal(err)
	}
	if top, _ := st.TopLevel(ctx, 0, lib.ID); stats.Removed != 0 || len(top) != 1 {
		t.Fatalf("removed %d, left %+v", stats.Removed, top)
	}
}

// Trailers belong to the title next to them, not in the library as titles of their own.
func TestScanLeavesOutTrailers(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe not installed")
	}
	root := t.TempDir()
	for _, rel := range []string{"Heat (1995)/Heat (1995).mkv", "Heat (1995)/Heat (1995)-trailer.mkv", "Heat (1995)/Trailers/Teaser.mkv"} {
		p := filepath.Join(root, rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if out, err := exec.Command(ffmpeg, "-loglevel", "error", "-f", "lavfi", "-i", "testsrc=d=1:s=64x36", p).CombinedOutput(); err != nil {
			t.Fatalf("make video: %v\n%s", err, out)
		}
	}
	st := storetest.Open(t)
	ctx := context.Background()
	lib, _ := st.CreateLibrary(ctx, "Movies", "movies", []string{root})
	s := &Scanner{Store: st, Prober: probe.Prober{Path: ffprobe}, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	if _, err := s.Scan(ctx, lib); err != nil {
		t.Fatal(err)
	}
	top, _ := st.TopLevel(ctx, 0, lib.ID)
	if len(top) != 1 || top[0].Title != "Heat" {
		t.Fatalf("movies: %+v", top)
	}
}
