package api

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/yeixio/matinee/internal/store"
	"github.com/yeixio/matinee/internal/together"
)

func TestWatchTogether(t *testing.T) {
	c, _, db := newTestServerStore(t)
	ctx := context.Background()
	var login struct{ Token string }
	c.do("POST", "/api/v1/setup", credentials{"admin", "correct horse"}, &login)
	c.token = login.Token

	movies, _ := db.CreateLibrary(ctx, "Movies", "movies", []string{"/m"})
	videos, _ := db.CreateLibrary(ctx, "Home videos", "movies", []string{"/v"})
	film, _ := db.UpsertFile(ctx, store.Item{LibraryID: movies.ID, Kind: "movie", Title: "Space Saga", Path: "/m/space.mp4"})
	sequel, _ := db.UpsertFile(ctx, store.Item{LibraryID: movies.ID, Kind: "movie", Title: "Space Saga 2", Path: "/m/space2.mp4"})
	party, _ := db.UpsertFile(ctx, store.Item{LibraryID: videos.ID, Kind: "movie", Title: "Birthday Party", Path: "/v/party.mp4"})

	// A friend who sees only the movies.
	var inv inviteView
	c.do("POST", "/api/v1/invites", map[string]any{"libraries": []int64{movies.ID}}, &inv)
	var joined struct{ Token string }
	(&client{t: t, base: c.base}).do("POST", "/api/v1/join/"+inv.Code, map[string]string{"username": "sam", "password": "password123"}, &joined)
	friend := &client{t: t, base: c.base, token: joined.Token}

	host := map[string]any{"clientId": "host-client-1", "device": "Living room TV", "platform": "tvos"}
	guest := map[string]any{"clientId": "guest-client-1", "device": "Sam's phone", "platform": "ios"}
	with := func(m map[string]any, kv ...any) map[string]any {
		out := map[string]any{}
		for k, v := range m {
			out[k] = v
		}
		for i := 0; i < len(kv); i += 2 {
			out[kv[i].(string)] = kv[i+1]
		}
		return out
	}

	// Home videos can't be shared with someone who can't see them.
	var st together.State
	if code := c.do("POST", "/api/v1/together", with(host, "itemId", party), &st); code != http.StatusCreated {
		t.Fatalf("start: %d", code)
	}
	var peek struct {
		together.State
		CanWatch bool
	}
	friend.do("GET", "/api/v1/together/"+st.Code, nil, &peek)
	if peek.CanWatch {
		t.Fatal("friend can watch home videos")
	}
	if code := friend.do("POST", "/api/v1/together/"+st.Code+"/join", guest, nil); code != http.StatusForbidden {
		t.Fatalf("join home video: %d", code)
	}

	// A film they both see.
	if code := c.do("POST", "/api/v1/together", with(host, "itemId", film, "positionMs", 5000, "paused", true), &st); code != http.StatusCreated {
		t.Fatalf("start: %d", code)
	}
	if len(st.Code) != 6 || len(st.Members) != 1 || !st.Members[0].Host || st.Members[0].Name != "admin" {
		t.Fatalf("started = %+v", st)
	}
	var gst together.State
	if code := friend.do("POST", "/api/v1/together/"+st.Code+"/join", guest, &gst); code != http.StatusOK {
		t.Fatalf("join: %d", code)
	}
	if gst.ItemID != film || gst.PositionMs != 5000 || !gst.Paused || len(gst.Members) != 2 {
		t.Fatalf("joined = %+v", gst)
	}

	// The host hears the friend joined, then hears them press play.
	var hst together.State
	c.do("GET", "/api/v1/together/"+st.Code+"/state?client=host-client-1&since="+itoa(st.Version), nil, &hst)
	if hst.Last == nil || hst.Last.Action != together.ActionJoin || hst.Last.By != "sam" {
		t.Fatalf("host heard %+v", hst.Last)
	}
	done := make(chan together.State)
	go func() {
		var s together.State
		c.do("GET", "/api/v1/together/"+st.Code+"/state?client=host-client-1&since="+itoa(hst.Version), nil, &s)
		done <- s
	}()
	time.Sleep(100 * time.Millisecond)
	friend.do("POST", "/api/v1/together/"+st.Code+"/actions", with(guest, "action", "play", "positionMs", 6000), nil)
	select {
	case s := <-done:
		if s.Paused || s.PositionMs != 6000 || s.Last.Action != "play" || s.Last.By != "sam" {
			t.Fatalf("after play = %+v", s)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the host never heard play")
	}

	// Nobody can act for someone else's player.
	if code := c.do("POST", "/api/v1/together/"+st.Code+"/actions", with(guest, "action", "pause"), nil); code != http.StatusForbidden {
		t.Fatalf("act as someone else: %d", code)
	}
	// Switching to something the friend can't see isn't allowed for them…
	if code := friend.do("POST", "/api/v1/together/"+st.Code+"/actions", with(guest, "action", "item", "itemId", party), nil); code != http.StatusForbidden {
		t.Fatalf("friend switches to home video: %d", code)
	}
	// …but the next film is.
	if code := c.do("POST", "/api/v1/together/"+st.Code+"/actions", with(host, "action", "item", "itemId", sequel), &hst); code != http.StatusOK || hst.ItemID != sequel {
		t.Fatalf("next film: %d %+v", code, hst)
	}

	// The host leaves; the friend hosts. Then the friend ends it.
	c.do("DELETE", "/api/v1/together/"+st.Code+"/members/host-client-1", nil, nil)
	friend.do("GET", "/api/v1/together/"+st.Code+"/state?client=guest-client-1&wait=0", nil, &gst)
	if len(gst.Members) != 1 || !gst.Members[0].Host {
		t.Fatalf("after host left = %+v", gst.Members)
	}
	if code := friend.do("POST", "/api/v1/together/"+st.Code+"/end", guest, nil); code != http.StatusNoContent {
		t.Fatalf("end: %d", code)
	}
	if code := friend.do("GET", "/api/v1/together/"+st.Code, nil, nil); code != http.StatusNotFound {
		t.Fatalf("peek ended: %d", code)
	}
	// An ended session can't be started again under its code.
	if code := c.do("POST", "/api/v1/together", with(host, "itemId", film, "resume", st.Code), nil); code != http.StatusNotFound {
		t.Fatalf("resume ended: %d", code)
	}
}
