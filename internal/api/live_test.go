package api

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/yeixio/matinee/internal/live"
	"github.com/yeixio/matinee/internal/scan"
	"github.com/yeixio/matinee/internal/store"
	"github.com/yeixio/matinee/internal/store/storetest"
)

func TestRemoteControl(t *testing.T) {
	db := storetest.Open(t)
	srv := &Server{Store: db, Scanner: &scan.Scanner{Store: db}, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	admin := &client{t: t, base: ts.URL}
	var login struct{ Token string }
	admin.do("POST", "/api/v1/setup", credentials{"admin", "correct horse"}, &login)
	admin.token = login.Token
	if code := admin.do("POST", "/api/v1/users", map[string]any{"username": "ellen", "password": "battery staple"}, nil); code != http.StatusCreated {
		t.Fatalf("create user: %d", code)
	}
	ellen := signIn(t, admin, "ellen", "battery staple", "Ellen's iPad")
	tv := &client{t: t, base: ts.URL} // at home, not signed in

	ctx := context.Background()
	lib, _ := db.CreateLibrary(ctx, "Movies", "movies", []string{"/m"})
	year, dur := 1988, int64(104*60_000)
	big, err := db.UpsertFile(ctx, store.Item{LibraryID: lib.ID, Kind: "movie", Title: "Big", Year: &year, DurationMs: &dur, Path: "/m/Big.mkv"})
	if err != nil {
		t.Fatal(err)
	}

	// Follow the feed the way Home Assistant does.
	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/events", nil)
	req.Header.Set("Authorization", "Bearer "+admin.token)
	res, err := http.DefaultClient.Do(req)
	if err != nil || res.StatusCode != 200 || res.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("events: %v %v", err, res)
	}
	defer res.Body.Close()
	feed := make(chan live.Event, 100)
	go func() {
		sc := bufio.NewScanner(res.Body)
		for sc.Scan() {
			if data, ok := strings.CutPrefix(sc.Text(), "data: "); ok {
				var e live.Event
				json.Unmarshal([]byte(data), &e)
				feed <- e
			}
		}
	}()
	next := func(typ string) live.Event {
		t.Helper()
		for {
			select {
			case e := <-feed:
				if e.Type == typ {
					return e
				}
			case <-time.After(5 * time.Second):
				t.Fatalf("no %s event", typ)
			}
		}
	}
	next("snapshot")
	if code := ellen.do("GET", "/api/v1/events", nil, nil); code != http.StatusForbidden {
		t.Fatalf("only admins follow everything, got %d", code)
	}

	// The TV says hello as the home guest.
	hello := map[string]any{"name": "Living room TV", "platform": "tvos", "commands": []string{"play", "pause", "seek", "playItem", "fly"}}
	if code := tv.do("PUT", "/api/v1/clients/short", hello, nil); code != http.StatusBadRequest {
		t.Fatalf("a client id is long enough to be unique, got %d", code)
	}
	if code := tv.do("PUT", "/api/v1/clients/tv-1234567", hello, nil); code != 200 {
		t.Fatalf("hello: %d", code)
	}
	if e := next(live.EventClientUpdated); e.Client.Name != "Living room TV" || !e.Client.Online || !e.Client.User.Guest ||
		strings.Join(e.Client.Commands, ",") != "play,pause,seek,playItem" {
		t.Fatalf("hello event: %+v", e.Client)
	}

	// Admins see every client; others only their own account's.
	var clients []live.Client
	if code := admin.do("GET", "/api/v1/clients", nil, &clients); code != 200 || len(clients) != 1 || clients[0].ID != "tv-1234567" {
		t.Fatalf("admin's clients: %d %+v", code, clients)
	}
	if code := ellen.do("GET", "/api/v1/clients", nil, &clients); code != 200 || len(clients) != 0 {
		t.Fatalf("ellen's clients: %d %+v", code, clients)
	}
	if code := ellen.do("POST", "/api/v1/clients/tv-1234567/commands", map[string]any{"type": "pause"}, nil); code != http.StatusNotFound {
		t.Fatalf("ellen can't control the TV, got %d", code)
	}
	if code := tv.do("GET", "/api/v1/clients", nil, nil); code != http.StatusUnauthorized {
		t.Fatalf("the guest can't list clients, got %d", code)
	}
	// Someone else can't take over the TV's id.
	if code := ellen.do("PUT", "/api/v1/clients/tv-1234567", hello, nil); code != http.StatusConflict {
		t.Fatalf("hijack: %d", code)
	}

	// Home Assistant starts a film on the TV.
	var sent live.Command
	if code := admin.do("POST", "/api/v1/clients/tv-1234567/commands", map[string]any{"type": "playItem", "itemId": big}, &sent); code != http.StatusAccepted ||
		sent.Item == nil || sent.Item.Title != "Big" || sent.From == "" {
		t.Fatalf("playItem: %d %+v", code, sent)
	}
	if code := admin.do("POST", "/api/v1/clients/tv-1234567/commands", map[string]any{"type": "volume", "volume": 0.5}, nil); code != http.StatusConflict {
		t.Fatalf("the TV has no volume, got %d", code)
	}
	if code := admin.do("POST", "/api/v1/clients/tv-1234567/commands", map[string]any{"type": "seek"}, nil); code != http.StatusBadRequest {
		t.Fatalf("seek needs a position, got %d", code)
	}
	if code := admin.do("POST", "/api/v1/clients/tv-1234567/commands", map[string]any{"type": "playItem", "itemId": 9999}, nil); code != http.StatusNotFound {
		t.Fatalf("playItem of nothing: %d", code)
	}
	// Playing a show plays the first episode not yet watched; specials come last.
	tvLib, _ := db.CreateLibrary(ctx, "Shows", "shows", []string{"/s"})
	show, _ := db.EnsureContainer(ctx, tvLib.ID, nil, "show", "Severance", nil, nil)
	zero, one, two := 0, 1, 2
	specials, _ := db.EnsureContainer(ctx, tvLib.ID, &show, "season", "Specials", &zero, nil)
	db.UpsertFile(ctx, store.Item{LibraryID: tvLib.ID, ParentID: &specials, Kind: "episode", Title: "Special", IndexNumber: &one, Path: "/s/s0e1.mkv"})
	s1, _ := db.EnsureContainer(ctx, tvLib.ID, &show, "season", "Season 1", &one, nil)
	e1, _ := db.UpsertFile(ctx, store.Item{LibraryID: tvLib.ID, ParentID: &s1, Kind: "episode", Title: "Good News About Hell", IndexNumber: &one, Path: "/s/e1.mkv"})
	e2, _ := db.UpsertFile(ctx, store.Item{LibraryID: tvLib.ID, ParentID: &s1, Kind: "episode", Title: "Half Loop", IndexNumber: &two, Path: "/s/e2.mkv"})
	guest, _ := db.HomeGuest(ctx)
	db.SetProgress(ctx, guest.ID, e1, 0, true)
	if code := admin.do("POST", "/api/v1/clients/tv-1234567/commands", map[string]any{"type": "playItem", "itemId": show}, &sent); code != http.StatusAccepted ||
		sent.ItemID != e2 || sent.Item.Title != "Half Loop" {
		t.Fatalf("play a show: %d %+v", code, sent)
	}

	var got struct{ Commands []live.Command }
	if code := tv.do("GET", "/api/v1/clients/tv-1234567/commands?wait=5", nil, &got); code != 200 || len(got.Commands) != 2 || got.Commands[0].ItemID != big || got.Commands[1].ItemID != e2 {
		t.Fatalf("commands: %d %+v", code, got)
	}

	// A command sent while the TV waits reaches it at once.
	done := make(chan []live.Command)
	go func() {
		var got struct{ Commands []live.Command }
		tv.do("GET", "/api/v1/clients/tv-1234567/commands?wait=10", nil, &got)
		done <- got.Commands
	}()
	time.Sleep(100 * time.Millisecond)
	start := time.Now()
	admin.do("POST", "/api/v1/clients/tv-1234567/commands", map[string]any{"type": "pause"}, nil)
	if cmds := <-done; len(cmds) != 1 || cmds[0].Type != "pause" || time.Since(start) > 3*time.Second {
		t.Fatalf("waiting TV got %+v after %v", cmds, time.Since(start))
	}

	// The TV reports what it plays; the feed says so.
	playing := func(pos int64, paused bool) map[string]any {
		return map[string]any{"name": "Living room TV", "platform": "tvos", "commands": []string{"play", "pause"},
			"playing": map[string]any{"itemId": big, "positionMs": pos, "paused": paused}}
	}
	tv.do("PUT", "/api/v1/clients/tv-1234567", playing(1000, false), nil)
	if e := next(live.EventPlaybackStarted); e.Item.Title != "Big" || e.User.Name == "" || e.Client.ID != "tv-1234567" {
		t.Fatalf("started: %+v", e)
	}
	tv.do("PUT", "/api/v1/clients/tv-1234567", playing(5000, true), nil)
	if e := next(live.EventPlaybackPaused); e.PositionMs != 5000 {
		t.Fatalf("paused: %+v", e)
	}
	var c live.Client
	if code := admin.do("GET", "/api/v1/clients/tv-1234567", nil, &c); code != 200 || c.Playing == nil || !c.Playing.Paused || c.Playing.DurationMs != dur {
		t.Fatalf("client: %d %+v", code, c)
	}
	tv.do("PUT", "/api/v1/clients/tv-1234567", playing(dur-60_000, false), nil)
	next(live.EventPlaybackResumed)
	tv.do("PUT", "/api/v1/clients/tv-1234567", hello, nil)
	if e := next(live.EventPlaybackStopped); !e.Finished {
		t.Fatalf("stopped near the end counts as finished: %+v", e)
	}

	// Closing goes offline; commands then fail.
	if code := tv.do("DELETE", "/api/v1/clients/tv-1234567", nil, nil); code != http.StatusNoContent {
		t.Fatalf("leave: %d", code)
	}
	if code := admin.do("POST", "/api/v1/clients/tv-1234567/commands", map[string]any{"type": "play"}, nil); code != http.StatusConflict {
		t.Fatalf("offline: %d", code)
	}
	if code := tv.do("GET", "/api/v1/clients/nobody-1234/commands", nil, nil); code != http.StatusNotFound {
		t.Fatalf("unknown client asks for commands: %d", code)
	}

	// Home Assistant's sensors count what each library has.
	var libs []struct {
		Name          string
		Titles, Files *int
	}
	if code := admin.do("GET", "/api/v1/libraries?counts=1", nil, &libs); code != 200 || len(libs) != 2 || libs[0].Name != "Movies" ||
		*libs[0].Titles != 1 || *libs[0].Files != 1 || *libs[1].Titles != 1 || *libs[1].Files != 3 {
		t.Fatalf("counts: %d %+v", code, libs)
	}
	libs = nil
	if ellen.do("GET", "/api/v1/libraries?counts=1", nil, &libs); libs[0].Titles != nil {
		t.Fatal("only admins get counts")
	}

	// Ellen's own iPad shows up for her, and she can control it.
	ellen.do("PUT", "/api/v1/clients/ipad-1234567", map[string]any{"name": "iPad", "platform": "ios", "commands": []string{"pause"}}, nil)
	if code := ellen.do("GET", "/api/v1/clients", nil, &clients); code != 200 || len(clients) != 1 {
		t.Fatalf("ellen's clients: %d %+v", code, clients)
	}
	if code := ellen.do("POST", "/api/v1/clients/ipad-1234567/commands", map[string]any{"type": "pause"}, &sent); code != http.StatusAccepted || sent.From != "Ellen's iPad" {
		t.Fatalf("ellen pauses her iPad: %d %+v", code, sent)
	}
}
