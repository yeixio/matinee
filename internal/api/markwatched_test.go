package api

import (
	"context"
	"net/http"
	"testing"

	"github.com/yeixio/matinee/internal/store"
)

// A movie, an episode, or a whole season or show can be marked watched by hand.
func TestMarkWatched(t *testing.T) {
	c, _, db := newTestServerStore(t)
	var login struct{ Token string }
	c.do("POST", "/api/setup", credentials{"admin", "correct horse"}, &login)
	c.token = login.Token
	ctx := context.Background()
	movies, _ := db.CreateLibrary(ctx, "Movies", "movies", []string{"/m"})
	heat, _ := db.UpsertFile(ctx, store.Item{LibraryID: movies.ID, Kind: "movie", Title: "Heat", Path: "/m/heat.mkv"})
	tv, _ := db.CreateLibrary(ctx, "TV", "shows", []string{"/tv"})
	one, two := 1, 2
	show, _ := db.EnsureContainer(ctx, tv.ID, nil, "show", "Dark", nil, nil)
	s1, _ := db.EnsureContainer(ctx, tv.ID, &show, "season", "Season 1", &one, nil)
	s2, _ := db.EnsureContainer(ctx, tv.ID, &show, "season", "Season 2", &two, nil)
	var eps []int64
	for i, season := range []int64{s1, s1, s2} {
		id, _ := db.UpsertFile(ctx, store.Item{LibraryID: tv.ID, ParentID: &season, Kind: "episode", Title: "Ep", Path: "/tv/" + itoa(int64(i)) + ".mkv"})
		eps = append(eps, id)
	}
	// Half way into an episode, it's on Continue Watching.
	c.do("PUT", "/api/v1/items/"+itoa(eps[0])+"/progress", map[string]any{"positionMs": 60_000}, nil)

	var n watchedCount
	if code := c.do("PUT", "/api/v1/items/"+itoa(heat)+"/watched", map[string]bool{"played": true}, &n); code != http.StatusOK || n != (watchedCount{1, 1}) {
		t.Fatalf("movie: %d %+v", code, n)
	}
	var it store.Item
	c.do("GET", "/api/v1/items/"+itoa(heat), nil, &it)
	if !it.Played {
		t.Fatal("movie isn't watched")
	}

	if code := c.do("PUT", "/api/v1/items/"+itoa(s1)+"/watched", map[string]bool{"played": true}, &n); code != http.StatusOK || n != (watchedCount{2, 2}) {
		t.Fatalf("season: %d %+v", code, n)
	}
	c.do("GET", "/api/v1/items/"+itoa(show)+"/watched", nil, &n)
	if n != (watchedCount{3, 2}) {
		t.Fatalf("show: %+v", n)
	}
	c.do("GET", "/api/v1/items/"+itoa(eps[0]), nil, &it)
	if !it.Played || it.PositionMs != 0 {
		t.Fatalf("episode: %+v", it)
	}

	if code := c.do("PUT", "/api/v1/items/"+itoa(show)+"/watched", map[string]bool{"played": false}, &n); code != http.StatusOK || n != (watchedCount{3, 0}) {
		t.Fatalf("unwatch show: %d %+v", code, n)
	}
	if code := c.do("PUT", "/api/v1/items/"+itoa(movies.ID+1000)+"/watched", map[string]bool{"played": true}, nil); code != http.StatusNotFound {
		t.Fatalf("missing item: %d", code)
	}
}
