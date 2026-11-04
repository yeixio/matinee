package scan

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/yeixio/matinee/internal/probe"
	"github.com/yeixio/matinee/internal/store"
	"github.com/yeixio/matinee/internal/store/storetest"
)

// fileID returns the id of the item that has this file, or 0.
func fileID(t *testing.T, st *store.Store, lib store.Library, path string) int64 {
	t.Helper()
	files, err := st.LibraryFiles(context.Background(), lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if f.Path == path {
			return f.ID
		}
	}
	return 0
}

// watched marks an item as played, rated and on the watch list for user.
func watched(t *testing.T, st *store.Store, userID, itemID int64) {
	t.Helper()
	ctx := context.Background()
	if err := st.SetProgress(ctx, userID, itemID, 0, true); err != nil {
		t.Fatal(err)
	}
	if err := st.SetRating(ctx, userID, itemID, 8); err != nil {
		t.Fatal(err)
	}
	if err := st.AddToWatchList(ctx, userID, itemID); err != nil {
		t.Fatal(err)
	}
}

// stillWatched checks that the item with id kept what watched gave it and has the file path.
func stillWatched(t *testing.T, st *store.Store, lib store.Library, userID, id int64, path string) {
	t.Helper()
	if got := fileID(t, st, lib, path); got != id {
		t.Fatalf("%s belongs to item %d, want %d", path, got, id)
	}
	it, err := st.Item(context.Background(), userID, id)
	if err != nil {
		t.Fatal(err)
	}
	if !it.Played || it.MyRating == nil || *it.MyRating != 8 || !it.OnWatchList {
		t.Fatalf("lost watch history: played %v rating %v watch list %v", it.Played, it.MyRating, it.OnWatchList)
	}
}

// A better copy of a movie under another name takes the old one's place:
// same item, so watched marks, ratings and watch lists stay.
func TestUpgradeKeepsWatchHistory(t *testing.T) {
	root := t.TempDir()
	old := filepath.Join(root, "Heat (1995)", "Heat (1995) 1080p.mkv")
	makeVideo(t, old)
	makeVideo(t, filepath.Join(root, "Ronin (1998).mkv"))
	st := storetest.Open(t)
	ctx := context.Background()
	user, _ := st.CreateUser(ctx, "mike", "hash", true)
	lib, _ := st.CreateLibrary(ctx, "Movies", "movies", []string{root})
	s := &Scanner{Store: st, Prober: probe.Prober{Path: "ffprobe"}, Log: quietLog()}
	if _, err := s.Scan(ctx, lib); err != nil {
		t.Fatal(err)
	}
	id := fileID(t, st, lib, old)
	watched(t, st, user.ID, id)

	os.Remove(old)
	nu := filepath.Join(root, "Heat (1995)", "Heat (1995) 2160p.mkv")
	makeVideo(t, nu)
	stats, err := s.Scan(ctx, lib)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Replaced != 1 || stats.Removed != 0 {
		t.Fatalf("stats: %+v", stats)
	}
	stillWatched(t, st, lib, user.ID, id, nu)
	if got := titles(t, st, lib); !slices.Equal(got, []string{"Heat", "Ronin"}) {
		t.Fatalf("titles: %v", got)
	}
}

// A renamed episode keeps its item too, when the watcher scans the old
// and new names together.
func TestRenamedEpisodeKeepsWatchHistory(t *testing.T) {
	root := t.TempDir()
	season := filepath.Join(root, "The Office", "Season 2")
	old := filepath.Join(season, "The Office S02E03.mkv")
	makeVideo(t, old)
	makeVideo(t, filepath.Join(season, "The Office S02E04.mkv"))
	st := storetest.Open(t)
	ctx := context.Background()
	user, _ := st.CreateUser(ctx, "mike", "hash", true)
	lib, _ := st.CreateLibrary(ctx, "TV", "tv", []string{root})
	s := &Scanner{Store: st, Prober: probe.Prober{Path: "ffprobe"}, Log: quietLog(), MissingGrace: time.Minute}
	if _, err := s.Scan(ctx, lib); err != nil {
		t.Fatal(err)
	}
	id := fileID(t, st, lib, old)
	watched(t, st, user.ID, id)

	nu := filepath.Join(season, "The Office - S02E03 - Office Olympics.mkv")
	if err := os.Rename(old, nu); err != nil {
		t.Fatal(err)
	}
	stats, err := s.ScanPaths(ctx, lib, []string{old, nu})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Replaced != 1 || stats.Removed != 0 {
		t.Fatalf("stats: %+v", stats)
	}
	stillWatched(t, st, lib, user.ID, id, nu)
}

// Download tools often delete the old copy first and move the new one in a
// while later. The watcher sees those apart, so a missing file waits a
// little for what replaces it, and is removed once the wait is over if
// nothing did.
func TestWatcherWaitsForReplacement(t *testing.T) {
	root := t.TempDir()
	heat := filepath.Join(root, "Heat (1995)", "Heat.1995.720p.mkv")
	ronin := filepath.Join(root, "Ronin (1998)", "Ronin (1998).mkv")
	makeVideo(t, heat)
	makeVideo(t, ronin)
	st := storetest.Open(t)
	ctx := context.Background()
	user, _ := st.CreateUser(ctx, "mike", "hash", true)
	lib, _ := st.CreateLibrary(ctx, "Movies", "movies", []string{root})
	grace := 500 * time.Millisecond
	s := &Scanner{Store: st, Prober: probe.Prober{Path: "ffprobe"}, Log: quietLog(), MissingGrace: grace}
	if _, err := s.Scan(ctx, lib); err != nil {
		t.Fatal(err)
	}
	id := fileID(t, st, lib, heat)
	watched(t, st, user.ID, id)

	os.Remove(heat)
	os.Remove(ronin)
	stats, err := s.ScanPaths(ctx, lib, []string{heat, ronin})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Removed != 0 {
		t.Fatalf("removed before the wait was over: %+v", stats)
	}
	nu := filepath.Join(root, "Heat (1995)", "Heat (1995) Bluray-1080p.mkv")
	makeVideo(t, nu)
	stats, err = s.ScanPaths(ctx, lib, []string{nu})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Replaced != 1 {
		t.Fatalf("stats: %+v", stats)
	}
	stillWatched(t, st, lib, user.ID, id, nu)

	// Nothing replaced Ronin, so it goes once the wait is over.
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(50 * time.Millisecond) {
		if got := titles(t, st, lib); slices.Equal(got, []string{"Heat"}) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("titles: %v", titles(t, st, lib))
		}
	}
}

// A different movie isn't taken for a replacement.
func TestDifferentMovieIsNotAReplacement(t *testing.T) {
	root := t.TempDir()
	heat := filepath.Join(root, "Heat (1995).mkv")
	makeVideo(t, heat)
	st := storetest.Open(t)
	ctx := context.Background()
	lib, _ := st.CreateLibrary(ctx, "Movies", "movies", []string{root})
	s := &Scanner{Store: st, Prober: probe.Prober{Path: "ffprobe"}, Log: quietLog()}
	if _, err := s.Scan(ctx, lib); err != nil {
		t.Fatal(err)
	}
	os.Remove(heat)
	makeVideo(t, filepath.Join(root, "Heat (1986).mkv"))
	stats, err := s.Scan(ctx, lib)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Replaced != 0 || stats.Removed != 1 || stats.Added != 1 {
		t.Fatalf("stats: %+v", stats)
	}
}
