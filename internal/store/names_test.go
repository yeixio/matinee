package store_test

import (
	"context"
	"testing"

	"github.com/yeixio/matinee/internal/store"
	"github.com/yeixio/matinee/internal/store/storetest"
)

// Titles scanned from file names with split accents or garbled letters are
// written the way they were meant, and the unmatched ones are looked up
// again; matched ones keep their match.
func TestRepairNames(t *testing.T) {
	st := storetest.Open(t)
	ctx := context.Background()
	lib, _ := st.CreateLibrary(ctx, "Movies", "movies", []string{"/m"})
	year := 2001
	add := func(title, path string) int64 {
		id, err := st.UpsertFile(ctx, store.Item{LibraryID: lib.ID, Kind: "movie", Title: title, Year: &year, Path: path})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	amelie := add("Amélie", "/m/a.mkv")
	aeon := add("Ã\u0086on Flux", "/m/b.mkv")
	matched := add("Café Society", "/m/c.mkv")
	plain := add("Heat", "/m/d.mkv")
	for _, id := range []int64{amelie, aeon, plain} {
		if err := st.ApplyMetadata(ctx, id, nil); err != nil { // looked up, no match
			t.Fatal(err)
		}
	}
	if err := st.ApplyMetadata(ctx, matched, &store.Metadata{TMDBID: 339397}); err != nil {
		t.Fatal(err)
	}
	if err := st.RepairNamesAgain(ctx); err != nil {
		t.Fatal(err)
	}
	want := map[int64]string{amelie: "Amélie", aeon: "Æon Flux", matched: "Café Society", plain: "Heat"}
	for id, title := range want {
		it, err := st.Item(ctx, 0, id)
		if err != nil {
			t.Fatal(err)
		}
		if it.Title != title {
			t.Errorf("item %d title = %q, want %q", id, it.Title, title)
		}
	}
	items, err := st.NeedsMetadata(ctx, lib.ID, []string{"movie"})
	if err != nil {
		t.Fatal(err)
	}
	again := map[int64]bool{}
	for _, it := range items {
		again[it.ID] = true
	}
	if !again[amelie] || !again[aeon] || again[matched] || again[plain] {
		t.Errorf("looked up again: %v, want only Amélie and Æon Flux", again)
	}
}
