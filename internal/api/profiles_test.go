package api

import (
	"context"
	"net/http"
	"slices"
	"sort"
	"testing"

	"github.com/yeixio/matinee/internal/rating"
	"github.com/yeixio/matinee/internal/store"
)

func TestProfilesAndKidsMode(t *testing.T) {
	c, _, db := newTestServerStore(t)
	ctx := context.Background()
	var login struct{ Token string }
	c.do("POST", "/api/v1/setup", credentials{"admin", "correct horse"}, &login)
	c.token = login.Token

	// Movies rated for every level, one without a rating, a show rated for
	// kids, and home videos nobody rated.
	movies, _ := db.CreateLibrary(ctx, "Movies", "movies", []string{"/m"})
	tv, _ := db.CreateLibrary(ctx, "TV", "tv", []string{"/t"})
	videos, _ := db.CreateLibrary(ctx, "Home videos", "movies", []string{"/v"})
	music, _ := db.CreateLibrary(ctx, "Music", "music", []string{"/a"})
	ids := map[string]int64{}
	add := func(lib store.Library, parent *int64, kind, title string, level int) int64 {
		t.Helper()
		id, err := db.UpsertFile(ctx, store.Item{LibraryID: lib.ID, ParentID: parent, Kind: kind, Title: title, Path: "/" + title + ".mp4"})
		if err != nil {
			t.Fatal(err)
		}
		if level > 0 {
			if err := db.ApplyDetails(ctx, id, store.DetailsUpdate{RatingLevel: level}); err != nil {
				t.Fatal(err)
			}
		}
		ids[title] = id
		return id
	}
	add(movies, nil, "movie", "Toy Tale", rating.LittleKids)
	add(movies, nil, "movie", "Space Saga", rating.Teens)
	add(movies, nil, "movie", "Grim Night", rating.Adults)
	add(movies, nil, "movie", "Mystery Reel", 0)
	add(videos, nil, "movie", "Birthday Party", 0)
	add(music, nil, "track", "Lullaby", 0)
	show, _ := db.EnsureContainer(ctx, tv.ID, nil, "show", "Cartoon Club", nil, nil)
	db.ApplyDetails(ctx, show, store.DetailsUpdate{RatingLevel: rating.Kids})
	ids["Cartoon Club"] = show
	season, _ := db.EnsureContainer(ctx, tv.ID, &show, "season", "Season 1", new(1), nil)
	add(tv, &season, "episode", "Cartoon Pilot", 0)
	late, _ := db.EnsureContainer(ctx, tv.ID, nil, "show", "Late Show", nil, nil)
	db.ApplyDetails(ctx, late, store.DetailsUpdate{RatingLevel: rating.Adults})
	lateSeason, _ := db.EnsureContainer(ctx, tv.ID, &late, "season", "Season 1", new(1), nil)
	add(tv, &lateSeason, "episode", "Late Pilot", 0)

	var me store.User
	c.do("GET", "/api/v1/me", nil, &me)

	// Without profiles, "Who's watching?" has only the account.
	var who struct {
		Current  int64
		Profiles []profileChoice
	}
	c.do("GET", "/api/v1/profiles", nil, &who)
	if len(who.Profiles) != 1 || !who.Profiles[0].Account || who.Current != me.ID {
		t.Fatalf("profiles before any: %+v", who)
	}

	// A kids profile gets movie and TV libraries by rating unless told otherwise.
	var kid store.Profile
	if code := c.do("POST", "/api/v1/profiles", map[string]any{"accountId": me.ID, "name": "Sam", "kidsLevel": rating.Kids}, &kid); code != http.StatusCreated {
		t.Fatalf("create kids profile: %d", code)
	}
	want := map[int64]string{movies.ID: "rated", tv.ID: "rated", videos.ID: "rated"}
	if len(kid.Libraries) != 3 || kid.Libraries[movies.ID] != "rated" || kid.Libraries[videos.ID] != "rated" || kid.KidsLevel != rating.Kids || kid.Avatar == "" {
		t.Fatalf("kids profile = %+v, want libraries %v", kid, want)
	}
	// Home videos have no ratings, so trust the whole library.
	libs := map[string]string{itoa(movies.ID): "rated", itoa(tv.ID): "rated", itoa(videos.ID): "all"}
	if code := c.do("PUT", "/api/v1/profiles/"+itoa(kid.ID), map[string]any{"libraries": libs}, &kid); code != 200 || kid.Libraries[videos.ID] != "all" {
		t.Fatalf("update kids profile: %d %+v", code, kid)
	}
	var ellen store.Profile
	c.do("POST", "/api/v1/profiles", map[string]any{"accountId": me.ID, "name": "Ellen", "pin": "1234"}, &ellen)
	if !ellen.HasPIN || ellen.KidsLevel != 0 {
		t.Fatalf("Ellen = %+v", ellen)
	}
	for _, bad := range []map[string]any{
		{"accountId": me.ID, "name": ""},
		{"accountId": me.ID, "name": "x", "pin": "12a4"},
		{"accountId": me.ID, "name": "x", "kidsLevel": 4},
		{"accountId": me.ID, "name": "x", "avatar": "plaid"},
		{"accountId": kid.ID, "name": "x"},
	} {
		if code := c.do("POST", "/api/v1/profiles", bad, nil); code != http.StatusBadRequest {
			t.Errorf("create %v: %d, want 400", bad, code)
		}
	}

	// Profiles aren't accounts: not listed, not counted, can't sign in.
	var users []store.User
	c.do("GET", "/api/v1/users", nil, &users)
	if len(users) != 1 {
		t.Fatalf("users = %+v", users)
	}
	if code := c.do("POST", "/api/v1/auth/login", map[string]string{"username": "Sam", "password": ""}, nil); code != http.StatusUnauthorized {
		t.Fatalf("signing in as a profile: %d", code)
	}

	c.do("GET", "/api/v1/profiles", nil, &who)
	if len(who.Profiles) != 3 || who.Profiles[1].Name != "Sam" || who.Profiles[2].Name != "Ellen" || !who.Profiles[2].HasPIN {
		t.Fatalf("profiles = %+v", who.Profiles)
	}

	// The TV switches to Sam. Same sign-in, different viewer.
	tvc := &client{t: t, base: c.base}
	tvc.do("POST", "/api/v1/auth/login", map[string]string{"username": "admin", "password": "correct horse", "deviceName": "Living room"}, &login)
	tvc.token = login.Token
	if code := tvc.do("POST", "/api/v1/profiles/select", map[string]any{"id": kid.ID}, nil); code != 200 {
		t.Fatalf("select Sam: %d", code)
	}
	tvc.do("GET", "/api/v1/me", nil, &me)
	if me.ID != kid.ID || me.KidsLevel != rating.Kids || me.Username != "Sam" || me.IsAdmin {
		t.Fatalf("me as Sam = %+v", me)
	}

	titles := func(cl *client, path string) []string {
		t.Helper()
		var items []store.Item
		if code := cl.do("GET", path, nil, &items); code != 200 {
			t.Fatalf("%s: %d", path, code)
		}
		var out []string
		for _, it := range items {
			out = append(out, it.Title)
		}
		sort.Strings(out)
		return out
	}
	visible := []string{"Birthday Party", "Cartoon Club", "Cartoon Pilot", "Toy Tale"}
	var seen []string
	for _, q := range []string{"toy", "space", "grim", "mystery", "birthday", "lullaby", "cartoon", "late"} {
		seen = append(seen, titles(tvc, "/api/v1/search?q="+q)...)
	}
	sort.Strings(seen)
	if !slices.Equal(seen, visible) {
		t.Fatalf("Sam's search found %v, want %v", seen, visible)
	}
	if got := titles(tvc, "/api/v1/libraries/"+itoa(movies.ID)+"/items"); !slices.Equal(got, []string{"Toy Tale"}) {
		t.Errorf("Sam's movies = %v", got)
	}
	if got := titles(tvc, "/api/v1/libraries/"+itoa(music.ID)+"/items"); len(got) != 0 {
		t.Errorf("Sam's music = %v", got)
	}
	var libList []libraryView
	tvc.do("GET", "/api/v1/libraries", nil, &libList)
	if len(libList) != 3 {
		t.Errorf("Sam's libraries = %+v", libList)
	}
	// Nor can a kids profile see what a hidden library could be filtered by.
	for _, path := range []string{"/filters", "/people"} {
		if code := tvc.do("GET", "/api/v1/libraries/"+itoa(music.ID)+path, nil, nil); code != 404 {
			t.Errorf("Sam's music%s = %d, want 404", path, code)
		}
		if code := tvc.do("GET", "/api/v1/libraries/"+itoa(movies.ID)+path, nil, nil); code != 200 {
			t.Errorf("Sam's movies%s = %d, want 200", path, code)
		}
	}
	for _, title := range []string{"Grim Night", "Space Saga", "Mystery Reel", "Late Pilot", "Lullaby"} {
		if code := tvc.do("GET", "/api/v1/items/"+itoa(ids[title]), nil, nil); code != http.StatusNotFound {
			t.Errorf("Sam opening %s: %d, want 404", title, code)
		}
		if code := tvc.do("GET", "/api/v1/items/"+itoa(ids[title])+"/file", nil, nil); code != http.StatusNotFound {
			t.Errorf("Sam playing %s: %d, want 404", title, code)
		}
	}
	if code := tvc.do("GET", "/api/v1/items/"+itoa(ids["Cartoon Pilot"]), nil, nil); code != 200 {
		t.Errorf("Sam opening an episode of a show for kids: %d", code)
	}
	for _, rt := range []struct{ method, path string }{
		{"GET", "/api/v1/requests"}, {"POST", "/api/v1/requests"}, {"GET", "/api/v1/upcoming"},
		{"GET", "/api/v1/devices"}, {"GET", "/api/v1/issues"}, {"POST", "/api/v1/items/" + itoa(ids["Toy Tale"]) + "/issues"},
	} {
		if code := tvc.do(rt.method, rt.path, nil, nil); code != http.StatusForbidden {
			t.Errorf("Sam %s %s: %d, want 403", rt.method, rt.path, code)
		}
	}
	for _, path := range []string{"/api/v1/settings", "/api/v1/profiles/manage"} {
		if code := tvc.do("GET", path, nil, nil); code != http.StatusForbidden {
			t.Errorf("Sam %s: %d, want 403", path, code)
		}
	}

	// Progress belongs to the profile.
	tvc.do("PUT", "/api/v1/items/"+itoa(ids["Cartoon Pilot"])+"/progress", map[string]any{"positionMs": 5000}, nil)
	var home struct{ ContinueWatching []store.Item }
	tvc.do("GET", "/api/v1/home", nil, &home)
	if len(home.ContinueWatching) != 1 {
		t.Errorf("Sam's continue watching = %+v", home.ContinueWatching)
	}
	c.do("GET", "/api/v1/home", nil, &home)
	if len(home.ContinueWatching) != 0 {
		t.Errorf("admin's continue watching = %+v", home.ContinueWatching)
	}

	// Ellen's PIN; five wrong tries lock her for a minute.
	if code := tvc.do("POST", "/api/v1/profiles/select", map[string]any{"id": ellen.ID}, nil); code != http.StatusForbidden {
		t.Fatalf("Ellen without PIN: %d", code)
	}
	if code := tvc.do("POST", "/api/v1/profiles/select", map[string]any{"id": ellen.ID, "pin": "1234"}, nil); code != 200 {
		t.Fatalf("Ellen with PIN: %d", code)
	}
	tvc.do("GET", "/api/v1/me", nil, &me)
	if me.ID != ellen.ID || me.AccountID == 0 {
		t.Fatalf("me as Ellen = %+v", me)
	}
	tvc.do("POST", "/api/v1/profiles/select", map[string]any{"id": kid.ID}, nil)
	for range pinTries {
		tvc.do("POST", "/api/v1/profiles/select", map[string]any{"id": ellen.ID, "pin": "0000"}, nil)
	}
	if code := tvc.do("POST", "/api/v1/profiles/select", map[string]any{"id": ellen.ID, "pin": "1234"}, nil); code != http.StatusTooManyRequests {
		t.Fatalf("Ellen after %d wrong PINs: %d, want 429", pinTries, code)
	}

	// The account itself can have a PIN, so kids can't switch back to it.
	c.do("PUT", "/api/v1/profiles/"+itoa(users[0].ID), map[string]any{"pin": "9876"}, nil)
	if code := tvc.do("POST", "/api/v1/profiles/select", map[string]any{"id": users[0].ID}, nil); code != http.StatusForbidden {
		t.Fatalf("back to the account without its PIN: %d", code)
	}
	if code := c.do("PUT", "/api/v1/profiles/"+itoa(users[0].ID), map[string]any{"name": "Boss"}, nil); code != http.StatusBadRequest {
		t.Errorf("renaming an account here: %d", code)
	}
	if code := tvc.do("POST", "/api/v1/profiles/select", map[string]any{"id": users[0].ID, "pin": "9876"}, nil); code != 200 {
		t.Fatalf("back to the account: %d", code)
	}
	if code := tvc.do("GET", "/api/v1/settings", nil, nil); code != 200 {
		t.Fatalf("admin settings after switching back: %d", code)
	}

	// Removing a profile takes its progress, and devices fall back to the account.
	tvc.do("POST", "/api/v1/profiles/select", map[string]any{"id": kid.ID}, nil)
	if code := c.do("DELETE", "/api/v1/profiles/"+itoa(kid.ID), nil, nil); code != http.StatusNoContent {
		t.Fatalf("delete Sam: %d", code)
	}
	tvc.do("GET", "/api/v1/me", nil, &me)
	if me.ID != users[0].ID {
		t.Fatalf("me after Sam was removed = %+v", me)
	}
	if code := c.do("DELETE", "/api/v1/profiles/"+itoa(users[0].ID), nil, nil); code != http.StatusNotFound {
		t.Fatalf("deleting an account as a profile: %d", code)
	}
}

func TestHomeProfiles(t *testing.T) {
	c, _, db := newTestServerStore(t)
	ctx := context.Background()
	var login struct{ Token string }
	c.do("POST", "/api/v1/setup", credentials{"admin", "correct horse"}, &login)
	c.token = login.Token
	movies, _ := db.CreateLibrary(ctx, "Movies", "movies", []string{"/m"})
	for title, level := range map[string]int{"Toy Tale": rating.LittleKids, "Grim Night": rating.Adults} {
		id, _ := db.UpsertFile(ctx, store.Item{LibraryID: movies.ID, Kind: "movie", Title: title, Path: "/" + title})
		db.ApplyDetails(ctx, id, store.DetailsUpdate{RatingLevel: level})
	}

	var manage struct {
		Accounts []accountProfiles
		Avatars  []string
	}
	c.do("GET", "/api/v1/profiles/manage", nil, &manage)
	if len(manage.Accounts) != 2 || !manage.Accounts[0].Account.Guest || len(manage.Avatars) == 0 {
		t.Fatalf("manage = %+v", manage)
	}
	home := manage.Accounts[0].Account
	var kid store.Profile
	c.do("POST", "/api/v1/profiles", map[string]any{"accountId": home.ID, "name": "Little one", "kidsLevel": rating.LittleKids}, &kid)

	// A device at home that hasn't signed in sees the Home account's profiles.
	guest := &client{t: t, base: c.base}
	var who struct{ Profiles []profileChoice }
	guest.do("GET", "/api/v1/profiles", nil, &who)
	if len(who.Profiles) != 2 || who.Profiles[0].Name != "Home" || who.Profiles[1].Name != "Little one" {
		t.Fatalf("home profiles = %+v", who.Profiles)
	}
	// Signed-in accounts don't see them.
	if code := c.do("POST", "/api/v1/profiles/select", map[string]any{"id": kid.ID}, nil); code != http.StatusNotFound {
		t.Fatalf("admin picking a Home profile: %d", code)
	}

	var picked struct {
		User  store.User
		Token string
	}
	if code := guest.do("POST", "/api/v1/profiles/select", map[string]any{"id": kid.ID, "deviceName": "Living room"}, &picked); code != 200 || picked.Token == "" {
		t.Fatalf("picking at home: %d %+v", code, picked)
	}
	guest.token = picked.Token
	var me store.User
	guest.do("GET", "/api/v1/me", nil, &me)
	if me.ID != kid.ID || !me.Guest || me.KidsLevel != rating.LittleKids {
		t.Fatalf("me = %+v", me)
	}
	var items []store.Item
	guest.do("GET", "/api/v1/libraries/"+itoa(movies.ID)+"/items", nil, &items)
	if len(items) != 1 || items[0].Title != "Toy Tale" {
		t.Fatalf("little one's movies = %+v", items)
	}
	// The home session can't do more than a guest can, and stops away from home.
	if code := guest.do("GET", "/api/v1/downloads", nil, nil); code != http.StatusUnauthorized {
		t.Errorf("downloads with a home session: %d", code)
	}
	req, _ := http.NewRequest("GET", c.base+"/api/v1/home", nil)
	req.Header.Set("Authorization", "Bearer "+picked.Token)
	req.Header.Set("X-Forwarded-For", "203.0.113.9")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("home session from outside: %d", res.StatusCode)
	}
	// Back to the shared Home account.
	if code := guest.do("POST", "/api/v1/profiles/select", map[string]any{"id": home.ID}, nil); code != 200 {
		t.Fatalf("back to Home: %d", code)
	}
	guest.do("GET", "/api/v1/libraries/"+itoa(movies.ID)+"/items", nil, &items)
	if len(items) != 2 {
		t.Fatalf("Home's movies = %+v", items)
	}
}
