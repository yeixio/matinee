package api

import (
	"testing"

	"github.com/yeixio/matinee/internal/store"
)

// tryLogin signs a user in as a new client of the same server, and says how it went.
func tryLogin(t *testing.T, c *client, username, password string) (*client, int) {
	t.Helper()
	var login struct {
		User  store.User `json:"user"`
		Token string     `json:"token"`
	}
	code := c.do("POST", "/api/v1/auth/login", credentials{username, password}, &login)
	return &client{t: t, base: c.base, token: login.Token, srv: c.srv}, code
}

func TestChangeOwnPassword(t *testing.T) {
	admin, _ := newTestServer(t)
	admin.do("POST", "/api/v1/setup", credentials{"admin", "correct horse"}, nil)
	admin, _ = tryLogin(t, admin, "admin", "correct horse")
	var bob store.User
	admin.do("POST", "/api/v1/users", map[string]any{"username": "bob", "password": "bob password"}, &bob)
	phone, _ := tryLogin(t, admin, "bob", "bob password")
	laptop, _ := tryLogin(t, admin, "bob", "bob password")

	change := func(current, next string) int {
		return laptop.do("PUT", "/api/v1/me/password", map[string]string{"current": current, "new": next}, nil)
	}
	if code := change("not it", "new password"); code != 403 {
		t.Fatalf("wrong current password: %d, want 403", code)
	}
	if code := change("bob password", "short"); code != 400 {
		t.Fatalf("too short: %d, want 400", code)
	}
	if code := change("bob password", "new password"); code != 204 {
		t.Fatalf("change: %d", code)
	}
	// The other device is signed out; this one isn't.
	if code := phone.do("GET", "/api/v1/me", nil, nil); code != 401 {
		t.Fatalf("other session after change: %d, want 401", code)
	}
	if code := laptop.do("GET", "/api/v1/me", nil, nil); code != 200 {
		t.Fatalf("this session after change: %d, want 200", code)
	}
	if _, code := tryLogin(t, admin, "bob", "bob password"); code != 401 {
		t.Fatalf("old password: %d, want 401", code)
	}
	if _, code := tryLogin(t, admin, "bob", "new password"); code != 200 {
		t.Fatalf("new password: %d, want 200", code)
	}
	// Without signing in there is no password to change.
	anon := &client{t: t, base: admin.base, srv: admin.srv}
	if code := anon.do("PUT", "/api/v1/me/password", map[string]string{"current": "x", "new": "new password"}, nil); code != 401 {
		t.Fatalf("signed out: %d, want 401", code)
	}
}

func TestAdminManagesUsers(t *testing.T) {
	admin, _ := newTestServer(t)
	admin.do("POST", "/api/v1/setup", credentials{"admin", "correct horse"}, nil)
	admin, _ = tryLogin(t, admin, "admin", "correct horse")
	var me, bob store.User
	admin.do("GET", "/api/v1/me", nil, &me)
	admin.do("POST", "/api/v1/users", map[string]any{"username": "bob", "password": "bob password"}, &bob)
	phone, _ := tryLogin(t, admin, "bob", "bob password")
	bobID := "/api/v1/users/" + itoa(bob.ID)

	// Not for people who aren't admins.
	for _, r := range []struct{ method, path string }{
		{"PUT", "/api/v1/users/" + itoa(me.ID) + "/password"},
		{"PATCH", "/api/v1/users/" + itoa(me.ID)},
		{"DELETE", "/api/v1/users/" + itoa(me.ID)},
	} {
		if code := phone.do(r.method, r.path, map[string]any{"password": "taken over", "isAdmin": false}, nil); code != 403 {
			t.Fatalf("%s %s as non-admin: %d, want 403", r.method, r.path, code)
		}
	}

	// Reset: no current password needed, and bob is signed out everywhere.
	if code := admin.do("PUT", bobID+"/password", map[string]string{"password": "short"}, nil); code != 400 {
		t.Fatalf("short reset: %d, want 400", code)
	}
	if code := admin.do("PUT", bobID+"/password", map[string]string{"password": "fresh start"}, nil); code != 204 {
		t.Fatalf("reset: %d", code)
	}
	if code := phone.do("GET", "/api/v1/me", nil, nil); code != 401 {
		t.Fatalf("bob after reset: %d, want 401", code)
	}
	phone, code := tryLogin(t, admin, "bob", "fresh start")
	if code != 200 {
		t.Fatalf("bob with reset password: %d", code)
	}
	if code := admin.do("PUT", "/api/v1/users/999/password", map[string]string{"password": "fresh start"}, nil); code != 404 {
		t.Fatalf("reset unknown user: %d, want 404", code)
	}

	// The last admin can't be demoted or removed, and nobody removes themselves.
	if code := admin.do("PATCH", "/api/v1/users/"+itoa(me.ID), map[string]bool{"isAdmin": false}, nil); code != 409 {
		t.Fatalf("demote last admin: %d, want 409", code)
	}
	if code := admin.do("DELETE", "/api/v1/users/"+itoa(me.ID), nil, nil); code != 400 {
		t.Fatalf("remove self: %d, want 400", code)
	}

	// With a second admin, the first can step down, and then bob (now the
	// only admin) can't be.
	var got store.User
	if code := admin.do("PATCH", bobID, map[string]bool{"isAdmin": true}, &got); code != 200 || !got.IsAdmin {
		t.Fatalf("promote bob: %d %+v", code, got)
	}
	if code := admin.do("PATCH", "/api/v1/users/"+itoa(me.ID), map[string]bool{"isAdmin": false}, nil); code != 200 {
		t.Fatalf("demote with another admin: %d", code)
	}
	if code := phone.do("PATCH", bobID, map[string]bool{"isAdmin": false}, nil); code != 409 {
		t.Fatalf("demote the new last admin: %d, want 409", code)
	}
	if code := phone.do("DELETE", "/api/v1/users/"+itoa(me.ID), nil, nil); code != 204 {
		t.Fatalf("remove former admin: %d", code)
	}

	// Removing takes sessions and watch history with it.
	if code := admin.do("GET", "/api/v1/me", nil, nil); code != 401 {
		t.Fatalf("removed user's session: %d, want 401", code)
	}
	var users []store.User
	phone.do("GET", "/api/v1/users", nil, &users)
	if len(users) != 1 || users[0].ID != bob.ID {
		t.Fatalf("users after remove: %+v", users)
	}
	if code := phone.do("DELETE", "/api/v1/users/"+itoa(me.ID), nil, nil); code != 404 {
		t.Fatalf("remove twice: %d, want 404", code)
	}
}

func TestDeleteUserTakesTheirData(t *testing.T) {
	admin, _, db := newTestServerStore(t)
	admin.do("POST", "/api/v1/setup", credentials{"admin", "correct horse"}, nil)
	admin, _ = tryLogin(t, admin, "admin", "correct horse")
	var bob store.User
	admin.do("POST", "/api/v1/users", map[string]any{"username": "bob", "password": "bob password"}, &bob)
	ctx := t.Context()
	lib, err := db.CreateLibrary(ctx, "Movies", "movies", []string{"/m"})
	if err != nil {
		t.Fatal(err)
	}
	item, err := db.UpsertFile(ctx, store.Item{LibraryID: lib.ID, Kind: "movie", Title: "Heat", Path: "/m/heat.mkv"})
	if err != nil {
		t.Fatal(err)
	}
	// Foreign keys are on, so the remove below only works if watch history
	// goes with the account.
	if err := db.SetProgress(ctx, bob.ID, item, 60_000, false); err != nil {
		t.Fatal(err)
	}
	if err := db.SetSetting(ctx, userThemeKey(bob.ID), "light"); err != nil {
		t.Fatal(err)
	}
	if code := admin.do("DELETE", "/api/v1/users/"+itoa(bob.ID), nil, nil); code != 204 {
		t.Fatalf("remove bob: %d", code)
	}
	if v, _ := db.Setting(ctx, userThemeKey(bob.ID)); v != "" {
		t.Fatalf("bob's theme survived: %q", v)
	}
	// Someone new with bob's old name starts fresh.
	if code := admin.do("POST", "/api/v1/users", map[string]any{"username": "bob", "password": "bob password"}, nil); code != 201 {
		t.Fatalf("reuse username: %d", code)
	}
}
