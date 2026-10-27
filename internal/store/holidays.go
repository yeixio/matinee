package store

import (
	"context"
	"strings"
)

// HolidayMatch says which movies and shows suit a holiday. A title matches
// when it has one of the TMDB keywords, or one of the words is in its title,
// its overview or (for a show) one of its episodes' titles. Words are
// matched without regard to case.
type HolidayMatch struct {
	Words    []string
	Keywords []string // TMDB keyword ids, as kept in item_terms
}

// HolidayItems lists the movies and shows the user can see that match,
// best rated first.
func (s *Store) HolidayItems(ctx context.Context, userID int64, m HolidayMatch, limit int) ([]Item, error) {
	where, args := m.where()
	if where == "" {
		return []Item{}, nil
	}
	return s.queryItems(ctx, "i.kind IN ('movie', 'show') AND ("+where+")",
		"COALESCE(i.rating, 0) DESC, i.id DESC LIMIT ?", append(append([]any{userID}, args...), limit)...)
}

// InGenreExcept lists movies and shows in a genre that don't match m, best
// rated first, to fill out a holiday's row.
func (s *Store) InGenreExcept(ctx context.Context, userID int64, genre string, m HolidayMatch, limit int) ([]Item, error) {
	where, args := m.where()
	if where == "" {
		where = "1 = 0"
	}
	return s.queryItems(ctx, "i.kind IN ('movie', 'show') AND i.genres LIKE ? AND NOT ("+where+")",
		"COALESCE(i.rating, 0) DESC, i.id DESC LIMIT ?", append(append([]any{userID, "%|" + genre + "|%"}, args...), limit)...)
}

func (m HolidayMatch) where() (string, []any) {
	var conds []string
	var args []any
	if len(m.Keywords) > 0 {
		conds = append(conds, "EXISTS (SELECT 1 FROM item_terms t WHERE t.item_id = i.id AND t.kind = 'keyword' AND t.term IN ("+placeholders(len(m.Keywords))+"))")
		for _, k := range m.Keywords {
			args = append(args, k)
		}
	}
	if len(m.Words) > 0 {
		var likes []string
		var words []any
		for _, w := range m.Words {
			likes = append(likes, "%s LIKE ?")
			words = append(words, "%"+strings.ToLower(w)+"%")
		}
		anyWord := "(" + strings.Join(likes, " OR ") + ")"
		on := func(col string) string { return strings.ReplaceAll(anyWord, "%s", "LOWER("+col+")") }
		conds = append(conds, on("i.title"), on("COALESCE(i.overview, '')"),
			"EXISTS (SELECT 1 FROM items se JOIN items ep ON ep.parent_id = se.id WHERE se.parent_id = i.id AND ep.kind = 'episode' AND "+on("ep.title")+")")
		for range 3 {
			args = append(args, words...)
		}
	}
	return strings.Join(conds, " OR "), args
}
