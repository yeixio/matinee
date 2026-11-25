package api

import (
	"context"
	"net/http"
	"slices"
	"strconv"
	"testing"

	"github.com/yeixio/matinee/internal/arr/arrtest"
	"github.com/yeixio/matinee/internal/store"
)

// With Box Office connected and no Overseerr, the Discover tab comes from
// Box Office, and asking for a title is an ordinary request.
func TestDiscoverFromBoxOffice(t *testing.T) {
	admin, _, db := newTestServerStore(t)
	ctx := context.Background()
	var login struct {
		Token string
		User  store.User
	}
	admin.do("POST", "/api/v1/setup", credentials{"admin", "correct horse"}, &login)
	admin.token = login.Token
	admin.do("POST", "/api/v1/users", map[string]any{"username": "ellen", "password": "password123"}, nil)
	ellen := &client{t: t, base: admin.base}
	admin.do("POST", "/api/v1/auth/login", credentials{"ellen", "password123"}, &login)
	ellen.token = login.Token
	ellenID := login.User.ID

	// Ellen watched Inception, which is in a library.
	lib, _ := db.CreateLibrary(ctx, "Movies", "movies", []string{"/m"})
	year := 2010
	inception, err := db.UpsertFile(ctx, store.Item{LibraryID: lib.ID, Kind: "movie", Title: "Inception", Year: &year, Path: "/m/Inception.mkv"})
	if err != nil {
		t.Fatal(err)
	}
	db.ApplyMetadata(ctx, inception, &store.Metadata{TMDBID: 27205})
	db.SetProgress(ctx, ellenID, inception, 0, true)

	bo := arrtest.NewBoxOffice(t)
	movie := func(id int, title string, status string) map[string]any {
		return map[string]any{"kind": "movie", "tmdbId": id, "title": title, "year": 2016, "status": status}
	}
	show := func(id int, title string, status string) map[string]any {
		return map[string]any{"kind": "series", "tmdbId": id, "title": title, "year": 2008, "status": status}
	}
	discover := map[string]any{
		"": map[string]any{"rows": []any{
			map[string]any{"id": "for-you", "title": "Recommended for you", "items": []any{movie(329865, "Arrival", "")}},
			map[string]any{"id": "trending", "title": "Trending this week", "more": true,
				"items": []any{movie(27205, "Inception", "available"), show(1396, "Breaking Bad", "wanted"), show(60059, "Better Call Saul", "")}},
			map[string]any{"id": "popular-series", "title": "Popular shows", "items": []any{show(60059, "Better Call Saul", "")}},
			map[string]any{"id": "because-series-1396", "title": "Because you have Breaking Bad", "because": "Breaking Bad",
				"items": []any{movie(27205, "Inception", "available"), show(60059, "Better Call Saul", "")}, "more": true},
			map[string]any{"id": "genre-movie-28", "title": "Action movies", "items": []any{movie(1, "Not shown", "")}},
		}},
		"/rows/because-series-1396": map[string]any{"id": "because-series-1396", "title": "Because you have Breaking Bad", "because": "Breaking Bad",
			"items": []any{show(60059, "Better Call Saul", ""), show(1, "Another", "")}, "page": 1, "pages": 2, "more": true},
		"/rows/like-movie-27205": map[string]any{"id": "like-movie-27205", "items": []any{movie(27205, "Inception", "available"), movie(157336, "Interstellar", "")}, "page": 1, "pages": 3, "more": true},
		"/rows/popular-series":   map[string]any{"id": "popular-series", "title": "Popular shows", "items": []any{show(60059, "Better Call Saul", "")}, "page": 2, "pages": 2},
		"/search":                []any{movie(329865, "Arrival", ""), show(60059, "Better Call Saul", "")},
		"/watch": map[string]any{"networks": []any{map[string]any{"id": 174, "name": "AMC"}}, "country": "US",
			"stream": []any{map[string]any{"id": 8, "name": "Netflix"}}, "countries": []any{"US"}},
	}
	bo.Radarr.Discover, bo.Sonarr.Discover = discover, discover
	bo.Radarr.Catalog = []map[string]any{{"tmdbId": 329865, "title": "Arrival", "year": 2016}}
	bo.Sonarr.Catalog = []map[string]any{{"tmdbId": 60059, "tvdbId": 273181, "title": "Better Call Saul", "year": 2015}}

	var opts requestOptions
	ellen.do("GET", "/api/v1/requests/options", nil, &opts)
	if opts.Discover {
		t.Fatalf("discover before connecting: %+v", opts)
	}
	if code := admin.do("PUT", "/api/v1/integrations/boxoffice", boxOfficeConfig{URL: bo.URL, APIKey: bo.Key}, nil); code != 200 {
		t.Fatalf("connect: %d", code)
	}
	ellen.do("GET", "/api/v1/requests/options", nil, &opts)
	if !opts.Discover {
		t.Fatalf("discover after connecting: %+v", opts)
	}

	// Where to watch a title comes from Box Office too.
	var watch struct {
		Networks []struct{ Name string }
		Stream   []struct{ Name string }
	}
	if code := ellen.do("GET", "/api/v1/discover/watch?kind=show&tmdbId=60059&country=us", nil, &watch); code != 200 || len(watch.Networks) != 1 || watch.Stream[0].Name != "Netflix" {
		t.Fatalf("watch: %d %+v", code, watch)
	}
	if code := ellen.do("GET", "/api/v1/discover/watch?kind=album&tmdbId=1", nil, nil); code != 400 {
		t.Fatalf("watch album: %d", code)
	}

	var view discoverView
	if code := ellen.do("GET", "/api/v1/discover", nil, &view); code != 200 || !view.CanRequest {
		t.Fatalf("discover: %d %+v", code, view)
	}
	var ids []string
	rows := map[string]discoverRow{}
	for _, r := range view.Rows {
		ids = append(ids, r.ID)
		rows[r.ID] = r
	}
	because := "because-" + strconv.FormatInt(inception, 10)
	// Box Office's own rows follow, in its order: "Because you have …"
	// too, without what's here already. Rows Matinee has no name for stay out.
	if want := []string{because, "for-you", "trending", "popular-shows", "have-show-1396"}; !slices.Equal(ids, want) {
		t.Fatalf("rows %v, want %v", ids, want)
	}
	if b := rows[because]; b.Because != "Inception" || len(b.Items) != 1 || b.Items[0].TMDBID != 157336 || !b.More {
		t.Fatalf("because: %+v", b)
	}
	if h := rows["have-show-1396"]; h.Have != "Breaking Bad" || h.Because != "" || len(h.Items) != 1 || h.Items[0].TMDBID != 60059 || !h.More {
		t.Fatalf("have: %+v", h)
	}
	tr := rows["trending"].Items
	if tr[0].Status != titleAvailable || tr[0].ItemID != inception || tr[1].Status != titleAdded || tr[1].Kind != "show" || tr[2].Status != "" || !rows["trending"].More {
		t.Fatalf("trending: %+v", tr)
	}

	var page struct {
		Row   discoverRow
		Page  int
		Pages int
	}
	if code := ellen.do("GET", "/api/v1/discover/rows/popular-shows?page=2", nil, &page); code != 200 || page.Row.Title != "Popular shows" || page.Page != 2 || page.Row.More {
		t.Fatalf("row page: %d %+v", code, page)
	}
	if code := ellen.do("GET", "/api/v1/discover/rows/"+because, nil, &page); code != 200 || page.Row.Because != "Inception" || len(page.Row.Items) != 1 {
		t.Fatalf("because page: %d %+v", code, page)
	}
	if code := ellen.do("GET", "/api/v1/discover/rows/have-show-1396", nil, &page); code != 200 || page.Row.Have != "Breaking Bad" || len(page.Row.Items) != 2 || page.Pages != 2 {
		t.Fatalf("have page: %d %+v", code, page)
	}
	for _, id := range []string{"nope", "have-series-1396", "have-show-x", "have-show-0", "genre-movie-28"} {
		if code := ellen.do("GET", "/api/v1/discover/rows/"+id, nil, nil); code != http.StatusNotFound {
			t.Fatalf("no such row %s: %d", id, code)
		}
	}
	if code := ellen.do("GET", "/api/v1/discover/rows/nope", nil, nil); code != http.StatusNotFound {
		t.Fatalf("no such row: %d", code)
	}
	var found []discoverTitle
	if code := ellen.do("GET", "/api/v1/discover/search?q=a", nil, &found); code != 200 || len(found) != 2 || found[1].Kind != "show" {
		t.Fatalf("search: %d %+v", code, found)
	}

	// Ellen's request waits for an admin; the admin's is added to Box Office.
	var asked discoverTitle
	if code := ellen.do("POST", "/api/v1/discover/requests", map[string]any{"kind": "show", "tmdbId": 60059}, &asked); code != http.StatusCreated || asked.Status != titleRequested {
		t.Fatalf("ellen asks: %d %+v", code, asked)
	}
	if req, err := db.OpenRequest(ctx, "show", 273181); err != nil || req.State != store.RequestPending || req.UserID != ellenID {
		t.Fatalf("ellen's request: %+v %v", req, err)
	}
	if code := ellen.do("POST", "/api/v1/discover/requests", map[string]any{"kind": "show", "tmdbId": 60059}, nil); code != http.StatusConflict {
		t.Fatalf("asking twice: %d", code)
	}
	ellen.do("GET", "/api/v1/discover/search?q=a", nil, &found)
	if found[1].Status != titleRequested {
		t.Fatalf("after asking: %+v", found)
	}
	if code := admin.do("POST", "/api/v1/discover/requests", map[string]any{"kind": "movie", "tmdbId": 329865}, &asked); code != http.StatusCreated || asked.Status != titleAdded {
		t.Fatalf("admin asks: %d %+v", code, asked)
	}
	if len(bo.Radarr.Library) != 1 {
		t.Fatalf("box office library: %+v", bo.Radarr.Library)
	}
}
