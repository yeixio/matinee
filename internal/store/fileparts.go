package store

import (
	"cmp"
	"context"
	"slices"
	"strconv"
	"strings"
)

// A movie can come in parts, a file each, such as "Heat CD1.avi" and
// "Heat CD2.avi". The first part is the movie; the others point to it
// (items.file_part_of) and are left out of every list. The movie plays its
// parts one after the other as one video (see stream.Input), its length
// is theirs together, and watched marks and places to resume from are
// kept on it for the whole movie.

// FilePart is one file of a movie in parts.
type FilePart struct {
	Path       string `json:"-"`
	DurationMs int64  `json:"durationMs"`
	Size       int64  `json:"size"`
}

// GroupParts makes the files of a library's movies that are parts of one
// movie into it, and makes files that no longer are movies of their own.
// partKey says which movie a file is a part of (files with the same key
// are one movie) and which part it is; key is "" for a file that isn't a
// part. A movie needs at least two different parts.
func (s *Store) GroupParts(ctx context.Context, libraryID int64, partKey func(path string) (key string, part int)) (changed int, err error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, path, COALESCE(file_part_of, 0), COALESCE(file_part, 0), COALESCE(duration_ms, 0) FROM items
		WHERE library_id = ? AND kind = 'movie' AND path IS NOT NULL`, libraryID)
	if err != nil {
		return 0, err
	}
	type file struct {
		id, partOf, ms int64
		part, was      int
	}
	groups := map[string][]file{}
	var all []file
	for rows.Next() {
		var f file
		var path string
		if err := rows.Scan(&f.id, &path, &f.partOf, &f.was, &f.ms); err != nil {
			rows.Close()
			return 0, err
		}
		key, n := partKey(path)
		f.part = n
		all = append(all, f)
		if key != "" {
			groups[key] = append(groups[key], f)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	// Where each file belongs: its movie's first part and its number.
	type place struct {
		of   int64
		part int
	}
	want := map[int64]place{}
	for _, g := range groups {
		slices.SortFunc(g, func(a, b file) int { return cmp.Or(cmp.Compare(a.part, b.part), cmp.Compare(a.id, b.id)) })
		var kept []file
		for _, f := range g {
			if len(kept) == 0 || kept[len(kept)-1].part != f.part {
				kept = append(kept, f) // a second copy of a part stays a movie of its own
			}
		}
		if len(kept) < 2 || allFeatureLength(kept, func(f file) int64 { return f.ms }) {
			continue
		}
		for i, f := range kept {
			p := place{part: f.part}
			if i > 0 {
				p.of = kept[0].id
			}
			want[f.id] = p
		}
	}
	t, err := s.db.BeginTx(ctx)
	if err != nil {
		return 0, err
	}
	defer t.Rollback()
	for _, f := range all {
		w := want[f.id]
		if w.of == f.partOf && w.part == f.was {
			continue
		}
		if w.of != 0 && f.partOf == 0 {
			// It was a movie of its own; what people did with it joins the
			// movie. A place to resume from in a later part means nothing
			// in the whole, so that's left behind.
			if err := carryRows(ctx, t, w.of, f.id, versionTables); err != nil {
				return 0, err
			}
		}
		q := "UPDATE items SET file_part_of = ?, file_part = ? WHERE id = ?"
		if w.of != 0 {
			// A later part is never a version of anything.
			q = "UPDATE items SET file_part_of = ?, file_part = ?, version_of = NULL WHERE id = ?"
		}
		if _, err := t.ExecContext(ctx, q, nullInt64(w.of), nullInt(w.part), f.id); err != nil {
			return 0, err
		}
		changed++
	}
	return changed, t.Commit()
}

// featureLength is how long a file runs at least to be a whole movie
// rather than a part of one. Halves of a movie on two CDs run about an
// hour each; "Disk 1" and "Disk 2" that each run this long are two copies
// or cuts of the movie, which are kept apart (and grouped as versions).
const featureLength = 80 * 60 * 1000

func allFeatureLength[T any](files []T, ms func(T) int64) bool {
	for _, f := range files {
		if ms(f) < featureLength {
			return false
		}
	}
	return true
}

// fillParts gives the movies among items that come in parts their parts,
// their length together, and no container, so players stream them through
// the server rather than playing the first file alone.
func (s *Store) fillParts(ctx context.Context, items []Item) error {
	var ids []string
	at := map[int64]int{}
	for i, it := range items {
		if it.Kind == "movie" && it.Path != "" {
			ids = append(ids, strconv.FormatInt(it.ID, 10))
			at[it.ID] = i
		}
	}
	if len(ids) == 0 {
		return nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT file_part_of, path, COALESCE(duration_ms, 0), COALESCE(size, 0) FROM items
		WHERE file_part_of IN (`+strings.Join(ids, ",")+`) ORDER BY file_part_of, file_part, id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var of int64
		var p FilePart
		if err := rows.Scan(&of, &p.Path, &p.DurationMs, &p.Size); err != nil {
			return err
		}
		it := &items[at[of]]
		if len(it.Parts) == 0 {
			first := FilePart{Path: it.Path, Size: it.Size}
			if it.DurationMs != nil {
				first.DurationMs = *it.DurationMs
			}
			it.Parts = []FilePart{first}
		}
		it.Parts = append(it.Parts, p)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, i := range at {
		it := &items[i]
		if len(it.Parts) < 2 {
			continue
		}
		var total int64
		for _, p := range it.Parts {
			total += p.DurationMs
		}
		it.DurationMs, it.Container = &total, ""
	}
	return nil
}
