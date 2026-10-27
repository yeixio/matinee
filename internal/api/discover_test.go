package api

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/yeixio/matinee/internal/overseerr"
	"github.com/yeixio/matinee/internal/overseerr/overseerrtest"
	"github.com/yeixio/matinee/internal/store"
	"github.com/yeixio/matinee/internal/store/storetest"
)

func TestDiscover(t *testing.T) {
	db := storetest.Open(t)
	srv := &Server{Store: db, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	admin := &client{t: t, base: ts.URL}
	var login struct{ Token string }
	admin.do("POST", "/api/v1/setup", credentials{"admin", "correct horse"}, &login)
	admin.token = login.Token
	var ellenUser store.User
	admin.do("POST", "/api/v1/users", map[string]any{"username": "ellen", "password": "password123"}, &ellenUser)
	admin.do("POST", "/api/v1/users", map[string]any{"username": "sam", "password": "password123"}, nil)
	signIn := func(name string) *client {
		c := &client{t: t, base: ts.URL}
		c.do("POST", "/api/v1/auth/login", credentials{name, "password123"}, &login)
		c.token = login.Token
		return c
	}
	ellen, sam := signIn("ellen"), signIn("sam")

	// Ellen watched Inception, which is in a library, and an episode of Severance.
	ctx := context.Background()
	lib, _ := db.CreateLibrary(ctx, "Movies", "movies", []string{"/m"})
	movie := func(title string, year int, tmdb int64) int64 {
		id, err := db.UpsertFile(ctx, store.Item{LibraryID: lib.ID, Kind: "movie", Title: title, Year: &year, Path: "/m/" + title + ".mkv"})
		if err != nil {
			t.Fatal(err)
		}
		db.ApplyMetadata(ctx, id, &store.Metadata{TMDBID: tmdb})
		return id
	}
	inception := movie("Inception", 2010, 27205)
	interstellar := movie("Interstellar", 2014, 157336)
	tv, _ := db.CreateLibrary(ctx, "Shows", "shows", []string{"/s"})
	show, _ := db.EnsureContainer(ctx, tv.ID, nil, "show", "Severance", nil, nil)
	db.ApplyMetadata(ctx, show, &store.Metadata{TMDBID: 95396})
	one := 1
	season, _ := db.EnsureContainer(ctx, tv.ID, &show, "season", "Season 1", &one, nil)
	episode, _ := db.UpsertFile(ctx, store.Item{LibraryID: tv.ID, ParentID: &season, Kind: "episode", Title: "Good News About Hell", IndexNumber: &one, Path: "/s/e1.mkv"})
	db.SetProgress(ctx, ellenUser.ID, inception, 0, true)
	db.SetProgress(ctx, ellenUser.ID, episode, 60_000, false)

	o := overseerrtest.New(t)
	o.Users = append(o.Users, map[string]any{"id": 2, "email": "ellen@example.com", "plexUsername": "Ellen"}, map[string]any{"id": 3, "displayName": "Family"})
	o.Catalog = []map[string]any{
		{"id": 157336, "mediaType": "movie", "title": "Interstellar", "releaseDate": "2014-11-05", "posterPath": "/i.jpg"},
		{"id": 329865, "mediaType": "movie", "title": "Arrival", "releaseDate": "2016-11-10", "posterPath": "/a.jpg", "overview": "Linguists meet visitors."},
		{"id": 693134, "mediaType": "movie", "title": "Dune: Part Two", "releaseDate": "2024-02-27"},
		{"id": 1396, "mediaType": "tv", "name": "Breaking Bad", "firstAirDate": "2008-01-20", "seasons": []map[string]any{{"seasonNumber": 0}, {"seasonNumber": 1}, {"seasonNumber": 2}}},
		{"id": 60059, "mediaType": "tv", "name": "Better Call Saul", "firstAirDate": "2015-02-08", "seasons": []map[string]any{{"seasonNumber": 1}}},
		{"id": 31, "mediaType": "person", "name": "Tom Hanks"},
	}
	o.Lists["/discover/trending"] = []int{31, 1396, 157336, 693134}
	o.Lists["/discover/movies"] = []int{329865, 157336}
	o.Lists["/movie/27205/recommendations"] = []int{157336, 329865}
	o.Lists["/tv/95396/recommendations"] = []int{60059}

	// Nothing to discover until Overseerr is connected.
	var opts requestOptions
	ellen.do("GET", "/api/v1/requests/options", nil, &opts)
	if opts.Discover {
		t.Fatalf("discover before connecting: %+v", opts)
	}
	if code := ellen.do("GET", "/api/v1/discover", nil, nil); code != http.StatusConflict {
		t.Fatalf("discover before connecting: %d", code)
	}

	// Connecting checks the address and key, and lists Overseerr's users.
	if code := admin.do("POST", "/api/v1/integrations/overseerr/test", overseerr.Config{URL: o.URL, APIKey: "wrong"}, nil); code != http.StatusBadGateway {
		t.Fatalf("test with a wrong key: %d", code)
	}
	var checked overseerrCheck
	if code := admin.do("POST", "/api/v1/integrations/overseerr/test", overseerr.Config{URL: o.URL, APIKey: o.Key}, &checked); code != 200 ||
		checked.Owner.Name != "Mike" || len(checked.Users) != 3 || checked.Version == "" {
		t.Fatalf("test: %d %+v", code, checked)
	}
	if code := ellen.do("PUT", "/api/v1/integrations/overseerr", overseerr.Config{URL: o.URL, APIKey: o.Key}, nil); code != http.StatusForbidden {
		t.Fatalf("non-admin connecting: %d", code)
	}
	if code := admin.do("PUT", "/api/v1/integrations/overseerr", overseerr.Config{URL: o.URL, APIKey: o.Key, RequestAs: 9}, nil); code != http.StatusBadRequest {
		t.Fatalf("requests as a user Overseerr doesn't have: %d", code)
	}
	var view overseerrView
	if code := admin.do("PUT", "/api/v1/integrations/overseerr", overseerr.Config{URL: o.URL, APIKey: o.Key, RequestAs: 3}, &view); code != 200 ||
		!view.Connected || !view.HasAPIKey || view.RequestAs != 3 {
		t.Fatalf("connect: %d %+v", code, view)
	}
	if body := get(t, admin, "/api/v1/integrations/overseerr"); strings.Contains(body, o.Key) {
		t.Fatalf("the API key leaves the server: %s", body)
	}
	// Leaving the key out keeps it, but only for the same address.
	if code := admin.do("PUT", "/api/v1/integrations/overseerr", overseerr.Config{URL: o.URL, RequestAs: 3}, nil); code != 200 {
		t.Fatalf("save without the key: %d", code)
	}
	if code := admin.do("PUT", "/api/v1/integrations/overseerr", overseerr.Config{URL: "http://elsewhere.example"}, nil); code != http.StatusBadRequest {
		t.Fatalf("new address without the key: %d", code)
	}
	ellen.do("GET", "/api/v1/requests/options", nil, &opts)
	if !opts.Discover {
		t.Fatalf("discover after connecting: %+v", opts)
	}

	// Ellen's Discover tab starts with titles like what she watched,
	// leaving out what's in a library.
	var d discoverView
	if code := ellen.do("GET", "/api/v1/discover", nil, &d); code != 200 || !d.CanRequest {
		t.Fatalf("discover: %d %+v", code, d)
	}
	var ids, titles []string
	for _, r := range d.Rows {
		ids, titles = append(ids, r.ID), append(titles, r.Title)
	}
	if got := strings.Join(titles, " | "); got != "Because you watched Severance | Because you watched Inception | Trending | Popular movies" {
		t.Fatalf("rows: %s", got)
	}
	if because := d.Rows[1].Items; len(because) != 1 || because[0].Title.Title != "Arrival" || because[0].Poster != "https://image.tmdb.org/t/p/w342/a.jpg" || because[0].Year != 2016 {
		t.Fatalf("because you watched Inception: %+v", because)
	}
	if show := d.Rows[0].Items; len(show) != 1 || show[0].Kind != "show" || show[0].Title.Title != "Better Call Saul" {
		t.Fatalf("because you watched Severance: %+v", show)
	}
	// Trending leaves people out, and says what's in a library.
	trending := d.Rows[2].Items
	if len(trending) != 3 || trending[0].Kind != "show" || trending[1].Status != titleAvailable || trending[1].ItemID != interstellar {
		t.Fatalf("trending: %+v", trending)
	}

	// Sam hasn't watched anything yet.
	sam.do("GET", "/api/v1/discover", nil, &d)
	if len(d.Rows) != 2 || d.Rows[0].ID != "trending" {
		t.Fatalf("sam's discover: %+v", d.Rows)
	}

	// A row's own page goes through Overseerr's pages, one at a time.
	o.SetPageSize(1)
	srv.discoverCache.clear()
	ellen.do("GET", "/api/v1/discover", nil, &d)
	if i := slices.IndexFunc(d.Rows, func(r discoverRow) bool { return r.ID == "popular-movies" }); i < 0 || !d.Rows[i].More || len(d.Rows[i].Items) != 1 {
		t.Fatalf("popular movies on more than one page: %+v", d.Rows)
	}
	var page struct {
		Row   discoverRow
		Page  int
		Pages int
	}
	if code := ellen.do("GET", "/api/v1/discover/rows/popular-movies?page=2", nil, &page); code != 200 ||
		page.Page != 2 || page.Pages != 2 || page.Row.More || len(page.Row.Items) != 1 || page.Row.Items[0].TMDBID != 157336 || page.Row.Title != "Popular movies" {
		t.Fatalf("popular movies page 2: %d %+v", code, page)
	}
	// Because-you-watched rows page too, still leaving out what's in a library.
	if code := ellen.do("GET", "/api/v1/discover/rows/because-"+strconv.FormatInt(inception, 10)+"?page=1", nil, &page); code != 200 ||
		page.Row.Because != "Inception" || !page.Row.More || len(page.Row.Items) != 0 {
		t.Fatalf("because page 1: %d %+v", code, page)
	}
	ellen.do("GET", "/api/v1/discover/rows/because-"+strconv.FormatInt(inception, 10)+"?page=2", nil, &page)
	if len(page.Row.Items) != 1 || page.Row.Items[0].Title.Title != "Arrival" {
		t.Fatalf("because page 2: %+v", page)
	}
	if code := ellen.do("GET", "/api/v1/discover/rows/nope", nil, nil); code != http.StatusNotFound {
		t.Fatalf("a row that isn't: %d", code)
	}
	o.SetPageSize(0)
	srv.discoverCache.clear()

	// Search finds any title, with spaces sent as %20.
	var found []discoverTitle
	ellen.do("GET", "/api/v1/discover/search?q=part+two", nil, &found)
	if len(found) != 1 || found[0].TMDBID != 693134 || found[0].Status != "" {
		t.Fatalf("search: %+v", found)
	}
	if q := o.Searches[len(o.Searches)-1]; !strings.Contains(q, "query=part%20two") {
		t.Fatalf("search query: %s", q)
	}

	// Ellen asks as her own Overseerr account, which needs approval there.
	var asked discoverTitle
	if code := ellen.do("POST", "/api/v1/discover/requests", map[string]any{"kind": "movie", "tmdbId": 693134}, &asked); code != http.StatusCreated || asked.Status != titleRequested {
		t.Fatalf("request: %d %+v", code, asked)
	}
	if r := o.Requests[0]; num(r["userId"]) != 2 || r["mediaType"] != "movie" || num(r["mediaId"]) != 693134 {
		t.Fatalf("Overseerr got: %+v", r)
	}
	if code := sam.do("POST", "/api/v1/discover/requests", map[string]any{"kind": "movie", "tmdbId": 693134}, nil); code != http.StatusConflict {
		t.Fatalf("asking twice: %d", code)
	}
	ellen.do("GET", "/api/v1/discover/search?q=dune", nil, &found)
	if found[0].Status != titleRequested {
		t.Fatalf("search after asking: %+v", found)
	}
	if code := ellen.do("POST", "/api/v1/discover/requests", map[string]any{"kind": "movie", "tmdbId": 157336}, nil); code != http.StatusConflict {
		t.Fatalf("asking for what's in a library: %d", code)
	}

	// Sam has no Overseerr account, so asks as the admin's pick. A show is
	// asked for with every season but specials.
	if code := sam.do("POST", "/api/v1/discover/requests", map[string]any{"kind": "show", "tmdbId": 1396}, &asked); code != http.StatusCreated {
		t.Fatalf("request a show: %d", code)
	}
	if r := o.Requests[1]; num(r["userId"]) != 3 || r["mediaType"] != "tv" || len(r["seasons"].([]any)) != 2 {
		t.Fatalf("Overseerr got: %+v", r)
	}

	// The admin asks as the key's owner, whose requests Overseerr approves.
	if code := admin.do("POST", "/api/v1/discover/requests", map[string]any{"kind": "movie", "tmdbId": 329865}, &asked); code != http.StatusCreated || asked.Status != titleAdded {
		t.Fatalf("admin request: %d %+v", code, asked)
	}
	if r := o.Requests[2]; r["userId"] != nil {
		t.Fatalf("Overseerr got: %+v", r)
	}

	// With requests off, only admins ask.
	admin.do("PUT", "/api/v1/settings", map[string]any{"requests": requestsOff}, nil)
	if code := ellen.do("POST", "/api/v1/discover/requests", map[string]any{"kind": "show", "tmdbId": 60059}, nil); code != http.StatusForbidden {
		t.Fatalf("asking with requests off: %d", code)
	}
	ellen.do("GET", "/api/v1/discover", nil, &d)
	if d.CanRequest {
		t.Fatal("discover says Ellen can ask with requests off")
	}

	// Disconnecting takes the tab away.
	if code := admin.do("DELETE", "/api/v1/integrations/overseerr", nil, nil); code != http.StatusNoContent {
		t.Fatalf("disconnect: %d", code)
	}
	ellen.do("GET", "/api/v1/requests/options", nil, &opts)
	if opts.Discover {
		t.Fatal("discover after disconnecting")
	}
}

func TestOverseerrDown(t *testing.T) {
	db := storetest.Open(t)
	srv := &Server{Store: db, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	admin := &client{t: t, base: ts.URL}
	var login struct{ Token string }
	admin.do("POST", "/api/v1/setup", credentials{"admin", "correct horse"}, &login)
	admin.token = login.Token

	o := overseerrtest.New(t)
	admin.do("PUT", "/api/v1/integrations/overseerr", overseerr.Config{URL: o.URL, APIKey: o.Key}, nil)
	// A wrong key saved somehow, or Overseerr gone: every row fails.
	o.Key = "rotated"
	if code := admin.do("GET", "/api/v1/discover", nil, nil); code != http.StatusBadGateway {
		t.Fatalf("discover with Overseerr refusing: %d", code)
	}
	// A web page where the API should be.
	if code := admin.do("POST", "/api/v1/integrations/overseerr/test", overseerr.Config{URL: o.URL + "/web", APIKey: "x"}, nil); code != http.StatusBadGateway {
		t.Fatalf("test against a web page: %d", code)
	}
}
