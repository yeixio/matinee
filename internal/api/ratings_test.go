package api

import (
	"context"
	"net/http"
	"testing"

	"github.com/yeixio/matinee/internal/store"
)

// Each person rates titles for themselves, and can sort by their ratings.
func TestRatings(t *testing.T) {
	c, _, db := newTestServerStore(t)
	var login struct{ Token string }
	c.do("POST", "/api/setup", credentials{"admin", "correct horse"}, &login)
	c.token = login.Token
	ctx := context.Background()
	lib, _ := db.CreateLibrary(ctx, "Movies", "movies", []string{"/m"})
	heat, _ := db.UpsertFile(ctx, store.Item{LibraryID: lib.ID, Kind: "movie", Title: "Heat", Path: "/m/heat.mkv"})
	ronin, _ := db.UpsertFile(ctx, store.Item{LibraryID: lib.ID, Kind: "movie", Title: "Ronin", Path: "/m/ronin.mkv"})

	if code := c.do("PUT", "/api/v1/items/"+itoa(heat)+"/rating", map[string]int{"rating": 11}, nil); code != http.StatusBadRequest {
		t.Fatalf("11: %d", code)
	}
	if code := c.do("PUT", "/api/v1/items/"+itoa(heat)+"/rating", map[string]int{"rating": 7}, nil); code != http.StatusNoContent {
		t.Fatalf("rate: %d", code)
	}
	c.do("PUT", "/api/v1/items/"+itoa(ronin)+"/rating", map[string]int{"rating": 9}, nil)
	var it store.Item
	c.do("GET", "/api/v1/items/"+itoa(heat), nil, &it)
	if it.MyRating == nil || *it.MyRating != 7 {
		t.Fatalf("myRating: %v", it.MyRating)
	}
	var items []store.Item
	c.do("GET", "/api/v1/libraries/"+itoa(lib.ID)+"/items?sort=myRating", nil, &items)
	if len(items) != 2 || items[0].ID != ronin {
		t.Fatalf("sorted by my rating: %+v", items)
	}
	if code := c.do("DELETE", "/api/v1/items/"+itoa(heat)+"/rating", nil, nil); code != http.StatusNoContent {
		t.Fatalf("clear: %d", code)
	}
	it = store.Item{}
	c.do("GET", "/api/v1/items/"+itoa(heat), nil, &it)
	if it.MyRating != nil {
		t.Fatalf("still rated: %v", *it.MyRating)
	}
}
