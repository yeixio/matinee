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

// makeVideos writes a tiny playable video at each path under root.
func makeVideos(t *testing.T, root string, rels ...string) {
	t.Helper()
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe not installed")
	}
	var first string
	for _, rel := range rels {
		p := filepath.Join(root, rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if first != "" {
			b, _ := os.ReadFile(first)
			os.WriteFile(p, b, 0o644)
			continue
		}
		if out, err := exec.Command(ffmpeg, "-loglevel", "error", "-f", "lavfi", "-i", "testsrc=d=1:s=64x36", "-f", "matroska", p).CombinedOutput(); err != nil {
			t.Fatalf("make video: %v\n%s", err, out)
		}
		first = p
	}
}

func scanLib(t *testing.T, kind, root string) (*store.Store, store.Library) {
	t.Helper()
	st := storetest.Open(t)
	ctx := context.Background()
	lib, _ := st.CreateLibrary(ctx, "Lib", kind, []string{root})
	s := &Scanner{Store: st, Prober: probe.Prober{Path: "ffprobe"}, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	if stats, err := s.Scan(ctx, lib); err != nil || stats.Failed > 0 {
		t.Fatalf("scan: %+v %v", stats, err)
	}
	return st, lib
}

// Shows that share a name but not a year or id stay apart, and a folder
// without a year joins the show it names.
func TestScanKeepsSameNamedShowsApart(t *testing.T) {
	root := t.TempDir()
	makeVideos(t, root,
		"Doctor Who (1963)/Season 1/Doctor Who - S01E01.mkv",
		"Doctor Who (2005)/Season 1/Doctor Who - S01E01.mkv",
		"Doctor Who (2005)/Season 1/Doctor Who - S01E02.mkv",
		"Shogun {tmdb-126308}/Season 1/Shogun - S01E01.mkv",
		"Shogun {tmdb-14366}/Season 1/Shogun - S01E01.mkv",
		"Severance/Season 1/Severance - S01E01.mkv",
	)
	st, lib := scanLib(t, "tv", root)
	top, _ := st.TopLevel(context.Background(), 0, lib.ID)
	if len(top) != 5 {
		t.Fatalf("shows: %+v", top)
	}
	years := map[int]int{}
	for _, s := range top {
		if s.Title == "Doctor Who" && s.Year != nil {
			years[*s.Year] = s.ChildCount
		}
	}
	if len(years) != 2 {
		t.Fatalf("Doctor Who shows by year: %v (%+v)", years, top)
	}
}

// A season with S01E01E02 and S01E03 shows two episodes, in order.
func TestScanMultiEpisodeFile(t *testing.T) {
	root := t.TempDir()
	makeVideos(t, root, "Show/Season 1/Show - S01E01E02.mkv", "Show/Season 1/Show - S01E03 - Third.mkv")
	st, lib := scanLib(t, "tv", root)
	ctx := context.Background()
	top, _ := st.TopLevel(ctx, 0, lib.ID)
	if len(top) != 1 {
		t.Fatalf("shows: %+v", top)
	}
	seasons, _ := st.Children(ctx, 0, top[0].ID)
	if len(seasons) != 1 {
		t.Fatalf("seasons: %+v", seasons)
	}
	eps, _ := st.Children(ctx, 0, seasons[0].ID)
	if len(eps) != 2 || eps[0].Title != "Episodes 1–2" || eps[1].Title != "Third" {
		t.Fatalf("episodes: %+v", eps)
	}
}
