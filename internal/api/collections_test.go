package api

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"testing"
	"time"

	"github.com/yeixio/matinee/internal/store"
	"github.com/yeixio/matinee/internal/store/storetest"
)

type collectionsList struct {
	Collections []savedView
	BoxSets     []store.BoxSet
}

func titlesOf(items []store.Item) []string {
	out := []string{}
	for _, it := range items {
		out = append(out, it.Title)
	}
	return out
}

func TestCollections(t *testing.T) {
	db := storetest.Open(t)
	srv := &Server{Store: db, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	admin := &client{t: t, base: ts.URL}
	var login struct{ Token string }
	admin.do("POST", "/api/v1/setup", credentials{"admin", "correct horse"}, &login)
	admin.token = login.Token
	if code := admin.do("POST", "/api/v1/users", map[string]any{"username": "ellen", "password": "battery staple"}, nil); code != http.StatusCreated {
		t.Fatalf("create user: %d", code)
	}
	ellen := signIn(t, admin, "ellen", "battery staple", "Phone")
	guest := &client{t: t, base: ts.URL} // at home, not signed in

	defer func(f func() time.Time) { today = f }(today)
	today = func() time.Time { return time.Date(2026, 10, 26, 20, 0, 0, 0, time.Local) }

	ctx := context.Background()
	lib, _ := db.CreateLibrary(ctx, "Movies", "movies", []string{"/m"})
	ids := map[string]int64{}
	add := func(title string, year int, minutes int64, genres []string, terms ...store.Term) {
		dur := minutes * 60_000
		id, err := db.UpsertFile(ctx, store.Item{LibraryID: lib.ID, Kind: "movie", Title: title, Year: &year, DurationMs: &dur, Path: "/m/" + title + ".mkv"})
		if err != nil {
			t.Fatal(err)
		}
		db.ApplyMetadata(ctx, id, &store.Metadata{TMDBID: id, Rating: float64(year % 10), Genres: genres})
		var d store.Details
		if len(terms) > 0 {
			d.Collection = &store.Collection{ID: 8091, Name: "Alien Collection"}
		}
		if err := db.ApplyDetails(ctx, id, store.DetailsUpdate{Details: d, Terms: terms}); err != nil {
			t.Fatal(err)
		}
		ids[title] = id
	}
	box := store.Term{Kind: "collection", Term: "8091"}
	add("Alien", 1979, 117, []string{"Horror", "Science Fiction"}, box)
	add("Aliens", 1986, 137, []string{"Action", "Science Fiction"}, box)
	add("Big", 1988, 104, []string{"Comedy"})
	add("Coco", 2017, 95, []string{"Animation", "Family"})
	add("Up", 2009, 96, []string{"Animation", "Family"})

	// A new server starts with the three starters, shared and only "tonight" pinned.
	var list collectionsList
	if code := guest.do("GET", "/api/v1/collections", nil, &list); code != 200 {
		t.Fatalf("list: %d", code)
	}
	var starters []string
	for _, c := range list.Collections {
		starters = append(starters, c.Starter)
		if !c.Shared || c.CanEdit || c.Kind != store.CollectionSmart || c.PinHome != (c.Starter == "tonight") {
			t.Errorf("starter %+v", c.SavedCollection)
		}
	}
	if !slices.Equal(starters, []string{"recent", "unwatched", "tonight"}) {
		t.Fatalf("starters: %v", starters)
	}
	tonight := list.Collections[2]
	if got := titlesOf(tonight.Items); len(got) != 2 || !slices.Contains(got, "Up") || !slices.Contains(got, "Coco") {
		t.Fatalf("short enough for tonight: %v", got)
	}

	// Box sets need two titles in the library.
	if len(list.BoxSets) != 1 || list.BoxSets[0].Name != "Alien Collection" || list.BoxSets[0].Count != 2 {
		t.Fatalf("box sets: %+v", list.BoxSets)
	}
	var bs struct {
		Name  string
		Items []store.Item
	}
	guest.do("GET", "/api/v1/box-sets/8091", nil, &bs)
	if got := titlesOf(bs.Items); !slices.Equal(got, []string{"Alien", "Aliens"}) {
		t.Fatalf("box set: %v", got)
	}
	// A kids profile only sees box sets of titles it may see.
	level := 1
	kid, err := db.CreateProfile(ctx, 1, store.ProfileChanges{KidsLevel: &level})
	if err != nil {
		t.Fatal(err)
	}
	if sets, err := db.BoxSets(ctx, kid.ID); err != nil || len(sets) != 0 {
		t.Fatalf("kids profile box sets: %v %+v", err, sets)
	}
	if code := guest.do("GET", "/api/v1/box-sets/1", nil, nil); code != http.StatusNotFound {
		t.Errorf("missing box set: %d", code)
	}

	// A smart collection is a saved filter, evaluated for whoever looks.
	filter, err := store.ParseItemFilter(url.Values{"genre": {"Animation"}, "watched": {"notStarted"}})
	if err != nil {
		t.Fatal(err)
	}
	var kids savedView
	body := map[string]any{"name": "Cartoons", "kind": "smart", "filter": filter, "sort": "year", "order": "asc", "pinLibrary": lib.ID}
	if code := ellen.do("POST", "/api/v1/collections", body, &kids); code != http.StatusCreated {
		t.Fatalf("create smart: %d", code)
	}
	if got := titlesOf(kids.Items); !slices.Equal(got, []string{"Up", "Coco"}) || kids.Count != 2 || !kids.CanEdit || kids.Shared {
		t.Fatalf("smart collection: %+v", kids)
	}
	ellen.do("PUT", "/api/v1/items/"+itoa(ids["Up"])+"/progress", map[string]any{"positionMs": 0, "played": true}, nil)
	ellen.do("GET", "/api/v1/collections/"+kids.ID, nil, &kids)
	if got := titlesOf(kids.Items); !slices.Equal(got, []string{"Coco"}) {
		t.Fatalf("after watching Up: %v", got)
	}
	// It's Ellen's: others can't see it, and the library row is hers.
	if code := admin.do("GET", "/api/v1/collections/"+kids.ID, nil, nil); code != http.StatusNotFound {
		t.Errorf("admin sees Ellen's collection: %d", code)
	}
	var rows collectionsList
	ellen.do("GET", "/api/v1/collections?library="+itoa(lib.ID), nil, &rows)
	if len(rows.Collections) != 1 || rows.Collections[0].ID != kids.ID || rows.BoxSets == nil || len(rows.BoxSets) != 0 {
		t.Fatalf("library rows: %+v", rows)
	}

	// Manual collections are kept by hand.
	var faves savedView
	if code := ellen.do("POST", "/api/v1/collections", map[string]any{"name": "Faves", "kind": "manual", "pinHome": true}, &faves); code != http.StatusCreated {
		t.Fatalf("create manual: %d", code)
	}
	for _, title := range []string{"Big", "Alien", "Big"} {
		if code := ellen.do("PUT", "/api/v1/collections/"+faves.ID+"/items/"+itoa(ids[title]), nil, nil); code != http.StatusNoContent {
			t.Fatalf("add %s: %d", title, code)
		}
	}
	if code := ellen.do("PUT", "/api/v1/collections/"+kids.ID+"/items/"+itoa(ids["Big"]), nil, nil); code != http.StatusBadRequest {
		t.Errorf("adding to a smart collection: %d", code)
	}
	if code := ellen.do("PUT", "/api/v1/collections/"+faves.ID+"/items/999999", nil, nil); code != http.StatusNotFound {
		t.Errorf("adding a missing item: %d", code)
	}
	ellen.do("GET", "/api/v1/collections/"+faves.ID, nil, &faves)
	if got := titlesOf(faves.Items); !slices.Equal(got, []string{"Big", "Alien"}) {
		t.Fatalf("manual order: %v", got)
	}
	ellen.do("PUT", "/api/v1/collections/"+faves.ID, map[string]any{"name": "Favorites", "sort": "title", "pinHome": true}, &faves)
	if got := titlesOf(faves.Items); faves.Name != "Favorites" || !slices.Equal(got, []string{"Alien", "Big"}) {
		t.Fatalf("renamed and sorted: %s %v", faves.Name, got)
	}
	if code := ellen.do("PUT", "/api/v1/collections/"+faves.ID, map[string]any{"name": "Favorites", "sort": "lastPlayed"}, nil); code != http.StatusBadRequest {
		t.Errorf("unknown manual sort: %d", code)
	}
	ellen.do("GET", "/api/v1/collections?item="+itoa(ids["Alien"]), nil, &list)
	for _, c := range list.Collections {
		if c.Kind == store.CollectionManual && (c.Contains == nil || !*c.Contains) || c.Kind == store.CollectionSmart && c.Contains != nil {
			t.Errorf("contains on %s: %v", c.Name, c.Contains)
		}
	}
	ellen.do("DELETE", "/api/v1/collections/"+faves.ID+"/items/"+itoa(ids["Alien"]), nil, nil)

	// Pinned collections lead Home's rows, for their owner.
	var home struct{ Rows []HomeRow }
	ellen.do("GET", "/api/v1/home", nil, &home)
	var pinned []string
	for _, r := range home.Rows {
		if r.CollectionID != "" {
			pinned = append(pinned, r.Title+":"+titlesOf(r.Items)[0])
		}
	}
	if len(pinned) != 2 || pinned[0] != "Short enough for tonight:Coco" || pinned[1] != "Favorites:Big" {
		t.Fatalf("pinned rows: %v", pinned)
	}
	admin.do("GET", "/api/v1/home", nil, &home)
	if home.Rows[0].CollectionID != tonight.ID || len(home.Rows) > 1 && home.Rows[1].CollectionID != "" {
		t.Fatalf("admin's home rows: %+v", home.Rows)
	}

	// Shared collections are an admin's to change; the Home guest only looks.
	if code := ellen.do("PUT", "/api/v1/collections/"+tonight.ID, map[string]any{"name": "Mine now", "filter": filter}, nil); code != http.StatusForbidden {
		t.Errorf("Ellen changing a shared collection: %d", code)
	}
	if code := ellen.do("POST", "/api/v1/collections", map[string]any{"name": "Ours", "kind": "manual", "shared": true}, nil); code != http.StatusForbidden {
		t.Errorf("Ellen sharing: %d", code)
	}
	if code := guest.do("POST", "/api/v1/collections", map[string]any{"name": "Ours", "kind": "manual"}, nil); code != http.StatusUnauthorized {
		t.Errorf("guest making one: %d", code)
	}
	var shared savedView
	if code := admin.do("POST", "/api/v1/collections", map[string]any{"name": "Movie night", "kind": "manual", "shared": true}, &shared); code != http.StatusCreated || !shared.Shared {
		t.Fatalf("admin sharing: %d %+v", code, shared)
	}
	if code := guest.do("GET", "/api/v1/collections/"+shared.ID, nil, nil); code != 200 {
		t.Errorf("guest seeing a shared collection: %d", code)
	}
	for _, bad := range []map[string]any{
		{"name": "", "kind": "manual"},
		{"name": "No rules", "kind": "smart"},
		{"name": "Odd", "kind": "folder"},
		{"name": "Sideways", "kind": "manual", "order": "sideways"},
		{"name": "Nowhere", "kind": "manual", "pinLibrary": 999},
	} {
		if code := ellen.do("POST", "/api/v1/collections", bad, nil); code != http.StatusBadRequest {
			t.Errorf("%v: %d", bad, code)
		}
	}
	if code := admin.do("DELETE", "/api/v1/collections/"+tonight.ID, nil, nil); code != http.StatusNoContent {
		t.Fatalf("delete: %d", code)
	}
	if code := admin.do("GET", "/api/v1/collections/"+tonight.ID, nil, nil); code != http.StatusNotFound {
		t.Errorf("deleted collection: %d", code)
	}
}

func TestShuffleIsDaily(t *testing.T) {
	db := storetest.Open(t)
	srv := &Server{Store: db}
	ctx := context.Background()
	lib, _ := db.CreateLibrary(ctx, "Movies", "movies", []string{"/m"})
	c, _ := db.CreateCollection(ctx, store.SavedCollection{Name: "Mix", Kind: store.CollectionManual, Sort: "shuffle"})
	for i := range 30 {
		id, _ := db.UpsertFile(ctx, store.Item{LibraryID: lib.ID, Kind: "movie", Title: "Movie " + itoa(int64(i)), Path: "/m/" + itoa(int64(i)) + ".mkv"})
		db.AddToCollection(ctx, c.ID, id)
	}
	defer func(f func() time.Time) { today = f }(today)
	order := func(day int) []string {
		today = func() time.Time { return time.Date(2026, 10, day, 9, 0, 0, 0, time.Local) }
		items, n, err := srv.resolveCollection(ctx, 0, c, 10)
		if err != nil || n != 30 || len(items) != 10 {
			t.Fatalf("resolve: %v %d %d", err, n, len(items))
		}
		return titlesOf(items)
	}
	if a, b := order(26), order(26); !slices.Equal(a, b) {
		t.Fatalf("same day, different order: %v %v", a, b)
	}
	if a, b := order(26), order(27); slices.Equal(a, b) {
		t.Fatalf("next day, same order: %v", a)
	}
}
