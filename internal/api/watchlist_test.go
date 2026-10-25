package api

import (
	"net/http"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/yeixio/matinee/internal/store"
)

func TestWatchList(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	c, tmp := newTestServer(t)
	var login struct{ Token string }
	c.do("POST", "/api/v1/setup", credentials{"admin", "correct horse"}, &login)
	c.token = login.Token

	lavfi := []string{"-f", "lavfi", "-i", "testsrc=duration=2:size=160x120:rate=24", "-c:v", "libx264", "-preset", "ultrafast"}
	makeMedia(t, filepath.Join(tmp, "movies", "First (2020)", "First (2020).mp4"), lavfi...)
	makeMedia(t, filepath.Join(tmp, "movies", "Second (2021)", "Second (2021).mp4"), lavfi...)
	makeMedia(t, filepath.Join(tmp, "tv", "Some Show", "Season 1", "Some Show - S01E01 - Pilot.mp4"), lavfi...)
	for _, lib := range []struct{ name, kind string }{{"Movies", "movies"}, {"TV", "tv"}} {
		body := map[string]any{"name": lib.name, "kind": lib.kind, "paths": []string{filepath.Join(tmp, lib.kind)}}
		if code := c.do("POST", "/api/v1/libraries", body, nil); code != http.StatusCreated {
			t.Fatalf("create %s: %d", lib.kind, code)
		}
	}
	waitForScan(t, c)

	byTitle := map[string]store.Item{}
	var hits []store.Item
	for _, q := range []string{"first", "second", "some", "pilot"} {
		hits = nil
		c.do("GET", "/api/v1/search?q="+q, nil, &hits)
		if len(hits) != 1 {
			t.Fatalf("search %q: %+v", q, hits)
		}
		byTitle[hits[0].Title] = hits[0]
	}
	first, second, show, pilot := byTitle["First"], byTitle["Second"], byTitle["Some Show"], byTitle["Pilot"]

	list := func(cl *client) []string {
		t.Helper()
		var items []store.Item
		if code := cl.do("GET", "/api/v1/watchlist", nil, &items); code != 200 {
			t.Fatalf("watch list: %d", code)
		}
		var titles []string
		for _, it := range items {
			if !it.OnWatchList {
				t.Errorf("%s listed but not marked onWatchList", it.Title)
			}
			titles = append(titles, it.Title)
		}
		return titles
	}

	if got := list(c); len(got) != 0 {
		t.Fatalf("new watch list: %v", got)
	}
	for _, it := range []store.Item{first, show, second, first} {
		if code := c.do("PUT", "/api/v1/watchlist/"+itoa(it.ID), nil, nil); code != http.StatusNoContent {
			t.Fatalf("add %s: %d", it.Title, code)
		}
	}
	// Newest first, and adding again keeps an item's place.
	if got := list(c); len(got) != 3 || got[0] != "Second" || got[1] != "Some Show" || got[2] != "First" {
		t.Fatalf("watch list = %v", got)
	}
	if code := c.do("PUT", "/api/v1/watchlist/"+itoa(pilot.ID), nil, nil); code != http.StatusBadRequest {
		t.Errorf("adding an episode: %d", code)
	}
	if code := c.do("PUT", "/api/v1/watchlist/999999", nil, nil); code != http.StatusNotFound {
		t.Errorf("adding a missing item: %d", code)
	}
	var detail itemView
	c.do("GET", "/api/v1/items/"+itoa(show.ID), nil, &detail)
	if !detail.OnWatchList {
		t.Error("item page doesn't say it's on the watch list")
	}
	var home struct{ WatchList []store.Item }
	c.do("GET", "/api/v1/home", nil, &home)
	if len(home.WatchList) != 3 {
		t.Errorf("home watch list row: %+v", home.WatchList)
	}

	// Watching a movie takes it off; a show stays.
	c.do("PUT", "/api/v1/items/"+itoa(second.ID)+"/progress", map[string]any{"positionMs": 0, "played": true}, nil)
	c.do("PUT", "/api/v1/items/"+itoa(pilot.ID)+"/progress", map[string]any{"positionMs": 0, "played": true}, nil)
	if got := list(c); len(got) != 2 || got[0] != "Some Show" {
		t.Fatalf("after watching: %v", got)
	}

	if code := c.do("DELETE", "/api/v1/watchlist/"+itoa(show.ID), nil, nil); code != http.StatusNoContent {
		t.Fatalf("remove: %d", code)
	}
	if code := c.do("DELETE", "/api/v1/watchlist/"+itoa(show.ID), nil, nil); code != http.StatusNoContent {
		t.Fatalf("remove again: %d", code)
	}
	if got := list(c); len(got) != 1 || got[0] != "First" {
		t.Fatalf("after removing: %v", got)
	}

	// Each person has their own list; devices at home share the Home one.
	guest := &client{t: t, base: c.base}
	if got := list(guest); len(got) != 0 {
		t.Fatalf("home list sees the admin's: %v", got)
	}
	if code := guest.do("PUT", "/api/v1/watchlist/"+itoa(second.ID), nil, nil); code != http.StatusNoContent {
		t.Fatalf("home add: %d", code)
	}
	if got := list(guest); len(got) != 1 || got[0] != "Second" {
		t.Fatalf("home list: %v", got)
	}
	if got := list(c); len(got) != 1 || got[0] != "First" {
		t.Fatalf("admin list changed: %v", got)
	}
	var item store.Item
	c.do("GET", "/api/v1/items/"+itoa(second.ID), nil, &item)
	if item.OnWatchList {
		t.Error("the Home list's item shows as on the admin's")
	}
}

func TestHideFromContinueWatching(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	c, tmp := newTestServer(t)
	var login struct{ Token string }
	c.do("POST", "/api/v1/setup", credentials{"admin", "correct horse"}, &login)
	c.token = login.Token
	makeMedia(t, filepath.Join(tmp, "movies", "Long One (2020)", "Long One (2020).mp4"),
		"-f", "lavfi", "-i", "testsrc=duration=10:size=160x120:rate=24", "-c:v", "libx264", "-preset", "ultrafast")
	c.do("POST", "/api/v1/libraries", map[string]any{"name": "Movies", "kind": "movies", "paths": []string{filepath.Join(tmp, "movies")}}, nil)
	waitForScan(t, c)
	var hits []store.Item
	c.do("GET", "/api/v1/search?q=long", nil, &hits)
	if len(hits) != 1 {
		t.Fatalf("search: %+v", hits)
	}
	movie := hits[0]

	row := func() int {
		t.Helper()
		var home struct{ ContinueWatching []store.Item }
		c.do("GET", "/api/v1/home", nil, &home)
		return len(home.ContinueWatching)
	}
	c.do("PUT", "/api/v1/items/"+itoa(movie.ID)+"/progress", map[string]any{"positionMs": 3000}, nil)
	if row() != 1 {
		t.Fatal("not in Continue Watching after starting it")
	}
	if code := c.do("DELETE", "/api/v1/continue-watching/"+itoa(movie.ID), nil, nil); code != http.StatusNoContent {
		t.Fatalf("hide: %d", code)
	}
	if row() != 0 {
		t.Fatal("still in Continue Watching after removing it")
	}
	// Its place is kept for its page.
	var it store.Item
	c.do("GET", "/api/v1/items/"+itoa(movie.ID), nil, &it)
	if it.PositionMs != 3000 {
		t.Fatalf("position after hiding = %d", it.PositionMs)
	}
	// Progress from before the removal, sent late by an offline device, doesn't bring it back.
	c.do("POST", "/api/v1/sync/progress", map[string]any{"events": []map[string]any{{"itemId": movie.ID, "positionMs": 4000, "at": 1}}}, nil)
	if row() != 0 {
		t.Fatal("stale offline progress brought it back")
	}
	// Playing it again does.
	c.do("PUT", "/api/v1/items/"+itoa(movie.ID)+"/progress", map[string]any{"positionMs": 5000}, nil)
	if row() != 1 {
		t.Fatal("playing again didn't bring it back")
	}
}
