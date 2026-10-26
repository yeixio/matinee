package store

import (
	"context"
	"strings"
)

// Genres are kept in one column as "|Comedy|Drama|", so a genre is found
// with LIKE '%|Comedy|%' on either database. NULL means not looked up yet,
// and "|" a movie or show with none.

func encodeGenres(g []string) any {
	if g == nil {
		return nil
	}
	return "|" + strings.Join(g, "|") + "|"
}

func decodeGenres(s string) []string {
	s = strings.Trim(s, "|")
	if s == "" {
		return nil
	}
	return strings.Split(s, "|")
}

// SetGenres records a movie's or show's genres (none is an empty list).
func (s *Store) SetGenres(ctx context.Context, id int64, genres []string) error {
	if genres == nil {
		genres = []string{}
	}
	_, err := s.db.ExecContext(ctx, "UPDATE items SET genres = ? WHERE id = ?", encodeGenres(genres), id)
	return err
}

// NeedsGenres lists a library's matched movies and shows whose genres
// weren't looked up, such as ones matched before Matinee kept genres.
func (s *Store) NeedsGenres(ctx context.Context, libraryID int64) ([]Item, error) {
	return s.queryItems(ctx, "i.library_id = ? AND i.kind IN ('movie', 'show') AND i.tmdb_id IS NOT NULL AND i.genres IS NULL",
		"i.id", 0, libraryID)
}

// GenreCounts says how many movies and shows there are in each genre.
func (s *Store) GenreCounts(ctx context.Context) (map[string]int, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT genres FROM items WHERE kind IN ('movie', 'show') AND genres IS NOT NULL")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	counts := map[string]int{}
	for rows.Next() {
		var g string
		if err := rows.Scan(&g); err != nil {
			return nil, err
		}
		for _, name := range decodeGenres(g) {
			counts[name]++
		}
	}
	return counts, rows.Err()
}

// InGenre lists movies and shows in a genre, best rated first.
func (s *Store) InGenre(ctx context.Context, userID int64, genre string, limit int) ([]Item, error) {
	return s.queryItems(ctx, "i.kind IN ('movie', 'show') AND i.genres LIKE ?",
		"COALESCE(i.rating, 0) DESC, i.id DESC LIMIT ?", userID, "%|"+genre+"|%", limit)
}

// NewReleases lists movies and shows from the given year on, newest first.
func (s *Store) NewReleases(ctx context.Context, userID int64, sinceYear, limit int) ([]Item, error) {
	return s.queryItems(ctx, "i.kind IN ('movie', 'show') AND i.year >= ?",
		"i.year DESC, i.added_at DESC, i.id DESC LIMIT ?", userID, sinceYear, limit)
}
