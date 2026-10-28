package store

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strconv"
	"strings"
)

// The background analysis (internal/analyze) keeps three columns per file:
// media_info, the probe.MediaInfo as JSON; media_tags, its Tags joined with
// commas for lists and filters; and media_key, which says what was analyzed
// (see MediaKey). A file whose key no longer matches is analyzed again.

// MediaKey identifies one version of a file, as analyzed by one version of
// the analysis. The scanner updates size and mtime when a file changes.
func MediaKey(version int, size, mtime int64) string {
	return "v" + strconv.Itoa(version) + ":" + strconv.FormatInt(size, 10) + ":" + strconv.FormatInt(mtime, 10)
}

// AnalysisFile is a file the background analysis may need to look at.
type AnalysisFile struct {
	ID    int64
	Title string
	Path  string
	Size  int64
	Mtime int64
	Key   string // media_key as stored, "" when never analyzed
}

// AnalysisFiles lists files with ids above afterID, in id order, so a pass
// over the library can stop and pick up where it was.
func (s *Store) AnalysisFiles(ctx context.Context, afterID int64, limit int) ([]AnalysisFile, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, title, path, COALESCE(size, 0), COALESCE(mtime, 0), COALESCE(media_key, '')
		FROM items WHERE path IS NOT NULL AND kind IN ('movie', 'episode', 'track') AND id > ? ORDER BY id LIMIT ?`, afterID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AnalysisFile
	for rows.Next() {
		var f AnalysisFile
		if err := rows.Scan(&f.ID, &f.Title, &f.Path, &f.Size, &f.Mtime, &f.Key); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// AnalysisCounts says how many files there are, how many are analyzed as
// they are now by this version, and how many of those couldn't be read.
func (s *Store) AnalysisCounts(ctx context.Context, version int) (total, done, unreadable int, err error) {
	err = s.db.QueryRowContext(ctx, `
		SELECT COUNT(*),
			COALESCE(SUM(CASE WHEN media_key = CAST(? AS TEXT) || CAST(COALESCE(size, 0) AS TEXT) || ':' || CAST(COALESCE(mtime, 0) AS TEXT) THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN media_tags = 'unreadable' THEN 1 ELSE 0 END), 0)
		FROM items WHERE path IS NOT NULL AND kind IN ('movie', 'episode', 'track')`, "v"+strconv.Itoa(version)+":").Scan(&total, &done, &unreadable)
	return
}

// SetMediaInfo stores a file's analysis, unless the file changed since it
// was looked at (the scanner got there first), in which case it reports false.
func (s *Store) SetMediaInfo(ctx context.Context, id int64, size, mtime int64, info string, tags []string, key string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `
		UPDATE items SET media_info = ?, media_tags = ?, media_key = ?
		WHERE id = ? AND COALESCE(size, 0) = ? AND COALESCE(mtime, 0) = ?`,
		info, strings.Join(tags, ","), key, id, size, mtime)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// ClearMediaKey has a file analyzed again soon.
func (s *Store) ClearMediaKey(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, "UPDATE items SET media_key = NULL WHERE id = ?", id)
	return err
}

// MediaInfo returns a file's analysis as JSON, and whether it still matches
// the file. ok is false when it was never analyzed.
func (s *Store) MediaInfo(ctx context.Context, id int64, version int) (info string, current, ok bool, err error) {
	var v, key sql.NullString
	var size, mtime int64
	err = s.db.QueryRowContext(ctx, "SELECT media_info, media_key, COALESCE(size, 0), COALESCE(mtime, 0) FROM items WHERE id = ?", id).
		Scan(&v, &key, &size, &mtime)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, false, ErrNotFound
	}
	return v.String, key.String == MediaKey(version, size, mtime), v.Valid, err
}

func splitTags(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
}

// fillContainerTags gives shows and seasons the tags of their episodes, so
// a 4K filter finds a show with any 4K episode.
func (s *Store) fillContainerTags(ctx context.Context, items []Item) error {
	var libs []int64
	for _, it := range items {
		if (it.Kind == "show" || it.Kind == "season") && !slices.Contains(libs, it.LibraryID) {
			libs = append(libs, it.LibraryID)
		}
	}
	if len(libs) == 0 {
		return nil
	}
	tags := map[int64][]string{}
	add := func(id int64, list string) {
		for _, t := range splitTags(list) {
			if t != "unreadable" && !slices.Contains(tags[id], t) {
				tags[id] = append(tags[id], t)
			}
		}
	}
	for _, lib := range libs {
		rows, err := s.db.QueryContext(ctx, `
			SELECT se.id, COALESCE(se.parent_id, 0), e.media_tags FROM items e JOIN items se ON se.id = e.parent_id
			WHERE e.library_id = ? AND e.kind = 'episode' AND e.media_tags IS NOT NULL AND e.media_tags <> ''`, lib)
		if err != nil {
			return err
		}
		for rows.Next() {
			var season, show int64
			var list string
			if err := rows.Scan(&season, &show, &list); err != nil {
				rows.Close()
				return err
			}
			add(season, list)
			add(show, list)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
	}
	for i := range items {
		if t, ok := tags[items[i].ID]; ok && (items[i].Kind == "show" || items[i].Kind == "season") {
			items[i].MediaTags = t
		}
	}
	return nil
}
