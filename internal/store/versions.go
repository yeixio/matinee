package store

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"slices"
	"strconv"
)

// A library can hold several files of one movie or episode: a 4K copy for
// the TV and a 1080p one for phones, or the theatrical and the extended
// cut. Each file stays an item of its own, so everything worked out from a
// file (its streams, intro, credits, seek bar pictures) is its own, but one
// of them is the title and the others are its versions (items.version_of).
// Lists show only the title; its page lists the versions to pick from; and
// watched marks, places to resume from, ratings, watch lists, collections
// and playlists are all kept on the title, whichever version is played.

// versionTables are what people did with an item that belongs to the
// title rather than a file. Downloads, found subtitles and problem reports
// are about one file, so they stay with it.
var versionTables = []struct{ table, key string }{
	{"ratings", "user_id"},
	{"watchlist", "user_id"},
	{"collection_items", "collection_id"},
	{"playlist_items", ""},
}

// primaryID is the item that keeps what people do with this one: its title
// when it's a version, otherwise itself. An unknown id is returned as is.
func (s *Store) primaryID(ctx context.Context, id int64) (int64, error) {
	var p int64
	err := s.db.QueryRowContext(ctx, "SELECT COALESCE(version_of, id) FROM items WHERE id = ?", id).Scan(&p)
	if errors.Is(err, sql.ErrNoRows) {
		return id, nil
	}
	return p, err
}

// VersionChanges is what GroupVersions did.
type VersionChanges struct {
	Grouped, Split, Editions int
}

// GroupVersions makes files of the same movie or episode in a library
// versions of one item, and splits off files that no longer are (after a
// fixed match, say). Two movie files are the same movie when they're
// matched to the same one online, or when neither is and their titles and
// years agree; episodes are the same episode of the same season of the
// same show. edition reads a file's edition from its path; GroupVersions
// keeps each file's up to date.
//
// The item that stays the title is one that already is, or else the oldest
// file. When files that were titles of their own become versions, what
// people did with them moves to the title.
func (s *Store) GroupVersions(ctx context.Context, libraryID int64, edition func(path string) string) (VersionChanges, error) {
	var ch VersionChanges
	files, err := s.LibraryFiles(ctx, libraryID)
	if err != nil {
		return ch, err
	}
	var videos []LibraryFile
	for _, f := range files {
		if f.Kind == "movie" || f.Kind == "episode" {
			videos = append(videos, f)
		}
	}
	t, err := s.db.BeginTx(ctx)
	if err != nil {
		return ch, err
	}
	defer t.Rollback()
	for _, f := range videos {
		if e := edition(f.Path); e != f.edition {
			if _, err := t.ExecContext(ctx, "UPDATE items SET edition = ? WHERE id = ?", nullStr(e), f.ID); err != nil {
				return ch, err
			}
			ch.Editions++
		}
	}
	for _, group := range versionGroups(videos) {
		primary := titleOf(group)
		for _, f := range group {
			want := primary
			if f.ID == primary {
				want = 0
			}
			if f.VersionOf == want {
				continue
			}
			switch {
			case want == 0:
				_, err = t.ExecContext(ctx, "UPDATE items SET version_of = NULL WHERE id = ?", f.ID)
				if f.VersionOf != 0 {
					ch.Split++
				}
			default:
				if f.VersionOf == 0 {
					// It was a title of its own; its history joins the title's.
					if err := carryUserData(ctx, t, primary, f.ID, versionTables); err != nil {
						return ch, err
					}
				}
				_, err = t.ExecContext(ctx, "UPDATE items SET version_of = ? WHERE id = ?", primary, f.ID)
				ch.Grouped++
			}
			if err != nil {
				return ch, err
			}
		}
	}
	return ch, t.Commit()
}

// versionGroups sorts files into the same movie or episode. Every file is
// in exactly one group, maybe alone.
func versionGroups(files []LibraryFile) [][]LibraryFile {
	// Movies matched online go by their match. One that isn't matched joins
	// the matched ones with its title and year, when they're all one movie.
	byMatch := map[string][]int{}
	keyMatches := map[string]map[int64]bool{}
	for i, f := range files {
		if f.Kind == "movie" && f.tmdbID != 0 && f.PartOf == 0 {
			k := "tmdb|" + strconv.FormatInt(f.tmdbID, 10)
			byMatch[k] = append(byMatch[k], i)
			if f.Key != "" {
				if keyMatches[f.Key] == nil {
					keyMatches[f.Key] = map[int64]bool{}
				}
				keyMatches[f.Key][f.tmdbID] = true
			}
		}
	}
	for i, f := range files {
		switch {
		case f.PartOf != 0:
			// A later part of a movie is part of it, not a version of it.
			byMatch["file|"+strconv.FormatInt(f.ID, 10)] = []int{i}
		case f.Kind == "movie" && f.tmdbID != 0:
		case f.Key == "":
			byMatch["file|"+strconv.FormatInt(f.ID, 10)] = []int{i}
		case f.Kind == "movie" && len(keyMatches[f.Key]) == 1:
			for id := range keyMatches[f.Key] {
				k := "tmdb|" + strconv.FormatInt(id, 10)
				byMatch[k] = append(byMatch[k], i)
			}
		case f.Kind == "movie" && len(keyMatches[f.Key]) > 1:
			// Two films of that name and year: can't tell which.
			byMatch["file|"+strconv.FormatInt(f.ID, 10)] = []int{i}
		default:
			byMatch[f.Key] = append(byMatch[f.Key], i)
		}
	}
	out := make([][]LibraryFile, 0, len(byMatch))
	for _, idx := range byMatch {
		g := make([]LibraryFile, len(idx))
		for j, i := range idx {
			g[j] = files[i]
		}
		out = append(out, g)
	}
	return out
}

// titleOf picks the file of a group that's the title: the one most of the
// others already are versions of, or else the oldest that's a title now,
// or else the oldest.
func titleOf(group []LibraryFile) int64 {
	refs := map[int64]int{}
	isTitle := map[int64]bool{}
	for _, f := range group {
		if f.VersionOf != 0 {
			refs[f.VersionOf]++
		} else {
			isTitle[f.ID] = true
		}
	}
	best := slices.MinFunc(group, func(a, b LibraryFile) int {
		ra, rb := refs[a.ID], refs[b.ID]
		if !isTitle[a.ID] {
			ra = -1
		}
		if !isTitle[b.ID] {
			rb = -1
		}
		if ra != rb {
			return cmp.Compare(rb, ra)
		}
		if isTitle[a.ID] != isTitle[b.ID] {
			if isTitle[a.ID] {
				return -1
			}
			return 1
		}
		return cmp.Compare(a.ID, b.ID)
	})
	return best.ID
}

// Versions lists the files of an item's title, the title first and then
// the rest from the biggest picture down, each with the user's progress
// (which is the title's). An item with no other versions lists only itself.
func (s *Store) Versions(ctx context.Context, userID, id int64) ([]Item, error) {
	primary, err := s.primaryID(ctx, id)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, "SELECT "+itemCols+itemFrom+
		" WHERE (i.id = ? OR i.version_of = ?) AND i.path IS NOT NULL"+visibleToMe+
		" ORDER BY CASE WHEN i.id = ? THEN 0 ELSE 1 END, COALESCE(i.height, 0) DESC, i.id", userID, primary, primary, primary)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Item{}
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	return out, s.fillParts(ctx, out)
}
