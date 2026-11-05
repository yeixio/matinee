package store

import (
	"context"
	"path/filepath"
	"testing"
)

// Notifications from before repeats were counted fold into one.
func TestGroupOldNotifications(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "matinee.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	u, err := s.CreateUser(ctx, "admin", "x", true)
	if err != nil {
		t.Fatal(err)
	}
	add := func(id, source, body string, at, read int64) {
		if _, err := s.db.ExecContext(ctx, `INSERT INTO notifications (id, user_id, source, event, level, title, body, created_at, read_at)
			VALUES (?, ?, ?, 'system.disk', 'warning', 'Disk space is low', ?, ?, ?)`, id, u.ID, source, body, at, read); err != nil {
			t.Fatal(err)
		}
	}
	add("a", "bo:1", "/downloads is 91% full.", 100, 1)
	add("b", "bo:2", "/downloads is 92% full.", 200, 0)
	add("c", "bo:3", "/downloads is 93% full.", 300, 1)
	add("d", "bo:4", "/movies is 93% full.", 250, 1)
	if err := s.SetSetting(ctx, "backfill:notifications", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.groupNotifications(ctx); err != nil {
		t.Fatal(err)
	}
	list, unread, err := s.Notifications(ctx, []int64{u.ID}, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || unread != 1 {
		t.Fatalf("grouped: %d unread %+v", unread, list)
	}
	if n := list[0]; n.ID != "c" || n.Repeats != 3 || n.FirstAt != 100 || n.Read || n.Body != "/downloads is 93% full." {
		t.Fatalf("downloads: %+v", n)
	}
	if n := list[1]; n.ID != "d" || n.Repeats != 0 {
		t.Fatalf("movies: %+v", n)
	}
	// What comes next adds to it.
	n, added, err := s.AddNotification(ctx, Notification{UserID: u.ID, Source: "bo:5", Event: "system.disk", Level: "warning",
		Title: "Disk space is low", Body: "/downloads is 94% full.", CreatedAt: 400})
	if err != nil || added != Repeated || n.ID != "c" || n.Repeats != 4 {
		t.Fatalf("added: %v %v %+v", err, added, n)
	}
	// And the same source again is left out.
	if _, added, _ := s.AddNotification(ctx, Notification{UserID: u.ID, Source: "bo:5", Event: "system.disk", Title: "Disk space is low"}); added != NotAdded {
		t.Fatalf("same source: %v", added)
	}
	// About a title, the wording has to match: episode 2 isn't episode 3.
	for _, body := range []string{"Episode 2 is ready.", "Episode 3 is ready."} {
		if _, added, _ := s.AddNotification(ctx, Notification{UserID: u.ID, Source: body, Event: "download.imported", Title: "Severance",
			Body: body, Kind: "show", TVDBID: 371980}); added != AddedNew {
			t.Fatalf("%s: %v", body, added)
		}
	}
}
