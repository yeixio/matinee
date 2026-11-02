package api

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/yeixio/matinee/internal/store"
	"github.com/yeixio/matinee/internal/store/storetest"
)

func TestPickGenres(t *testing.T) {
	counts := map[string]int{"Comedy": 9, "Drama": 12, "Horror": 2, "Romance": 5, "Science Fiction": 7, "Western": 4, "Mystery": 6, "War": 4, "Family": 8}
	monday := dayNumber(time.Date(2026, 10, 26, 9, 0, 0, 0, time.Local))
	picked := pickGenres(counts, monday, 6)
	if len(picked) != 6 {
		t.Fatalf("picked %v", picked)
	}
	// Too few horror titles for a row.
	if slices.Contains(picked, "Horror") {
		t.Fatalf("picked a genre with too few titles: %v", picked)
	}
	// The same all day, whenever it's asked.
	if again := pickGenres(counts, dayNumber(time.Date(2026, 10, 26, 23, 0, 0, 0, time.Local)), 6); !slices.Equal(again, picked) {
		t.Fatalf("changed within the day: %v then %v", picked, again)
	}
	// Different on other days.
	changed := false
	for d := uint64(1); d <= 7; d++ {
		if !slices.Equal(pickGenres(counts, monday+d, 6), picked) {
			changed = true
		}
	}
	if !changed {
		t.Fatal("the same rows every day for a week")
	}
	if got := pickGenres(map[string]int{"Drama": 3}, monday, 6); len(got) != 0 {
		t.Fatalf("a small library gets no genre rows: %v", got)
	}
}

// Home lists new releases and the day's genre rows below the rows that stay on top.
func TestHomeRows(t *testing.T) {
	db := storetest.Open(t)
	srv := &Server{Store: db, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	c := &client{t: t, base: ts.URL}
	var login struct{ Token string }
	c.do("POST", "/api/v1/setup", credentials{"admin", "correct horse"}, &login)
	c.token = login.Token

	defer func(f func() time.Time) { today = f }(today)
	today = func() time.Time { return time.Date(2026, 10, 26, 20, 0, 0, 0, time.Local) }

	ctx := context.Background()
	lib, _ := db.CreateLibrary(ctx, "Movies", "movies", []string{"/m"})
	add := func(title string, year int, rating float64, genres ...string) {
		id, err := db.UpsertFile(ctx, store.Item{LibraryID: lib.ID, Kind: "movie", Title: title, Year: &year, Path: "/m/" + title + ".mkv"})
		if err != nil {
			t.Fatal(err)
		}
		db.ApplyMetadata(ctx, id, &store.Metadata{TMDBID: int64(len(title)), Rating: rating, Genres: genres})
	}
	for i, title := range []string{"Orbit", "Star Ferry", "Glass Moon", "Red Planet", "Night Signal"} {
		add(title, 2010+i, float64(5+i), "Science Fiction", "Drama")
	}
	add("Paper Hearts", 2026, 7, "Romance")
	add("Last Dance", 2025, 6, "Romance", "Drama")
	// Shows stay out of the genre rows, and don't count toward one.
	tv, _ := db.CreateLibrary(ctx, "TV", "tv", []string{"/tv"})
	for i, title := range []string{"Love Island Nights", "Second Chances", "Deep Space Drift"} {
		show, _ := db.EnsureContainer(ctx, tv.ID, nil, "show", title, nil, nil)
		db.ApplyMetadata(ctx, show, &store.Metadata{TMDBID: int64(100 + i), Rating: 9, Genres: []string{"Romance", "Science Fiction"}})
	}

	var home struct {
		NewReleases []store.Item
		Rows        []HomeRow
	}
	if code := c.do("GET", "/api/v1/home", nil, &home); code != 200 {
		t.Fatalf("home: %d", code)
	}
	var releases []string
	for _, it := range home.NewReleases {
		releases = append(releases, it.Title)
	}
	if !slices.Equal(releases, []string{"Paper Hearts", "Last Dance"}) {
		t.Fatalf("new releases: %v", releases)
	}
	// Science Fiction and Drama have enough titles for a row; Romance doesn't.
	var titles []string
	for _, r := range home.Rows {
		titles = append(titles, r.Title)
		if r.Genre == "Science Fiction" && len(r.Items) != 5 {
			t.Fatalf("sci-fi row: %+v", r.Items)
		}
		for _, it := range r.Items {
			if it.Kind != "movie" {
				t.Fatalf("%s (a %s) in the %s row", it.Title, it.Kind, r.Genre)
			}
			if !slices.Contains(it.Genres, r.Genre) {
				t.Fatalf("%s in the %s row has genres %v", it.Title, r.Genre, it.Genres)
			}
		}
	}
	slices.Sort(titles)
	if !slices.Equal(titles, []string{"Dramas", "Sci-fi favorites"}) {
		t.Fatalf("rows: %v", titles)
	}
}
