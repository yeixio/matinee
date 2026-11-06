package store

import (
	"context"
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// Letter is where titles starting with one letter begin in a filtered list,
// for the A to Z bar beside a library's grid.
type Letter struct {
	Letter string `json:"letter"` // A to Z, # for digits and symbols, or a letter of another alphabet
	Index  int    `json:"index"`  // the first title's place in the list
	Count  int    `json:"count"`
}

// LetterOf is the letter a sort title files under: its first letter
// without accents and in capitals, or # when it starts with anything else.
func LetterOf(sortTitle string) string {
	for _, r := range norm.NFD.String(strings.TrimSpace(sortTitle)) {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		if !unicode.IsLetter(r) {
			return "#"
		}
		switch r {
		case 'ß':
			return "S"
		case 'æ', 'Æ':
			return "A"
		case 'ø', 'Ø':
			return "O"
		case 'œ', 'Œ':
			return "O"
		case 'ł', 'Ł':
			return "L"
		}
		return string(unicode.ToUpper(r))
	}
	return "#"
}

// FilterLetters says where each letter starts in the list FilterItems gives
// for the same filter. Only a title sort runs in letter order, so other
// sorts get no letters.
func (s *Store) FilterLetters(ctx context.Context, userID int64, f ItemFilter) ([]Letter, error) {
	if err := f.Validate(); err != nil {
		return nil, err
	}
	out := []Letter{}
	if f.Sort != "" && f.Sort != "title" {
		return out, nil
	}
	where, order, args := f.sql(time.Now())
	rows, err := s.db.QueryContext(ctx, "SELECT COALESCE(i.sort_title, '')"+itemFrom+" WHERE ("+where+") AND i.version_of IS NULL AND i.file_part_of IS NULL"+visibleToMe+" ORDER BY "+order,
		append([]any{userID}, args...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	at := map[string]int{} // a letter's place in out
	n := 0
	for rows.Next() {
		var title string
		if err := rows.Scan(&title); err != nil {
			return nil, err
		}
		l := LetterOf(title)
		if i, ok := at[l]; ok {
			// The list keeps a letter together; a stray one (sorted by a
			// rule the letter doesn't follow) counts where it first began.
			out[i].Count++
		} else {
			at[l] = len(out)
			out = append(out, Letter{Letter: l, Index: n, Count: 1})
		}
		n++
	}
	return out, rows.Err()
}
