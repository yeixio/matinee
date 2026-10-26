package store

import (
	"context"
	"path/filepath"
	"testing"
)

// A database made by the first release picks up later schema changes on open.
func TestMigrateFromVersion1(t *testing.T) {
	path := filepath.Join(t.TempDir(), "matinee.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, q := range []string{
		"ALTER TABLE items DROP COLUMN subtitle_streams",
		"ALTER TABLE items DROP COLUMN mbid",
		"ALTER TABLE sessions DROP COLUMN device_name",
		"ALTER TABLE sessions DROP COLUMN platform",
		"ALTER TABLE sessions DROP COLUMN created_at",
		"ALTER TABLE sessions DROP COLUMN last_seen_at",
		"DROP TABLE device_codes",
		"DROP TABLE downloads",
		"DROP TABLE settings",
		"ALTER TABLE users DROP COLUMN guest",
		"DROP TABLE watchlist",
		"ALTER TABLE progress DROP COLUMN hidden",
		"DROP TABLE requests",
		"DROP TABLE issues",
		"DROP INDEX items_tmdb",
		"ALTER TABLE items DROP COLUMN media_info",
		"ALTER TABLE items DROP COLUMN media_tags",
		"ALTER TABLE items DROP COLUMN media_key",
		"UPDATE schema_version SET version = 1",
	} {
		if _, err := s.db.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	s.Close()

	s, err = Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	var version int
	s.db.QueryRowContext(ctx, "SELECT version FROM schema_version").Scan(&version)
	if version != len(migrations)+1 {
		t.Fatalf("version %d, want %d", version, len(migrations)+1)
	}
	if _, _, err := s.SubtitleStreams(ctx, 1); err != ErrNotFound {
		t.Fatalf("subtitle_streams column missing: %v", err)
	}
	s.Close()
	if s, err = Open(path); err != nil {
		t.Fatalf("reopening an up-to-date database: %v", err)
	}
	s.Close()
}

// The home network's guest account is made once, can't sign in, and isn't
// counted or listed as a person.
func TestHomeGuest(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "matinee.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	if _, err := s.CreateUser(ctx, "mike", "hash", true); err != nil {
		t.Fatal(err)
	}
	g, err := s.HomeGuest(ctx)
	if err != nil || !g.Guest || g.Username != "Home" || g.IsAdmin {
		t.Fatalf("guest = %+v, %v", g, err)
	}
	again, _ := s.HomeGuest(ctx)
	if again.ID != g.ID {
		t.Fatalf("second guest %d, want %d", again.ID, g.ID)
	}
	if n, _ := s.CountUsers(ctx); n != 1 {
		t.Fatalf("CountUsers = %d, want 1", n)
	}
	if users, _ := s.Users(ctx); len(users) != 1 {
		t.Fatalf("Users = %+v", users)
	}
	if v, _ := s.Setting(ctx, "x"); v != "" {
		t.Fatalf("unset setting = %q", v)
	}
	s.SetSetting(ctx, "x", "1")
	s.SetSetting(ctx, "x", "2")
	if v, _ := s.Setting(ctx, "x"); v != "2" {
		t.Fatalf("setting = %q, want 2", v)
	}
}
