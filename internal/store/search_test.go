package store_test

import (
	"context"
	"testing"

	"github.com/yeixio/matinee/internal/store"
	"github.com/yeixio/matinee/internal/store/storetest"
)

// Searching finds titles by name, then by who's in them.
func TestSearchByCast(t *testing.T) {
	db := storetest.Open(t)
	ctx := context.Background()
	user, err := db.CreateUser(ctx, "mike", "hash", true)
	if err != nil {
		t.Fatal(err)
	}
	movies, _ := db.CreateLibrary(ctx, "Movies", "movies", []string{"/m"})
	shows, _ := db.CreateLibrary(ctx, "Shows", "shows", []string{"/s"})
	weaver := store.Credit{PersonID: 10205, Name: "Sigourney Weaver", Role: "Ripley", Photo: "https://image.tmdb.org/t/p/w185/w.jpg"}
	movie := func(title string, cast ...store.Credit) int64 {
		id, err := db.UpsertFile(ctx, store.Item{LibraryID: movies.ID, Kind: "movie", Title: title, Path: "/m/" + title + ".mkv"})
		if err != nil {
			t.Fatal(err)
		}
		if err := db.ApplyDetails(ctx, id, store.DetailsUpdate{Cast: cast}); err != nil {
			t.Fatal(err)
		}
		return id
	}
	alien := movie("Alien", weaver)
	working := movie("Working Girl", weaver, store.Credit{PersonID: 3, Name: "Harrison Ford", Role: "Jack"})
	tale := movie("The Weaver's Tale")
	movie("Speed", store.Credit{PersonID: 6384, Name: "Keanu Reeves", Role: "Jack"})

	// A season's cast counts toward its show; a guest star's episode doesn't.
	show, _ := db.EnsureContainer(ctx, shows.ID, nil, "show", "Severance", nil, nil)
	one := 1
	season, _ := db.EnsureContainer(ctx, shows.ID, &show, "season", "Season 1", &one, nil)
	ep, _ := db.UpsertFile(ctx, store.Item{LibraryID: shows.ID, ParentID: &season, Kind: "episode", Title: "Good News About Hell", IndexNumber: &one, Path: "/s/1.mkv"})
	db.ApplyDetails(ctx, season, store.DetailsUpdate{Cast: []store.Credit{{PersonID: 1, Name: "Adam Scott", Role: "Mark"}}})
	db.ApplyEpisodeDetails(ctx, ep, store.DetailsUpdate{Guests: []store.Credit{{PersonID: 2, Name: "Sigourney Weaverling", Role: "Guest"}}})

	ids := func(q string) []int64 {
		t.Helper()
		items, err := db.Search(ctx, user.ID, q, 50)
		if err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		out := []int64{}
		for _, it := range items {
			out = append(out, it.ID)
		}
		return out
	}
	eq := func(q string, got, want []int64) {
		t.Helper()
		if len(got) != len(want) {
			t.Fatalf("%s: %v, want %v", q, got, want)
		}
		for i := range got {
			if got[i] != want[i] {
				t.Fatalf("%s: %v, want %v", q, got, want)
			}
		}
	}
	// The title named like the search first, then hers by name.
	eq("weaver", ids("weaver"), []int64{tale, alien, working})
	eq("sigourney weav", ids("sigourney weav"), []int64{alien, working})
	eq("Weaver, Sigourney", ids("Weaver, Sigourney"), []int64{alien, working})
	eq("adam sc", ids("adam sc"), []int64{show})
	eq("alien", ids("alien"), []int64{alien})
	eq("igourney", ids("igourney"), []int64{})
	eq("ford", ids("ford"), []int64{working})

	people, err := db.SearchPeople(ctx, user.ID, "sigourney", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(people) != 2 || people[0].ID != 10205 || people[0].Count != 2 || !people[0].HasPhoto || people[1].ID != 2 || people[1].Count != 1 {
		t.Fatalf("people: %+v", people)
	}
	if people, _ := db.SearchPeople(ctx, user.ID, "  ", 10); len(people) != 0 {
		t.Fatalf("no words: %+v", people)
	}
}
