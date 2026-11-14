package store

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// moveDSN is a new, empty database of the kind: a SQLite file, or a fresh
// PostgreSQL database when MATINEE_TEST_DATABASE_URL is set (the test is
// skipped otherwise).
func moveDSN(t *testing.T, kind string) string {
	t.Helper()
	if kind == "sqlite" {
		return filepath.Join(t.TempDir(), "matinee.db")
	}
	admin := os.Getenv("MATINEE_TEST_DATABASE_URL")
	if admin == "" {
		t.Skip("MATINEE_TEST_DATABASE_URL isn't set")
	}
	b := make([]byte, 6)
	rand.Read(b)
	name := "matinee_move_" + hex.EncodeToString(b)
	conn, err := sql.Open("pgx", admin)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec("CREATE DATABASE " + name); err != nil {
		t.Fatalf("create test database: %v", err)
	}
	u, _ := url.Parse(admin)
	u.Path = "/" + name
	t.Cleanup(func() {
		conn.Exec("DROP DATABASE IF EXISTS " + name + " WITH (FORCE)")
		conn.Close()
	})
	return u.String()
}

// fill puts a little of everything a move has to get right in s: rows that
// refer to others, a child row made before its parent, profiles (users
// that belong to users), bytes, and settings that stay behind.
func fill(t *testing.T, s *Store) (userID, episodeID int64) {
	t.Helper()
	ctx := context.Background()
	u, err := s.CreateUser(ctx, "mike", "hash", true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateProfile(ctx, u.ID, ProfileChanges{Name: ptr("Kids")}); err != nil {
		t.Fatal(err)
	}
	lib, err := s.CreateLibrary(ctx, "TV", "shows", []string{"/media/tv"})
	if err != nil {
		t.Fatal(err)
	}
	// The episode is made first and the season and show after it, so its
	// id is lower than its parents'.
	ep, err := s.UpsertFile(ctx, Item{LibraryID: lib.ID, Kind: "episode", Title: "Pilot", Path: "/media/tv/Show/S01E01.mkv"})
	if err != nil {
		t.Fatal(err)
	}
	show, err := s.EnsureContainer(ctx, lib.ID, nil, "show", "Moving Day", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	one := 1
	season, err := s.EnsureContainer(ctx, lib.ID, &show, "season", "Season 1", &one, nil)
	if err != nil {
		t.Fatal(err)
	}
	// A second show with a season of the same name: rows that only differ
	// by their parent.
	other, _ := s.EnsureContainer(ctx, lib.ID, nil, "show", "Other Show", nil, nil)
	if _, err := s.EnsureContainer(ctx, lib.ID, &other, "season", "Season 1", &one, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, "UPDATE items SET parent_id = ? WHERE id = ?", season, ep); err != nil {
		t.Fatal(err)
	}
	// Versions and parts refer to another file of the same table too, made
	// after them here: a 4K version and a second part before the file they
	// belong to, and a file that names itself.
	var files []int64
	for _, name := range []string{"4k", "part2", "main", "self"} {
		id, err := s.UpsertFile(ctx, Item{LibraryID: lib.ID, Kind: "movie", Title: "Film " + name, Path: "/media/movies/" + name + ".mkv"})
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, id)
	}
	for _, q := range []struct {
		sql      string
		row, ref int64
	}{
		{"UPDATE items SET version_of = ? WHERE id = ?", files[0], files[2]},
		{"UPDATE items SET file_part_of = ? WHERE id = ?", files[1], files[2]},
		{"UPDATE items SET version_of = ? WHERE id = ?", files[3], files[3]},
	} {
		if _, err := s.db.ExecContext(ctx, q.sql, q.ref, q.row); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SetProgress(ctx, u.ID, ep, 60000, false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, "INSERT INTO plugin_storage (plugin_id, user_id, key, value, updated_at) VALUES (?, ?, ?, ?, ?)",
		"arcade", u.ID, "save", []byte{0, 1, 2, 255}, 1); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSetting(ctx, "time_zone", "America/Anchorage"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSetting(ctx, SettingMoveError, "an older try failed"); err != nil {
		t.Fatal(err)
	}
	return u.ID, ep
}

func ptr[T any](v T) *T { return &v }

func TestMove(t *testing.T) {
	moveBatch = 1
	t.Cleanup(func() { moveBatch = 500 })
	for _, c := range []struct{ from, to string }{
		{"sqlite", "sqlite"},
		{"sqlite", "postgres"},
		{"postgres", "postgres"},
		{"postgres", "sqlite"},
	} {
		t.Run(c.from+" to "+c.to, func(t *testing.T) {
			ctx := context.Background()
			fromDSN, toDSN := moveDSN(t, c.from), moveDSN(t, c.to)
			src, err := Open(fromDSN)
			if err != nil {
				t.Fatal(err)
			}
			defer src.Close()
			userID, ep := fill(t, src)
			if err := src.CheckMoveTarget(ctx, toDSN); err != nil {
				t.Fatalf("check: %v", err)
			}
			if c.to == "sqlite" {
				if _, err := os.Stat(toDSN); !errors.Is(err, os.ErrNotExist) {
					t.Errorf("checking left a file behind: %v", err)
				}
			}
			var calls int
			report, err := src.MoveTo(ctx, toDSN, func(string, int64, int64) { calls++ })
			if err != nil {
				t.Fatalf("move: %v", err)
			}
			if report.Rows == 0 || calls == 0 {
				t.Errorf("report = %+v after %d progress calls", report, calls)
			}
			if err := src.FinishMove(ctx, fromDSN, toDSN, report); err != nil {
				t.Fatal(err)
			}
			src.Close()

			if got, err := Follow(ctx, fromDSN, Options{}); err != nil || got != toDSN {
				t.Fatalf("follow = %q, %v", got, err)
			}
			dst, err := Open(toDSN)
			if err != nil {
				t.Fatal(err)
			}
			defer dst.Close()
			if u, err := dst.UserByName(ctx, "mike"); err != nil || u.ID != userID {
				t.Errorf("user = %+v, %v", u, err)
			}
			it, err := dst.Item(ctx, userID, ep)
			if err != nil {
				t.Fatal(err)
			}
			if it.PositionMs != 60000 || it.ParentID == nil {
				t.Errorf("episode = %+v", it)
			}
			var b []byte
			if err := dst.db.QueryRowContext(ctx, "SELECT value FROM plugin_storage WHERE key = 'save'").Scan(&b); err != nil || !bytes.Equal(b, []byte{0, 1, 2, 255}) {
				t.Errorf("bytes = %v, %v", b, err)
			}
			if v, _ := dst.Setting(ctx, "time_zone"); v != "America/Anchorage" {
				t.Errorf("time zone = %q", v)
			}
			for _, k := range []string{SettingMoveError, SettingMovedTo} {
				if v, _ := dst.Setting(ctx, k); v != "" {
					t.Errorf("%s came along: %q", k, v)
				}
			}
			if v, _ := dst.Setting(ctx, SettingMovedFrom); !strings.Contains(v, `"rows"`) {
				t.Errorf("moved from = %q", v)
			}
			// New rows carry on numbering after the copied ones.
			u2, err := dst.CreateUser(ctx, "ellen", "hash", false)
			if err != nil {
				t.Fatalf("new user: %v", err)
			}
			if u2.ID <= userID {
				t.Errorf("new user got id %d, not after %d", u2.ID, userID)
			}
			// Search works, whichever way the new database finds titles.
			found, err := dst.Search(ctx, userID, "moving", 10)
			if err != nil || len(found) == 0 {
				t.Errorf("search = %v, %v", found, err)
			}

			// The old database still has everything, and one more move to
			// the same place finds it isn't empty.
			old, err := Open(fromDSN)
			if err != nil {
				t.Fatal(err)
			}
			defer old.Close()
			if _, err := old.UserByName(ctx, "mike"); err != nil {
				t.Errorf("old database lost its user: %v", err)
			}
			if err := old.CheckMoveTarget(ctx, toDSN); !errors.Is(err, ErrNotEmpty) {
				t.Errorf("moving onto the copy: %v", err)
			}
			if err := UndoMove(ctx, fromDSN); err != nil {
				t.Fatal(err)
			}
			if got, _ := Follow(ctx, fromDSN, Options{}); got != fromDSN {
				t.Errorf("after undo, follow = %q", got)
			}
		})
	}
}

// A move to the database in use, or to a folder, is refused without
// changing anything.
func TestMoveRefuses(t *testing.T) {
	ctx := context.Background()
	path := moveDSN(t, "sqlite")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	fill(t, s)
	if err := s.CheckMoveTarget(ctx, path); err == nil || !strings.Contains(err.Error(), "using now") {
		t.Errorf("same database: %v", err)
	}
	if err := s.CheckMoveTarget(ctx, t.TempDir()); err == nil || !strings.Contains(err.Error(), "folder") {
		t.Errorf("folder: %v", err)
	}
	if err := s.CheckMoveTarget(ctx, "matinee.db"); err == nil {
		t.Error("a relative path was taken")
	}
	if err := s.CheckMoveTarget(ctx, "postgres://matinee:x@127.0.0.1:1/matinee?connect_timeout=1"); err == nil || !strings.Contains(err.Error(), "reach") {
		t.Errorf("unreachable: %v", err)
	}
}

// A failed copy leaves no file behind and the new database empty.
func TestMoveFailureLeavesNothing(t *testing.T) {
	ctx := context.Background()
	s, err := Open(moveDSN(t, "sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	fill(t, s)
	to := moveDSN(t, "sqlite")
	cctx, cancel := context.WithCancel(ctx)
	_, err = s.MoveTo(cctx, to, func(string, int64, int64) { cancel() })
	if err == nil {
		t.Fatal("a cancelled move worked")
	}
	if _, err := os.Stat(to); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the failed move left %s: %v", to, err)
	}
}

func TestDescribe(t *testing.T) {
	if got := Describe("postgres://matinee:secret@tower:5432/matinee?sslmode=disable"); strings.Contains(got, "secret") || !strings.Contains(got, "tower:5432/matinee") {
		t.Errorf("describe = %q", got)
	}
	if got := Describe("/data/matinee.db"); got != "/data/matinee.db" {
		t.Errorf("describe = %q", got)
	}
}
