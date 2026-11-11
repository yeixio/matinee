package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// Share stalls add up for one version of a file, and start over for a new
// one, after a long quiet spell, or with ClearShareStalls.
func TestShareStalls(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "matinee.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	lib, err := s.CreateLibrary(ctx, "Movies", "movies", []string{t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	var id int64
	if err := s.db.QueryRowContext(ctx, `INSERT INTO items (library_id, kind, title, sort_title, added_at)
		VALUES (?, 'movie', 'Milk', 'milk', 1) RETURNING id`, lib.ID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	add := func(key string, want int) {
		t.Helper()
		if n, err := s.AddShareStall(ctx, id, key, time.Hour); err != nil || n != want {
			t.Fatalf("%s: %d %v, want %d", key, n, err, want)
		}
	}
	add("a", 1)
	add("a", 2)
	add("b", 1) // the file changed
	add("b", 2)
	s.db.ExecContext(ctx, "UPDATE share_stalls SET last_at = last_at - 7200")
	add("b", 1) // the last one was too long ago
	s.ClearShareStalls(ctx, id)
	add("b", 1)
}
