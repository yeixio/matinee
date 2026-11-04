package scan

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/yeixio/matinee/internal/probe"
	"github.com/yeixio/matinee/internal/store/storetest"
)

// Two files of one movie are one title with two versions, and what's
// watched on either counts for both.
func TestVersionsOfAMovie(t *testing.T) {
	root := t.TempDir()
	hd := filepath.Join(root, "Heat (1995)", "Heat (1995) 1080p.mkv")
	cut := filepath.Join(root, "Heat (1995)", "Heat (1995) - Director's Cut.mkv")
	makeVideo(t, hd)
	makeVideo(t, filepath.Join(root, "Ronin (1998).mkv"))
	st := storetest.Open(t)
	ctx := context.Background()
	user, _ := st.CreateUser(ctx, "mike", "hash", true)
	lib, _ := st.CreateLibrary(ctx, "Movies", "movies", []string{root})
	s := &Scanner{Store: st, Prober: probe.Prober{Path: "ffprobe"}, Log: quietLog()}
	if _, err := s.Scan(ctx, lib); err != nil {
		t.Fatal(err)
	}
	title := fileID(t, st, lib, hd)
	watched(t, st, user.ID, title)

	makeVideo(t, cut)
	if _, err := s.Scan(ctx, lib); err != nil {
		t.Fatal(err)
	}
	if got := titles(t, st, lib); !slices.Equal(got, []string{"Heat", "Ronin"}) {
		t.Fatalf("titles: %v", got)
	}
	version := fileID(t, st, lib, cut)
	vs, err := st.Versions(ctx, user.ID, title)
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != 2 || vs[0].ID != title || vs[1].ID != version || vs[1].Edition != "Director's Cut" || vs[0].Edition != "" {
		t.Fatalf("versions: %+v", vs)
	}
	// The version shares the title's history, and playing it saves there.
	stillWatched(t, st, lib, user.ID, version, cut)
	if err := st.SetProgress(ctx, user.ID, version, 60_000, false); err != nil {
		t.Fatal(err)
	}
	it, _ := st.Item(ctx, user.ID, title)
	if it.PositionMs != 60_000 || it.Played {
		t.Fatalf("title progress: %d played %v", it.PositionMs, it.Played)
	}

	// Deleting the title's file leaves the title with the other version,
	// keeping its id and what people did with it.
	if err := st.SetProgress(ctx, user.ID, title, 0, true); err != nil {
		t.Fatal(err)
	}
	os.Remove(hd)
	stats, err := s.Scan(ctx, lib)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Replaced != 1 {
		t.Fatalf("stats: %+v", stats)
	}
	stillWatched(t, st, lib, user.ID, title, cut)
	if vs, _ := st.Versions(ctx, user.ID, title); len(vs) != 1 || vs[0].Edition != "Director's Cut" {
		t.Fatalf("versions after removing one: %+v", vs)
	}
}

// Removing a version leaves its title alone.
func TestRemovingAVersion(t *testing.T) {
	root := t.TempDir()
	hd := filepath.Join(root, "Heat (1995)", "Heat (1995) 1080p.mkv")
	uhd := filepath.Join(root, "Heat (1995)", "Heat (1995) 2160p.mkv")
	makeVideo(t, hd)
	st := storetest.Open(t)
	ctx := context.Background()
	user, _ := st.CreateUser(ctx, "mike", "hash", true)
	lib, _ := st.CreateLibrary(ctx, "Movies", "movies", []string{root})
	s := &Scanner{Store: st, Prober: probe.Prober{Path: "ffprobe"}, Log: quietLog()}
	if _, err := s.Scan(ctx, lib); err != nil {
		t.Fatal(err)
	}
	title := fileID(t, st, lib, hd)
	makeVideo(t, uhd)
	if _, err := s.Scan(ctx, lib); err != nil {
		t.Fatal(err)
	}
	watched(t, st, user.ID, fileID(t, st, lib, uhd))
	os.Remove(uhd)
	stats, err := s.Scan(ctx, lib)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Removed != 1 || stats.Replaced != 0 {
		t.Fatalf("stats: %+v", stats)
	}
	stillWatched(t, st, lib, user.ID, title, hd)
}

// Two copies of an episode are versions of it, and the season lists it once.
func TestVersionsOfAnEpisode(t *testing.T) {
	root := t.TempDir()
	season := filepath.Join(root, "The Office", "Season 2")
	makeVideo(t, filepath.Join(season, "The Office S02E03 720p.mkv"))
	makeVideo(t, filepath.Join(season, "The Office S02E03 1080p.mkv"))
	makeVideo(t, filepath.Join(season, "The Office S02E04.mkv"))
	st := storetest.Open(t)
	ctx := context.Background()
	lib, _ := st.CreateLibrary(ctx, "TV", "tv", []string{root})
	s := &Scanner{Store: st, Prober: probe.Prober{Path: "ffprobe"}, Log: quietLog()}
	if _, err := s.Scan(ctx, lib); err != nil {
		t.Fatal(err)
	}
	shows, _ := st.TopLevel(ctx, 0, lib.ID)
	seasons, _ := st.Children(ctx, 0, shows[0].ID)
	if seasons[0].ChildCount != 2 {
		t.Fatalf("season counts %d episodes", seasons[0].ChildCount)
	}
	eps, _ := st.Children(ctx, 0, seasons[0].ID)
	if len(eps) != 2 {
		t.Fatalf("episodes: %d", len(eps))
	}
	if vs, _ := st.Versions(ctx, 0, eps[0].ID); len(vs) != 2 {
		t.Fatalf("versions: %d", len(vs))
	}
}
