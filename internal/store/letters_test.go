package store_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/yeixio/matinee/internal/store"
	"github.com/yeixio/matinee/internal/store/storetest"
)

func TestLetterOf(t *testing.T) {
	for in, want := range map[string]string{
		"matrix": "M", "élan": "E", "2001: a space odyssey": "#", "(500) days of summer": "#",
		"øvelse": "O", "straße": "S", "": "#", "  zoo": "Z", "амели": "А", "千と千尋": "千",
	} {
		if got := store.LetterOf(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}

// Letters point at where each starts in the same list the filter gives.
func TestFilterLetters(t *testing.T) {
	db := storetest.Open(t)
	ctx := context.Background()
	user, _ := db.CreateUser(ctx, "mike", "hash", true)
	lib, _ := db.CreateLibrary(ctx, "Movies", "movies", []string{"/m"})
	for _, title := range []string{"The Matrix", "Alien", "Aliens", "2001", "Brazil", "Mad Max", "Zodiac"} {
		if _, err := db.UpsertFile(ctx, store.Item{LibraryID: lib.ID, Kind: "movie", Title: title, Path: "/m/" + title + ".mkv"}); err != nil {
			t.Fatal(err)
		}
	}
	f := store.ItemFilter{Library: []int64{lib.ID}}
	got, err := db.FilterLetters(ctx, user.ID, f)
	if err != nil {
		t.Fatal(err)
	}
	want := []store.Letter{letter("#", 0, 1), letter("A", 1, 2), letter("B", 3, 1), letter("M", 4, 2), letter("Z", 6, 1)}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v", got)
	}
	items, _ := db.FilterItems(ctx, user.ID, f)
	for _, l := range got {
		if s := store.LetterOf(store.SortTitle(items[l.Index].Title)); s != l.Letter {
			t.Errorf("%s points at %q", l.Letter, items[l.Index].Title)
		}
	}

	f.Order = "desc"
	got, _ = db.FilterLetters(ctx, user.ID, f)
	if len(got) != 5 || got[0] != letter("Z", 0, 1) || got[4] != letter("#", 6, 1) {
		t.Fatalf("desc: %+v", got)
	}
	got, _ = db.FilterLetters(ctx, user.ID, store.ItemFilter{Library: []int64{lib.ID}, Q: "alien"})
	if !reflect.DeepEqual(got, []store.Letter{letter("A", 0, 2)}) {
		t.Fatalf("filtered: %+v", got)
	}
	// Other sorts aren't in letter order.
	got, err = db.FilterLetters(ctx, user.ID, store.ItemFilter{Library: []int64{lib.ID}, Sort: "year"})
	if err != nil || len(got) != 0 {
		t.Fatalf("year: %+v %v", got, err)
	}
}

func letter(l string, index, count int) store.Letter {
	return store.Letter{Letter: l, Index: index, Count: count}
}
