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

func TestActiveHolidays(t *testing.T) {
	ids := func(y int, m time.Month, d int) []string {
		var out []string
		for _, h := range activeHolidays(time.Date(y, m, d, 21, 0, 0, 0, time.Local)) {
			out = append(out, h.ID)
		}
		return out
	}
	for _, c := range []struct {
		y    int
		m    time.Month
		d    int
		want []string
	}{
		{2026, time.September, 30, nil},
		{2026, time.October, 1, []string{"halloween"}},
		// Thanksgiving is November 26th this year, 30 days off.
		{2026, time.October, 27, []string{"halloween", "thanksgiving"}},
		{2026, time.October, 31, []string{"halloween", "thanksgiving"}},
		{2026, time.November, 1, []string{"thanksgiving"}},
		{2026, time.November, 26, []string{"thanksgiving", "christmas"}},
		{2026, time.November, 27, []string{"christmas"}},
		{2026, time.December, 24, []string{"christmas", "newyear"}},
		{2026, time.December, 26, []string{"newyear"}},
		{2027, time.January, 1, nil},
		{2027, time.January, 20, []string{"valentines"}},
		// Easter 2027 is March 28th.
		{2027, time.March, 28, []string{"easter"}},
		{2027, time.March, 29, nil},
	} {
		if got := ids(c.y, c.m, c.d); !slices.Equal(got, c.want) {
			t.Errorf("%d-%02d-%02d: %v, want %v", c.y, c.m, c.d, got, c.want)
		}
	}
	for y, want := range map[int]string{2024: "2024-03-31", 2025: "2025-04-20", 2026: "2026-04-05", 2027: "2027-03-28"} {
		if got := easter(y).Format(time.DateOnly); got != want {
			t.Errorf("Easter %d: %s, want %s", y, got, want)
		}
	}
	for y, want := range map[int]string{2025: "2025-11-27", 2026: "2026-11-26", 2027: "2027-11-25"} {
		if got := thanksgiving(y).Format(time.DateOnly); got != want {
			t.Errorf("Thanksgiving %d: %s, want %s", y, got, want)
		}
	}
}

type fakeKeywords map[string][]int64

func (f fakeKeywords) Keywords(_ context.Context, name string) ([]int64, error) { return f[name], nil }

// In late October, Home leads with a Halloween row: titles about it first,
// then horror to fill it out.
func TestHolidayRows(t *testing.T) {
	db := storetest.Open(t)
	srv := &Server{Store: db, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Keywords: fakeKeywords{"halloween": {3335}, "witch": {616}}}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	c := &client{t: t, base: ts.URL}
	var login struct{ Token string }
	c.do("POST", "/api/v1/setup", credentials{"admin", "correct horse"}, &login)
	c.token = login.Token

	defer func(f func() time.Time) { today = f }(today)
	today = func() time.Time { return time.Date(2026, 10, 27, 20, 0, 0, 0, time.Local) }

	ctx := context.Background()
	lib, _ := db.CreateLibrary(ctx, "Movies", "movies", []string{"/m"})
	n := 0
	add := func(title, overview string, genres ...string) int64 {
		n++
		year := 2000 + n
		id, err := db.UpsertFile(ctx, store.Item{LibraryID: lib.ID, Kind: "movie", Title: title, Year: &year, Path: "/m/" + title + ".mkv"})
		if err != nil {
			t.Fatal(err)
		}
		db.ApplyMetadata(ctx, id, &store.Metadata{TMDBID: int64(n), Overview: overview, Rating: 6, Genres: genres})
		return id
	}
	add("Halloween Night", "A town's longest night.", "Horror")
	add("Candy Run", "Three kids trick-or-treat on the wrong street.", "Family")
	add("Switchboard", "An operator hears too much.", "Thriller") // has "witch" in it
	tagged := add("Broom Closet", "A comedy about a cleaning company.", "Comedy")
	if err := db.ApplyDetails(ctx, tagged, store.DetailsUpdate{Terms: []store.Term{{Kind: "keyword", Term: "616"}}}); err != nil {
		t.Fatal(err)
	}
	add("Cellar", "Something lives downstairs.", "Horror")
	add("Mistletoe Lane", "Snowed in at Christmas.", "Romance")
	// A show with a Halloween episode.
	tv, _ := db.CreateLibrary(ctx, "Shows", "shows", []string{"/s"})
	show, _ := db.EnsureContainer(ctx, tv.ID, nil, "show", "Office Hours", nil, nil)
	one := 1
	season, _ := db.EnsureContainer(ctx, tv.ID, &show, "season", "Season 1", &one, nil)
	db.UpsertFile(ctx, store.Item{LibraryID: tv.ID, ParentID: &season, Kind: "episode", Title: "The Halloween Party", IndexNumber: &one, Path: "/s/e1.mkv"})

	home := func() []HomeRow {
		var out struct{ Rows []HomeRow }
		if code := c.do("GET", "/api/v1/home", nil, &out); code != 200 {
			t.Fatalf("home: %d", code)
		}
		return out.Rows
	}
	titles := func(r HomeRow) []string {
		var out []string
		for _, it := range r.Items {
			out = append(out, it.Title)
		}
		return out
	}
	// Keywords are looked up in the background the first time.
	home()
	for i := 0; len(srv.keywordIDs([]string{"halloween", "witch"})) < 2; i++ {
		if i == 100 {
			t.Fatal("keywords never looked up")
		}
		time.Sleep(10 * time.Millisecond)
	}
	rows := home()
	if len(rows) == 0 || rows[0].ID != "holiday:halloween" || rows[0].Title != "Spooky season" || rows[0].Holiday != "halloween" {
		t.Fatalf("rows: %+v", rows)
	}
	got := titles(rows[0])
	about, fill := got[:min(4, len(got))], got[min(4, len(got)):]
	slices.Sort(about)
	if !slices.Equal(about, []string{"Broom Closet", "Candy Run", "Halloween Night", "Office Hours"}) || !slices.Equal(fill, []string{"Cellar"}) {
		t.Fatalf("Halloween row: %v", got)
	}
	// Thanksgiving is coming too, but nothing's about it.
	for _, r := range rows[1:] {
		if r.Holiday != "" {
			t.Fatalf("another holiday row: %+v", r)
		}
	}

	// Not in summer.
	today = func() time.Time { return time.Date(2026, 7, 1, 20, 0, 0, 0, time.Local) }
	for _, r := range home() {
		if r.Holiday != "" {
			t.Fatalf("holiday row in July: %v", r.ID)
		}
	}

	// An admin can turn them off.
	today = func() time.Time { return time.Date(2026, 10, 27, 20, 0, 0, 0, time.Local) }
	var set serverSettings
	c.do("PUT", "/api/settings", map[string]any{"holidayRows": false}, &set)
	if set.HolidayRows {
		t.Fatal("still on")
	}
	for _, r := range home() {
		if r.Holiday != "" {
			t.Fatalf("holiday row while off: %v", r.ID)
		}
	}
	c.do("GET", "/api/settings", nil, &set)
	if set.HolidayRows {
		t.Fatal("off didn't stick")
	}
}
