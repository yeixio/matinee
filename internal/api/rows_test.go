package api

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/yeixio/matinee/internal/store"
	"github.com/yeixio/matinee/internal/store/storetest"
)

// Rows with more titles than they show say so, and /rows/{id} has them all.
func TestRowPages(t *testing.T) {
	db := storetest.Open(t)
	srv := &Server{Store: db, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	c := &client{t: t, base: ts.URL}
	var login struct{ Token string }
	c.do("POST", "/api/v1/setup", credentials{"admin", "correct horse"}, &login)
	c.token = login.Token

	defer func(f func() time.Time) { today = f }(today)
	today = func() time.Time { return time.Date(2026, 6, 1, 20, 0, 0, 0, time.Local) }

	// 35 science fiction movies, and 5 westerns.
	ctx := context.Background()
	lib, _ := db.CreateLibrary(ctx, "Movies", "movies", []string{"/m"})
	var first int64
	add := func(title string, year int, genre string, tmdb int64) {
		id, err := db.UpsertFile(ctx, store.Item{LibraryID: lib.ID, Kind: "movie", Title: title, Year: &year, Path: "/m/" + title + ".mkv"})
		if err != nil {
			t.Fatal(err)
		}
		db.ApplyMetadata(ctx, id, &store.Metadata{TMDBID: tmdb, Rating: 7, Genres: []string{genre}})
		// All recommended alike, so all related.
		db.ApplyDetails(ctx, id, store.DetailsUpdate{Terms: []store.Term{{Kind: "rec", Term: genre}}})
		if first == 0 {
			first = id
		}
	}
	for i := range 35 {
		add(fmt.Sprintf("Orbit %d", i), 2000+i%10, "Science Fiction", int64(100+i))
	}
	for i := range 5 {
		add(fmt.Sprintf("Dust %d", i), 1960, "Western", int64(200+i))
	}

	var home struct {
		RecentlyAdded []store.Item
		WatchList     []store.Item
		Rows          []HomeRow
		More          map[string]bool
	}
	if code := c.do("GET", "/api/v1/home", nil, &home); code != 200 {
		t.Fatalf("home: %d", code)
	}
	if len(home.RecentlyAdded) != homeRowItems || !home.More[rowRecent] || home.More[rowWatchList] {
		t.Fatalf("recently added: %d titles, more %v", len(home.RecentlyAdded), home.More)
	}
	rows := map[string]HomeRow{}
	for _, r := range home.Rows {
		rows[r.ID] = r
	}
	if r := rows["genre:Science Fiction"]; len(r.Items) != genreRowItems || !r.More {
		t.Fatalf("science fiction row: %d titles, more %v", len(r.Items), r.More)
	}
	if r := rows["genre:Western"]; len(r.Items) != 5 || r.More {
		t.Fatalf("western row: %d titles, more %v", len(r.Items), r.More)
	}

	page := func(id string) (HomeRow, int) {
		var r HomeRow
		code := c.do("GET", "/api/v1/rows/"+url.PathEscape(id), nil, &r)
		return r, code
	}
	if r, code := page(rowRecent); code != 200 || len(r.Items) != 40 || r.More {
		t.Fatalf("recently added page: %d %d titles", code, len(r.Items))
	}
	if r, _ := page("genre:Science Fiction"); len(r.Items) != 35 || r.Genre != "Science Fiction" || r.Title != "Sci-fi favorites" {
		t.Fatalf("science fiction page: %d titles %+v", len(r.Items), r.Title)
	}
	if r, _ := page(rowReleases); len(r.Items) != 0 {
		t.Fatalf("new releases page: %d titles", len(r.Items))
	}
	if r, _ := page("holiday:halloween"); r.Holiday != "halloween" || r.Title != "Spooky season" {
		t.Fatalf("halloween page: %+v", r)
	}
	if r, _ := page("related:" + strconv.FormatInt(first, 10)); r.RelatedTo != "Orbit 0" || len(r.Items) < 30 {
		t.Fatalf("more like Orbit 0: %q, %d titles", r.RelatedTo, len(r.Items))
	}
	var related relatedView
	c.do("GET", "/api/v1/items/"+strconv.FormatInt(first, 10)+"/related", nil, &related)
	if len(related.Items) != relatedItems || !related.More {
		t.Fatalf("related: %d titles, more %v", len(related.Items), related.More)
	}
	for _, id := range []string{"nope", "holiday:arbor-day", "genre:", "related:x", "collection:nope"} {
		if _, code := page(id); code != http.StatusNotFound {
			t.Fatalf("%s: %d", id, code)
		}
	}
}
