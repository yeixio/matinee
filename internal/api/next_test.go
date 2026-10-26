package api

import (
	"context"
	"testing"

	"github.com/yeixio/matinee/internal/probe"
	"github.com/yeixio/matinee/internal/store"
)

// After each episode comes the next, across seasons but not into specials,
// and after the last a similar show; reaching the credits counts as
// watched; and each person keeps their own auto-play choices.
func TestNextEpisodeAndPlaybackPrefs(t *testing.T) {
	c, _, db := newTestServerStore(t)
	ctx := context.Background()
	var login struct{ Token string }
	c.do("POST", "/api/v1/setup", credentials{"admin", "correct horse"}, &login)
	c.token = login.Token

	tv, _ := db.CreateLibrary(ctx, "TV", "tv", []string{"/t"})
	ids := map[string]int64{}
	show := func(title string, terms ...store.Term) int64 {
		id, _ := db.EnsureContainer(ctx, tv.ID, nil, "show", title, nil, nil)
		if err := db.ApplyDetails(ctx, id, store.DetailsUpdate{Terms: terms}); err != nil {
			t.Fatal(err)
		}
		return id
	}
	season := func(show int64, n int) int64 {
		id, _ := db.EnsureContainer(ctx, tv.ID, &show, "season", "Season", new(n), nil)
		return id
	}
	episode := func(season int64, n int, title string) {
		dur := int64(1_800_000)
		id, err := db.UpsertFile(ctx, store.Item{LibraryID: tv.ID, ParentID: &season, Kind: "episode", Title: title,
			IndexNumber: new(n), DurationMs: &dur, Path: "/t/" + title + ".mkv"})
		if err != nil {
			t.Fatal(err)
		}
		ids[title] = id
	}
	drama := store.Term{Kind: "rec", Term: "drama"}
	office := show("The Office Hours", drama)
	s0, s1, s2 := season(office, 0), season(office, 1), season(office, 2)
	// Added out of order: the next episode goes by number.
	episode(s1, 2, "Second")
	episode(s1, 1, "Pilot")
	episode(s2, 1, "New Season")
	episode(s0, 1, "Blooper Reel")
	episode(s0, 2, "Behind the Scenes")
	other := show("Night Shift", drama)
	episode(season(other, 1), 1, "Night One")

	next := func(title string) nextView {
		t.Helper()
		var v nextView
		if code := c.do("GET", "/api/v1/items/"+itoa(ids[title])+"/next", nil, &v); code != 200 {
			t.Fatalf("next after %s: %d", title, code)
		}
		return v
	}
	for from, want := range map[string]string{"Pilot": "Second", "Second": "New Season", "Blooper Reel": "Behind the Scenes"} {
		if v := next(from); v.Next == nil || v.Next.Title != want || v.Similar != nil {
			t.Errorf("after %s: %+v, want %s", from, v.Next, want)
		}
	}
	if v := next("Behind the Scenes"); v.Next != nil {
		t.Errorf("specials lead into the show: %+v", v.Next)
	}
	if v := next("New Season"); v.Next != nil || v.Similar == nil || v.Similar.Title != "Night Shift" {
		t.Errorf("after the last episode: %+v", v)
	}
	if v := next("Night One"); v.Next != nil || v.Similar == nil || v.Similar.Title != "The Office Hours" {
		t.Errorf("after Night Shift: %+v", v)
	}

	// Reaching the credits marks an episode watched, even well before 92%.
	if _, err := db.SetMediaInfo(ctx, ids["Pilot"], 0, 0, `{"durationMs":1800000,"creditsMs":1500000}`, nil,
		store.MediaKey(probe.MediaInfoVersion, 0, 0)); err != nil {
		t.Fatal(err)
	}
	c.do("PUT", "/api/v1/items/"+itoa(ids["Pilot"])+"/progress", map[string]any{"positionMs": 1_400_000}, nil)
	var it store.Item
	c.do("GET", "/api/v1/items/"+itoa(ids["Pilot"]), nil, &it)
	if it.Played || it.PositionMs != 1_400_000 {
		t.Fatalf("before the credits: %+v", it)
	}
	c.do("PUT", "/api/v1/items/"+itoa(ids["Pilot"])+"/progress", map[string]any{"positionMs": 1_510_000}, nil)
	c.do("GET", "/api/v1/items/"+itoa(ids["Pilot"]), nil, &it)
	if !it.Played {
		t.Fatalf("in the credits: %+v", it)
	}

	// Auto-play is on by default, per person.
	var p playbackPrefs
	c.do("GET", "/api/v1/me/playback", nil, &p)
	if p != defaultPlayback {
		t.Fatalf("default: %+v", p)
	}
	if code := c.do("PUT", "/api/v1/me/playback", playbackPrefs{AutoPlay: true, Binge: true}, &p); code != 200 || !p.Binge {
		t.Fatalf("binge: %d %+v", code, p)
	}
	c.do("GET", "/api/v1/me/playback", nil, &p)
	if !p.Binge || !p.AutoPlay {
		t.Fatalf("saved: %+v", p)
	}
	if code := c.do("PUT", "/api/v1/me/playback", map[string]any{"countdown": 5}, nil); code != 400 {
		t.Errorf("unknown field: %d", code)
	}
}
