package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
)

// UpdateLibrary renames a library and sets its folders. Files under a
// folder that was taken out go on the library's next full scan; files
// under one that was added come in with it.
func (s *Store) UpdateLibrary(ctx context.Context, id int64, name string, paths []string) (Library, error) {
	t, err := s.db.BeginTx(ctx)
	if err != nil {
		return Library{}, err
	}
	defer t.Rollback()
	res, err := t.ExecContext(ctx, "UPDATE libraries SET name = ? WHERE id = ?", name, id)
	if err != nil {
		return Library{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return Library{}, ErrNotFound
	}
	if _, err := t.ExecContext(ctx, "DELETE FROM library_paths WHERE library_id = ?", id); err != nil {
		return Library{}, err
	}
	for _, p := range paths {
		if _, err := t.ExecContext(ctx, "INSERT INTO library_paths (library_id, path) VALUES (?, ?) ON CONFLICT DO NOTHING", id, p); err != nil {
			return Library{}, err
		}
	}
	if err := t.Commit(); err != nil {
		return Library{}, err
	}
	return s.Library(ctx, id)
}

// FolderOwner finds the library, other than except, whose folders take in
// path or sit inside it. One file in two libraries would flip between them
// on every scan, so a folder belongs to one library only. It returns false
// when no other library has it.
func FolderOwner(libs []Library, except int64, path string) (Library, bool) {
	for _, l := range libs {
		if l.ID == except {
			continue
		}
		for _, p := range l.Paths {
			if within(path, p) || within(p, path) {
				return l, true
			}
		}
	}
	return Library{}, false
}

// within reports whether path is dir or inside it.
func within(path, dir string) bool {
	path, dir = filepath.Clean(path), filepath.Clean(dir)
	if path == dir {
		return true
	}
	if !strings.HasSuffix(dir, string(filepath.Separator)) {
		dir += string(filepath.Separator)
	}
	return strings.HasPrefix(path, dir)
}

// MergeLibraries makes two libraries of the same kind one: from's folders
// and titles join into, and from goes. A show, season, artist or album
// both have becomes one, keeping into's, with from's episodes and tracks
// under it and anyone's watch list entry, rating or collection place for
// it. Files keep their items, so what people watched and how far they got
// stays. Profiles and friends that could see either library see the
// merged one.
func (s *Store) MergeLibraries(ctx context.Context, into, from int64) error {
	if into == from {
		return errors.New("a library can't be combined with itself")
	}
	t, err := s.db.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer t.Rollback()
	var intoKind, fromKind string
	if err := t.QueryRowContext(ctx, "SELECT kind FROM libraries WHERE id = ?", into).Scan(&intoKind); errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	if err := t.QueryRowContext(ctx, "SELECT kind FROM libraries WHERE id = ?", from).Scan(&fromKind); errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	if intoKind != fromKind {
		return ErrKindMismatch
	}
	if _, err := t.ExecContext(ctx, `INSERT INTO library_paths (library_id, path)
		SELECT CAST(? AS BIGINT), path FROM library_paths WHERE library_id = ? ON CONFLICT DO NOTHING`, into, from); err != nil {
		return err
	}

	// Containers, top down: each round takes those whose parent is already
	// in into (or that have none), so a show is placed before its seasons.
	type container struct {
		id, parent, tmdb int64
		kind, title      string
		index, year      sql.NullInt64
	}
	for {
		rows, err := t.QueryContext(ctx, `SELECT c.id, COALESCE(c.parent_id, 0), c.kind, c.title, c.index_number, c.year, COALESCE(c.tmdb_id, 0)
			FROM items c WHERE c.library_id = ? AND c.path IS NULL
			  AND (c.parent_id IS NULL OR c.parent_id IN (SELECT id FROM items WHERE library_id = ?))
			ORDER BY c.id`, from, into)
		if err != nil {
			return err
		}
		var round []container
		for rows.Next() {
			var c container
			if err := rows.Scan(&c.id, &c.parent, &c.kind, &c.title, &c.index, &c.year, &c.tmdb); err != nil {
				rows.Close()
				return err
			}
			round = append(round, c)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		if len(round) == 0 {
			break
		}
		for _, c := range round {
			match, err := sameContainer(ctx, t, into, c.kind, c.parent, c.title, c.index, c.year, c.tmdb)
			if err != nil {
				return err
			}
			if match == 0 {
				if _, err := t.ExecContext(ctx, "UPDATE items SET library_id = ? WHERE id = ?", into, c.id); err != nil {
					return err
				}
				continue
			}
			if err := foldItem(ctx, t, c.id, match); err != nil {
				return err
			}
		}
	}
	// Files and anything else left.
	if _, err := t.ExecContext(ctx, "UPDATE items SET library_id = ? WHERE library_id = ?", into, from); err != nil {
		return err
	}
	for _, q := range []string{
		`INSERT INTO profile_libraries (user_id, library_id, mode)
			SELECT user_id, CAST(? AS BIGINT), mode FROM profile_libraries WHERE library_id = ? ON CONFLICT DO NOTHING`,
		`INSERT INTO friend_libraries (user_id, library_id)
			SELECT user_id, CAST(? AS BIGINT) FROM friend_libraries WHERE library_id = ? ON CONFLICT DO NOTHING`,
		"UPDATE collections SET pin_library = ? WHERE pin_library = ?",
	} {
		if _, err := t.ExecContext(ctx, q, into, from); err != nil {
			return err
		}
	}
	if _, err := t.ExecContext(ctx, "DELETE FROM libraries WHERE id = ?", from); err != nil {
		return err
	}
	return t.Commit()
}

// ErrKindMismatch is returned when libraries of different kinds are
// combined, such as Movies and TV.
var ErrKindMismatch = errors.New("only libraries of the same type can be combined")

// sameContainer finds into's container that is the same as one of the
// library being merged in: same kind, parent, title and number. Shows of
// the same name are the same show unless their TMDB ids or years differ,
// as EnsureShow tells them apart.
func sameContainer(ctx context.Context, t *tx, into int64, kind string, parent int64, title string, index, year sql.NullInt64, tmdb int64) (int64, error) {
	idx := int64(-1)
	if index.Valid {
		idx = index.Int64
	}
	rows, err := t.QueryContext(ctx, `SELECT id, COALESCE(year, 0), COALESCE(tmdb_id, 0) FROM items
		WHERE library_id = ? AND path IS NULL AND kind = ? AND COALESCE(parent_id, 0) = ? AND title = ?
		  AND COALESCE(index_number, -1) = ?
		ORDER BY id`, into, kind, parent, title, idx)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, y, tm int64
		if err := rows.Scan(&id, &y, &tm); err != nil {
			return 0, err
		}
		if kind == "show" {
			if tmdb != 0 && tm != 0 && tmdb != tm {
				continue
			}
			if !(tmdb != 0 && tmdb == tm) && year.Valid && year.Int64 != 0 && y != 0 && year.Int64 != y {
				continue
			}
		}
		return id, nil
	}
	return 0, rows.Err()
}

// foldItem makes the container from one with into: its children move
// under into, people's watch list entries, ratings, follows and collection
// places move to it where into doesn't have them already, and from goes.
func foldItem(ctx context.Context, t *tx, from, into int64) error {
	for _, q := range []string{
		"UPDATE items SET parent_id = ? WHERE parent_id = ?",
		"UPDATE items SET version_of = ? WHERE version_of = ?",
		`UPDATE watchlist SET item_id = ? WHERE item_id = ? AND NOT EXISTS (
			SELECT 1 FROM watchlist o WHERE o.item_id = ? AND o.user_id = watchlist.user_id)`,
		`UPDATE ratings SET item_id = ? WHERE item_id = ? AND NOT EXISTS (
			SELECT 1 FROM ratings o WHERE o.item_id = ? AND o.user_id = ratings.user_id)`,
		`UPDATE podcast_follows SET item_id = ? WHERE item_id = ? AND NOT EXISTS (
			SELECT 1 FROM podcast_follows o WHERE o.item_id = ? AND o.user_id = podcast_follows.user_id)`,
		`UPDATE progress SET item_id = ? WHERE item_id = ? AND NOT EXISTS (
			SELECT 1 FROM progress o WHERE o.item_id = ? AND o.user_id = progress.user_id)`,
		`UPDATE collection_items SET item_id = ? WHERE item_id = ? AND NOT EXISTS (
			SELECT 1 FROM collection_items o WHERE o.item_id = ? AND o.collection_id = collection_items.collection_id)`,
	} {
		args := []any{into, from}
		if strings.Contains(q, "NOT EXISTS") {
			args = append(args, into)
		}
		if _, err := t.ExecContext(ctx, q, args...); err != nil {
			return err
		}
	}
	_, err := t.ExecContext(ctx, "DELETE FROM items WHERE id = ?", from)
	return err
}
