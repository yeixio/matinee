package scan

import (
	"context"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

// CD1 and CD2 of a movie are one movie that plays both, in order.
func TestScanMovieInParts(t *testing.T) {
	root := t.TempDir()
	makeVideos(t, root,
		"Heat (1995)/Heat (1995) CD2.mkv",
		"Heat (1995)/Heat (1995) CD1.mkv",
		"Ronin.1998.Part1.XviD.mkv",
		"Ronin.1998.Part2.XviD.mkv",
		"Deathly Hallows Part 1 (2010)/Deathly Hallows Part 1 (2010).mkv",
		"Deathly Hallows Part 2 (2011)/Deathly Hallows Part 2 (2011).mkv",
		"Lone (2001)/Lone (2001) CD1.mkv",
	)
	st, lib := scanLib(t, "movies", root)
	ctx := context.Background()
	top, _ := st.TopLevel(ctx, 0, lib.ID)
	titles := map[string]store.Item{}
	for _, it := range top {
		titles[it.Title] = it
	}
	if len(top) != 5 {
		t.Fatalf("movies: %v", titles)
	}
	for _, name := range []string{"Heat", "Ronin"} {
		it, err := st.Item(ctx, 0, titles[name].ID)
		if err != nil || len(it.Parts) != 2 || it.Container != "" || it.DurationMs == nil || *it.DurationMs < 1900 {
			t.Fatalf("%s: %+v %v", name, it, err)
		}
		if !strings.Contains(it.Parts[0].Path, "1") || !strings.Contains(it.Parts[1].Path, "2") {
			t.Errorf("%s parts out of order: %+v", name, it.Parts)
		}
	}
	if it := titles["Lone"]; len(it.Parts) != 0 {
		t.Errorf("a lone CD1 is a movie of its own: %+v", it)
	}
	// Scanning again changes nothing.
	if n, err := st.GroupParts(ctx, lib.ID, PartKey); err != nil || n != 0 {
		t.Errorf("regroup: %d %v", n, err)
	}
}

// A daily show's episodes go in their year's season, in date order, and
// a season folder's numbered files are its episodes.
func TestScanDailyAndNumberedEpisodes(t *testing.T) {
	root := t.TempDir()
	makeVideos(t, root,
		"The Nightly Desk/2024-03-15.mkv",
		"The Nightly Desk/The.Nightly.Desk.2024.01.02.Guest.mkv",
		"Crumb Street/Season 2/02 - Rise.mkv",
		"Crumb Street/Season 2/01 - Dough.mkv",
	)
	st, lib := scanLib(t, "tv", root)
	ctx := context.Background()
	top, _ := st.TopLevel(ctx, 0, lib.ID)
	if len(top) != 2 {
		t.Fatalf("shows: %+v", top)
	}
	want := map[string][]string{"The Nightly Desk": {"2024", "Guest", "Mar 15, 2024"}, "Crumb Street": {"Season 2", "Dough", "Rise"}}
	for _, show := range top {
		seasons, _ := st.Children(ctx, 0, show.ID)
		if len(seasons) != 1 {
			t.Fatalf("%s seasons: %+v", show.Title, seasons)
		}
		eps, _ := st.Children(ctx, 0, seasons[0].ID)
		w := want[show.Title]
		if seasons[0].Title != w[0] || len(eps) != 2 || eps[0].Title != w[1] || eps[1].Title != w[2] {
			var got []string
			for _, e := range eps {
				got = append(got, e.Title)
			}
			t.Errorf("%s: season %q, episodes %v; want %v", show.Title, seasons[0].Title, got, w)
		}
	}
}

// .nfo files next to the media describe it, pin its match and win over
// online details unless the library prefers those; hand edits win over both.
func TestScanReadsNFOFiles(t *testing.T) {
	root := t.TempDir()
	makeVideos(t, root, "Heat (1995)/Heat (1995).mkv")
	write := func(rel, body string) {
		if err := os.WriteFile(filepath.Join(root, rel), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("Heat (1995)/movie.nfo", `<movie><title>Heat (Director's Print)</title><plot>A heist.</plot><genre>Crime</genre><uniqueid type="tmdb">949</uniqueid></movie>`)
	st, lib := scanLib(t, "movies", root)
	ctx := context.Background()
	top, _ := st.TopLevel(ctx, 0, lib.ID)
	var movie store.Item
	for _, it := range top {
		if it.Kind == "movie" {
			movie, _ = st.Item(ctx, 0, it.ID)
		}
	}
	if movie.Title != "Heat (Director's Print)" || movie.Overview != "A heist." || movie.TMDBID == nil || *movie.TMDBID != 949 || movie.MetaAt != 0 {
		t.Fatalf("movie from its .nfo: %+v", movie)
	}
	// Online details don't replace the file's, but fill in the rest.
	st.ApplyMetadata(ctx, movie.ID, &store.Metadata{TMDBID: 949, Overview: "Online words.", Rating: 7.9})
	movie, _ = st.Item(ctx, 0, movie.ID)
	if movie.Overview != "A heist." || movie.Rating == nil || *movie.Rating != 7.9 {
		t.Fatalf("online over local: %+v", movie)
	}
	// Unless the library prefers online details.
	st.SetMetadataMode(ctx, lib.ID, store.PreferOnline)
	st.ApplyMetadata(ctx, movie.ID, &store.Metadata{TMDBID: 949, Overview: "Online words."})
	if movie, _ = st.Item(ctx, 0, movie.ID); movie.Overview != "Online words." {
		t.Fatalf("prefer online: %+v", movie)
	}
	// A hand edit wins over everything, rescans included.
	title := "Heat"
	overview := "My words."
	if err := st.EditDetails(ctx, movie.ID, store.Edit{Title: &title, Overview: &overview}); err != nil {
		t.Fatal(err)
	}
	st.ApplyMetadata(ctx, movie.ID, &store.Metadata{TMDBID: 949, Overview: "Online words."})
	s := &Scanner{Store: st, Prober: probe.Prober{Path: "ffprobe"}, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	st.ApplyLocal(ctx, movie.ID, nil) // as if the .nfo changed
	if _, err := s.Scan(ctx, lib); err != nil {
		t.Fatal(err)
	}
	if movie, _ = st.Item(ctx, 0, movie.ID); movie.Title != "Heat" || movie.Overview != "My words." {
		t.Fatalf("hand edit: %+v", movie)
	}
	if err := st.UnlockDetails(ctx, movie.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Scan(ctx, lib); err != nil {
		t.Fatal(err)
	}
	if movie, _ = st.Item(ctx, 0, movie.ID); movie.Title != "Heat (Director's Print)" || len(movie.LockedFields) != 0 {
		t.Fatalf("unlocked: %+v", movie)
	}
}

func TestScanReadsShowNFOFiles(t *testing.T) {
	root := t.TempDir()
	makeVideos(t, root, "Crumb Street/Season 1/Crumb Street - S01E01.mkv")
	os.WriteFile(filepath.Join(root, "Crumb Street/tvshow.nfo"), []byte(`<tvshow><title>Something Else</title><plot>A bakery.</plot><uniqueid type="tmdb">77</uniqueid></tvshow>`), 0o644)
	os.WriteFile(filepath.Join(root, "Crumb Street/Season 1/Crumb Street - S01E01.nfo"), []byte(`<episodedetails><title>Dough Rises</title></episodedetails>`), 0o644)
	st, lib := scanLib(t, "tv", root)
	ctx := context.Background()
	top, _ := st.TopLevel(ctx, 0, lib.ID)
	if len(top) != 1 || top[0].Title != "Crumb Street" || top[0].Overview != "A bakery." || top[0].TMDBID == nil || *top[0].TMDBID != 77 {
		t.Fatalf("show keeps its folder's name, takes the rest: %+v", top)
	}
	seasons, _ := st.Children(ctx, 0, top[0].ID)
	eps, _ := st.Children(ctx, 0, seasons[0].ID)
	if len(eps) != 1 || eps[0].Title != "Dough Rises" {
		t.Fatalf("episode: %+v", eps)
	}
}
