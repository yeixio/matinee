package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/yeixio/matinee/internal/store"
)

func TestFriendInvites(t *testing.T) {
	c, _, db := newTestServerStore(t)
	ctx := context.Background()
	var login struct{ Token string }
	c.do("POST", "/api/v1/setup", credentials{"admin", "correct horse"}, &login)
	c.token = login.Token

	movies, _ := db.CreateLibrary(ctx, "Movies", "movies", []string{"/m"})
	videos, _ := db.CreateLibrary(ctx, "Home videos", "movies", []string{"/v"})
	film, _ := db.UpsertFile(ctx, store.Item{LibraryID: movies.ID, Kind: "movie", Title: "Space Saga", Path: "/m/space.mp4"})
	party, _ := db.UpsertFile(ctx, store.Item{LibraryID: videos.ID, Kind: "movie", Title: "Birthday Party", Path: "/v/party.mp4"})

	// An invite needs a library.
	if code := c.do("POST", "/api/v1/invites", map[string]any{"name": "Sam"}, nil); code != http.StatusBadRequest {
		t.Fatalf("invite without libraries: %d", code)
	}
	var inv inviteView
	if code := c.do("POST", "/api/v1/invites", map[string]any{"name": "Sam", "libraries": []int64{movies.ID}}, &inv); code != http.StatusCreated {
		t.Fatalf("create invite: %d", code)
	}
	if len(inv.Code) != 16 || !strings.HasSuffix(inv.URL, "/join/"+inv.Code) || inv.State != "open" || inv.CreatedBy != "admin" {
		t.Fatalf("invite = %+v", inv)
	}

	// The invite page needs no sign-in, and shows only what's shared.
	anon := &client{t: t, base: c.base}
	var info struct {
		Server    string
		InvitedBy string
		Libraries []struct{ Name string }
	}
	if code := anon.do("GET", "/api/v1/join/"+strings.ToLower(inv.Code), nil, &info); code != http.StatusOK {
		t.Fatalf("join info: %d", code)
	}
	if info.InvitedBy != "admin" || len(info.Libraries) != 1 || info.Libraries[0].Name != "Movies" {
		t.Fatalf("join info = %+v", info)
	}
	if code := anon.do("GET", "/api/v1/join/NOPENOPENOPENOPE", nil, nil); code != http.StatusNotFound {
		t.Fatalf("bad code: %d", code)
	}

	// Names already taken are turned away; the invite still works after.
	if code := anon.do("POST", "/api/v1/join/"+inv.Code, map[string]string{"username": "Admin", "password": "password123"}, nil); code != http.StatusConflict {
		t.Fatalf("taken name: %d", code)
	}
	var joined struct {
		Token string
		User  store.User
	}
	if code := anon.do("POST", "/api/v1/join/"+inv.Code, map[string]string{"username": "sam", "password": "password123"}, &joined); code != http.StatusOK {
		t.Fatalf("accept: %d", code)
	}
	if joined.Token == "" || !joined.User.Friend {
		t.Fatalf("accepted = %+v", joined)
	}
	// Once only.
	if code := anon.do("POST", "/api/v1/join/"+inv.Code, map[string]string{"username": "sam2", "password": "password123"}, nil); code != http.StatusGone {
		t.Fatalf("second use: %d", code)
	}

	// The friend sees the movies but not the home videos.
	friend := &client{t: t, base: c.base, token: joined.Token}
	var libs []struct{ ID int64 }
	friend.do("GET", "/api/v1/libraries", nil, &libs)
	if len(libs) != 1 || libs[0].ID != movies.ID {
		t.Fatalf("friend libraries = %+v", libs)
	}
	if code := friend.do("GET", "/api/v1/items/"+itoa(film), nil, nil); code != http.StatusOK {
		t.Fatalf("shared film: %d", code)
	}
	if code := friend.do("GET", "/api/v1/items/"+itoa(party), nil, nil); code != http.StatusNotFound {
		t.Fatalf("home video: %d", code)
	}
	if code := friend.do("GET", "/api/v1/items/"+itoa(party)+"/file", nil, nil); code != http.StatusNotFound {
		t.Fatalf("home video file: %d", code)
	}
	var found []store.Item
	friend.do("GET", "/api/v1/search?q=party", nil, &found)
	if len(found) != 0 {
		t.Fatalf("search found %+v", found)
	}
	// Friends aren't admins.
	if code := friend.do("GET", "/api/v1/invites", nil, nil); code != http.StatusForbidden {
		t.Fatalf("friend lists invites: %d", code)
	}

	// The admin sees the friend and what they share, and can share more.
	var users []friendView
	c.do("GET", "/api/v1/users", nil, &users)
	var sam friendView
	for _, u := range users {
		if u.Username == "sam" {
			sam = u
		}
	}
	if !sam.Friend || len(sam.Libraries) != 1 || sam.Libraries[0] != movies.ID {
		t.Fatalf("sam = %+v", sam)
	}
	if code := c.do("PATCH", "/api/v1/users/"+itoa(sam.ID), map[string]any{"libraries": []int64{movies.ID, videos.ID}}, nil); code != http.StatusOK {
		t.Fatalf("share more: %d", code)
	}
	if code := friend.do("GET", "/api/v1/items/"+itoa(party), nil, nil); code != http.StatusOK {
		t.Fatalf("home video after sharing: %d", code)
	}
	// Friends can't be admins without becoming household first.
	if code := c.do("PATCH", "/api/v1/users/"+itoa(sam.ID), map[string]any{"isAdmin": true}, nil); code != http.StatusConflict {
		t.Fatalf("friend admin: %d", code)
	}
	if code := c.do("PATCH", "/api/v1/users/"+itoa(sam.ID), map[string]any{"friend": false}, nil); code != http.StatusOK {
		t.Fatalf("household: %d", code)
	}
	var me store.User
	friend.do("GET", "/api/v1/me", nil, &me)
	if me.Friend {
		t.Fatalf("still a friend: %+v", me)
	}
	// And back to a friend with one library.
	if code := c.do("PATCH", "/api/v1/users/"+itoa(sam.ID), map[string]any{"friend": true}, nil); code != http.StatusBadRequest {
		t.Fatalf("friend without libraries: %d", code)
	}
	if code := c.do("PATCH", "/api/v1/users/"+itoa(sam.ID), map[string]any{"friend": true, "libraries": []int64{videos.ID}}, nil); code != http.StatusOK {
		t.Fatalf("friend again: %d", code)
	}
	if code := friend.do("GET", "/api/v1/items/"+itoa(film), nil, nil); code != http.StatusNotFound {
		t.Fatalf("film after unsharing: %d", code)
	}

	// Taking an invite back.
	var other inviteView
	c.do("POST", "/api/v1/invites", map[string]any{"libraries": []int64{movies.ID}, "days": 1}, &other)
	if code := c.do("DELETE", "/api/v1/invites/"+other.Code, nil, nil); code != http.StatusNoContent {
		t.Fatalf("delete invite: %d", code)
	}
	if code := anon.do("GET", "/api/v1/join/"+other.Code, nil, nil); code != http.StatusNotFound {
		t.Fatalf("deleted invite: %d", code)
	}
	var list struct{ Invites []inviteView }
	c.do("GET", "/api/v1/invites", nil, &list)
	if len(list.Invites) != 1 || list.Invites[0].State != "used" || list.Invites[0].UsedBy != "sam" {
		t.Fatalf("invites = %+v", list)
	}
}
