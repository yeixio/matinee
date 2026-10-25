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
