package store

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

// A file that's replaced by a better copy, renamed or moved looks to a scan
// like one file gone and another added. Deleting the old item would take
// everything tied to it along (watched marks, resume points, ratings, watch
// lists, collections, downloads, found subtitles), so before a missing file
// is removed it's matched with the file that took its place: the same movie,
// the same episode of the same show, the same song on the same album. The
// old item then takes the new file and keeps its id; see MoveFile.

// LibraryFile is a file a library lists, with what it's matched on.
type LibraryFile struct {
	ID   int64
	Path string
	Kind string
	// Key is what the file is (see fileKey); "" when it can't be matched.
	Key    string
	tmdbID int64
	// VersionOf is the item this file is another version of, or 0.
	VersionOf int64
	edition   string
}

// LibraryFiles lists a library's files with their match keys.
func (s *Store) LibraryFiles(ctx context.Context, libraryID int64) ([]LibraryFile, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT i.id, i.path, i.kind, i.title, COALESCE(i.year, 0), i.index_number, COALESCE(i.disc_number, 0),
			COALESCE(i.parent_id, 0), COALESCE(i.tmdb_id, 0), COALESCE(se.parent_id, 0), se.index_number,
			COALESCE(i.version_of, 0), COALESCE(i.edition, '')
		FROM items i LEFT JOIN items se ON i.kind = 'episode' AND se.id = i.parent_id
		WHERE i.library_id = ? AND i.path IS NOT NULL`, libraryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LibraryFile
	for rows.Next() {
		var f LibraryFile
		var title string
		var year, disc int
		var parent, showID int64
		var index, season sql.NullInt64
		if err := rows.Scan(&f.ID, &f.Path, &f.Kind, &title, &year, &index, &disc, &parent, &f.tmdbID, &showID, &season, &f.VersionOf, &f.edition); err != nil {
			return nil, err
		}
		f.Key = fileKey(f.Kind, title, year, index, disc, parent, showID, season)
		out = append(out, f)
	}
	return out, rows.Err()
}

// fileKey says what a file is, so a copy of the same movie, episode or song
// under another name can be told apart from a different one.
func fileKey(kind, title string, year int, index sql.NullInt64, disc int, parent, showID int64, season sql.NullInt64) string {
	t := matchTitle(title)
	n := func(v int64) string { return strconv.FormatInt(v, 10) }
	switch kind {
	case "movie":
		if t == "" {
			return ""
		}
		return "movie|" + t + "|" + strconv.Itoa(year)
	case "episode":
		// The title is the file's own and often changes with it, so it's left out.
		if !index.Valid || !season.Valid || showID == 0 {
			return ""
		}
		return "episode|" + n(showID) + "|" + n(season.Int64) + "|" + n(index.Int64)
	default: // track, part, and kinds plugins add
		if t == "" && !index.Valid {
			return ""
		}
		idx := ""
		if index.Valid {
			idx = n(index.Int64)
		}
		return kind + "|" + n(parent) + "|" + strconv.Itoa(disc) + "|" + idx + "|" + t
	}
}

// matchTitle keeps only a title's letters and digits, lowercased, so
// "Heat" and "heat." match.
func matchTitle(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Replacements pairs each missing file with the file that took its place:
// another file of the library with the same key that isn't missing itself.
// The map goes from the missing file's id to its replacement's.
//
// Files with the same key can also be versions of one item (see
// GroupVersions). A missing version is simply removed, since what people
// did with it is kept on its item. A missing item takes one of the files
// still there, preferring one that isn't a version of anything yet (most
// likely the new copy), so its id and everyone's history stay even when
// it had versions. When more items than files went missing for one key,
// none are paired, since it can't be told which took which.
func Replacements(files []LibraryFile, missing map[int64]bool) map[int64]int64 {
	byKey := map[string][]LibraryFile{}
	for _, f := range files {
		if f.Key != "" {
			byKey[f.Key] = append(byKey[f.Key], f)
		}
	}
	out := map[int64]int64{}
	for _, group := range byKey {
		var gone, here []LibraryFile
		for _, f := range group {
			switch {
			case !missing[f.ID]:
				here = append(here, f)
			case f.VersionOf == 0 || missing[f.VersionOf] || !inGroup(group, f.VersionOf):
				gone = append(gone, f)
			}
		}
		if len(gone) == 0 || len(here) == 0 {
			continue
		}
		// Not versions first, then the newest file.
		slices.SortFunc(here, func(a, b LibraryFile) int {
			if (a.VersionOf == 0) != (b.VersionOf == 0) {
				if a.VersionOf == 0 {
					return -1
				}
				return 1
			}
			return cmp.Compare(b.ID, a.ID)
		})
		if len(gone) > 1 && len(gone) > len(here) {
			continue
		}
		slices.SortFunc(gone, func(a, b LibraryFile) int { return cmp.Compare(a.ID, b.ID) })
		used := map[int64]bool{}
		for _, old := range gone {
			for _, nu := range here {
				if used[nu.ID] || nu.VersionOf != 0 && nu.VersionOf != old.ID {
					continue
				}
				if old.tmdbID != 0 && nu.tmdbID != 0 && old.tmdbID != nu.tmdbID {
					continue // two films of the same name and year, told apart online
				}
				out[old.ID] = nu.ID
				used[nu.ID] = true
				break
			}
		}
	}
	return out
}

func inGroup(group []LibraryFile, id int64) bool {
	return slices.ContainsFunc(group, func(f LibraryFile) bool { return f.ID == id })
}

// userTables are the tables that hold what people did with an item, and
// the columns besides item_id that make a row unique ("" when nothing
// does). MoveFile carries them from the new item to the old one.
var userTables = []struct{ table, key string }{
	{"ratings", "user_id"},
	{"watchlist", "user_id"},
	{"collection_items", "collection_id"},
	{"playlist_items", ""},
	{"downloads", "device_id"},
	{"found_subtitles", ""},
	{"issues", ""},
}

// MoveFile gives item keep the file of item from, then deletes from. keep
// is a file that went missing and from the file that took its place, so
// keep's id, and with it everyone's watch history, ratings, lists and
// downloads, stays. Anything people already did with from (it may have
// been around for a little while) is kept too, unless keep has its own;
// for progress the newer wins. What's worked out from the file itself, such
// as intro markers and seek bar pictures, is redone in the background when
// the file is different (see MediaKey).
func (s *Store) MoveFile(ctx context.Context, keep, from int64) error {
	t, err := s.db.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer t.Rollback()
	var (
		libraryID                            int64
		parent                               sql.NullInt64
		path                                 string
		size, mtime                          int64
		duration                             sql.NullInt64
		container, vcodec, acodec            sql.NullString
		width, height                        sql.NullInt64
		subs, chapters, info, tags, mediaKey sql.NullString
		edition                              sql.NullString
	)
	err = t.QueryRowContext(ctx, `
		SELECT library_id, parent_id, path, size, mtime, duration_ms, container, video_codec, audio_codec, width, height,
			subtitle_streams, chapters, media_info, media_tags, media_key, edition
		FROM items WHERE id = ? AND path IS NOT NULL`, from).Scan(
		&libraryID, &parent, &path, &size, &mtime, &duration, &container, &vcodec, &acodec, &width, &height,
		&subs, &chapters, &info, &tags, &mediaKey, &edition)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if err := carryUserData(ctx, t, keep, from, userTables); err != nil {
		return err
	}
	if _, err := t.ExecContext(ctx, "DELETE FROM items WHERE id = ?", from); err != nil {
		return err
	}
	if _, err := t.ExecContext(ctx, `
		UPDATE items SET parent_id = ?, path = ?, size = ?, mtime = ?, duration_ms = ?, container = ?,
			video_codec = ?, audio_codec = ?, width = ?, height = ?, subtitle_streams = ?, chapters = ?,
			media_info = ?, media_tags = ?, media_key = ?, edition = ?
		WHERE id = ?`,
		parent, path, size, mtime, duration, container, vcodec, acodec, width, height,
		subs, chapters, info, tags, mediaKey, edition, keep); err != nil {
		return err
	}
	if err := pruneEmpty(ctx, t, libraryID); err != nil {
		return err
	}
	return t.Commit()
}

// carryUserData moves what people did with item from to item keep: the
// newer place to resume from, and the rows of tables, unless keep has its
// own.
func carryUserData(ctx context.Context, t execer, keep, from int64, tables []struct{ table, key string }) error {
	// Progress: whichever was updated last.
	if _, err := t.ExecContext(ctx, `
		DELETE FROM progress WHERE item_id = ? AND user_id IN (
			SELECT n.user_id FROM progress n WHERE n.item_id = ? AND n.updated_at > progress.updated_at)`, keep, from); err != nil {
		return err
	}
	if _, err := t.ExecContext(ctx, `
		UPDATE progress SET item_id = ? WHERE item_id = ?
		AND NOT EXISTS (SELECT 1 FROM progress o WHERE o.item_id = ? AND o.user_id = progress.user_id)`, keep, from, keep); err != nil {
		return err
	}
	for _, u := range tables {
		q := "UPDATE " + u.table + " SET item_id = ? WHERE item_id = ?"
		args := []any{keep, from}
		if u.key != "" {
			q += " AND NOT EXISTS (SELECT 1 FROM " + u.table + " o WHERE o.item_id = ? AND o." + u.key + " = " + u.table + "." + u.key + ")"
			args = append(args, keep)
		}
		if _, err := t.ExecContext(ctx, q, args...); err != nil {
			return err
		}
	}
	return nil
}

// RemoveFiles deletes these files, then any containers of the library left empty.
func (s *Store) RemoveFiles(ctx context.Context, libraryID int64, ids []int64) error {
	for _, id := range ids {
		if _, err := s.db.ExecContext(ctx, "DELETE FROM items WHERE id = ? AND library_id = ?", id, libraryID); err != nil {
			return err
		}
	}
	return pruneEmpty(ctx, s.db, libraryID)
}

type execer interface {
	ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error)
}

// pruneEmpty deletes a library's containers left with nothing in them.
// Radio stations have no file and no children but aren't containers.
func pruneEmpty(ctx context.Context, e execer, libraryID int64) error {
	// Empty seasons make empty shows, so repeat until nothing changes.
	for {
		res, err := e.ExecContext(ctx, `
			DELETE FROM items WHERE library_id = ? AND path IS NULL AND kind <> 'station'
			AND NOT EXISTS (SELECT 1 FROM items c WHERE c.parent_id = items.id)`, libraryID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return nil
		}
	}
}
