package store

import (
	"context"
	"encoding/json"
	"sort"

	"github.com/yeixio/matinee/internal/probe"
)

// Audiobooks: an author holds books, and a book holds its parts, the files
// it comes in. A book is one long listen: where someone is in it is kept on
// the book itself, across its parts, and its chapters come from the parts'
// own chapters, or one per part when a part has none.

// BookInfo is what a book's files say about it.
type BookInfo struct {
	Narrators   []string
	Series      string
	SeriesIndex *float64 // its place in the series, such as 2 or 2.5
	Overview    string
}

// SetBookInfo records what a book's files say about it. What a file
// leaves out keeps what the book had.
func (s *Store) SetBookInfo(ctx context.Context, id int64, b BookInfo) error {
	var idx any
	if b.SeriesIndex != nil {
		idx = *b.SeriesIndex
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE items SET narrators = COALESCE(?, narrators), series = COALESCE(?, series),
			series_index = COALESCE(?, series_index), overview = COALESCE(?, overview)
		WHERE id = ?`,
		encodeGenres(b.Narrators), nullStr(b.Series), idx, nullStr(b.Overview), id)
	return err
}

// SumBookLengths gives each book of a library the length of all its parts.
func (s *Store) SumBookLengths(ctx context.Context, libraryID int64) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE items SET duration_ms = (SELECT SUM(c.duration_ms) FROM items c WHERE c.parent_id = items.id)
		WHERE library_id = ? AND kind = 'book'`, libraryID)
	return err
}

// BookChapter is a chapter on the book's own clock, which runs across its parts.
type BookChapter struct {
	Title   string `json:"title"`
	StartMs int64  `json:"startMs"`
	EndMs   int64  `json:"endMs"`
	PartID  int64  `json:"partId"`
}

// BookPart is one file of a book, and where on the book's clock it starts.
type BookPart struct {
	Item
	StartMs int64 `json:"startMs"`
}

// Book is a book with its parts in order and its chapters.
type Book struct {
	Item
	Parts    []BookPart    `json:"parts"`
	Chapters []BookChapter `json:"chapters"`
}

// Book returns a book with its parts and chapters, for the given person.
// A part's id finds its book.
func (s *Store) Book(ctx context.Context, userID, id int64) (Book, error) {
	it, err := s.Item(ctx, userID, id)
	if err != nil {
		return Book{}, err
	}
	if it.Kind == "part" && it.ParentID != nil {
		if it, err = s.Item(ctx, userID, *it.ParentID); err != nil {
			return Book{}, err
		}
	}
	if it.Kind != "book" {
		return Book{}, ErrNotFound
	}
	parts, err := s.Children(ctx, userID, it.ID)
	if err != nil {
		return Book{}, err
	}
	marks, err := s.fileChapters(ctx, it.ID)
	if err != nil {
		return Book{}, err
	}
	b := Book{Item: it, Parts: []BookPart{}}
	var at int64
	for _, p := range parts {
		if !p.Playable {
			continue
		}
		b.Parts = append(b.Parts, BookPart{Item: p, StartMs: at})
		var length int64
		if p.DurationMs != nil {
			length = *p.DurationMs
		}
		at += length
	}
	b.Chapters = bookChapters(b.Parts, marks)
	return b, nil
}

func (s *Store) fileChapters(ctx context.Context, bookID int64) (map[int64][]probe.Chapter, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id, chapters FROM items WHERE parent_id = ? AND chapters IS NOT NULL", bookID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64][]probe.Chapter{}
	for rows.Next() {
		var id int64
		var js string
		if err := rows.Scan(&id, &js); err != nil {
			return nil, err
		}
		var c []probe.Chapter
		if json.Unmarshal([]byte(js), &c) == nil {
			out[id] = c
		}
	}
	return out, rows.Err()
}

// bookChapters puts every part's chapters on the book's clock. A part
// without chapters of its own is one chapter, named after the part.
func bookChapters(parts []BookPart, marks map[int64][]probe.Chapter) []BookChapter {
	out := []BookChapter{}
	for _, p := range parts {
		var length int64
		if p.DurationMs != nil {
			length = *p.DurationMs
		}
		cs := marks[p.ID]
		if len(cs) == 0 {
			out = append(out, BookChapter{Title: p.Title, StartMs: p.StartMs, EndMs: p.StartMs + length, PartID: p.ID})
			continue
		}
		for i, c := range cs {
			end := c.EndMs
			if length > 0 && (end > length || i == len(cs)-1) {
				end = length
			}
			out = append(out, BookChapter{Title: c.Title, StartMs: p.StartMs + c.StartMs, EndMs: p.StartMs + end, PartID: p.ID})
		}
	}
	return out
}

// SetBookProgress saves where someone is in a book. Its parts count as
// played when the book is finished, and as not played again when it isn't,
// so filters such as unwatched treat books like albums.
func (s *Store) SetBookProgress(ctx context.Context, userID, bookID, positionMs int64, played bool) error {
	if err := s.SetProgress(ctx, userID, bookID, positionMs, played); err != nil {
		return err
	}
	return s.markParts(ctx, userID, bookID, played, now())
}

// MergeBookProgress is SetBookProgress for a place recorded offline: it
// applies unless the book's place changed more recently (see
// MergeProgress), and reports whether it did.
func (s *Store) MergeBookProgress(ctx context.Context, userID int64, e ProgressEvent) (bool, error) {
	ok, err := s.MergeProgress(ctx, userID, e)
	if err != nil || !ok {
		return ok, err
	}
	return true, s.markParts(ctx, userID, e.ItemID, e.Played, e.At)
}

// markParts plays a book's parts with the book, or unplays them.
func (s *Store) markParts(ctx context.Context, userID, bookID int64, played bool, at int64) error {
	if played {
		_, err := s.db.ExecContext(ctx, `
			INSERT INTO progress (user_id, item_id, position_ms, played, updated_at)
			SELECT ?, id, 0, 1, ? FROM items WHERE parent_id = ? AND path IS NOT NULL
			ON CONFLICT (user_id, item_id) DO UPDATE SET played = 1, position_ms = 0, updated_at = excluded.updated_at`,
			userID, at, bookID)
		return err
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE progress SET played = 0 WHERE user_id = ? AND played = 1
		AND item_id IN (SELECT id FROM items WHERE parent_id = ?)`, userID, bookID)
	return err
}

// ContinueListening lists books someone started and didn't finish, the
// latest first, leaving out the ones they took off the row.
func (s *Store) ContinueListening(ctx context.Context, userID int64, limit int) ([]Item, error) {
	return s.queryItems(ctx, "p.played = 0 AND p.position_ms > 0 AND p.hidden = 0 AND i.kind = 'book'",
		"p.updated_at DESC LIMIT ?", userID, limit)
}

// BookGroup is a series or a narrator, with how many books it has and a
// few of them for artwork.
type BookGroup struct {
	Name  string  `json:"name"`
	Books int     `json:"books"`
	Items []int64 `json:"itemIds"` // up to four books, first in reading order
}

// Series lists a library's series by name.
func (s *Store) Series(ctx context.Context, userID, libraryID int64) ([]BookGroup, error) {
	books, err := s.queryItems(ctx, "i.library_id = ? AND i.kind = 'book' AND i.series IS NOT NULL",
		"COALESCE(i.series_index, 0), i.sort_title", userID, libraryID)
	if err != nil {
		return nil, err
	}
	return group(books, func(b Item) []string { return []string{b.Series} }), nil
}

// Narrators lists everyone who narrates a book of the library, by name.
func (s *Store) Narrators(ctx context.Context, userID, libraryID int64) ([]BookGroup, error) {
	books, err := s.queryItems(ctx, "i.library_id = ? AND i.kind = 'book' AND i.narrators IS NOT NULL",
		"i.sort_title", userID, libraryID)
	if err != nil {
		return nil, err
	}
	return group(books, func(b Item) []string { return b.Narrators }), nil
}

func group(books []Item, names func(Item) []string) []BookGroup {
	byName := map[string]*BookGroup{}
	for _, b := range books {
		for _, n := range names(b) {
			if n == "" {
				continue
			}
			g := byName[n]
			if g == nil {
				g = &BookGroup{Name: n, Items: []int64{}}
				byName[n] = g
			}
			g.Books++
			if len(g.Items) < 4 {
				g.Items = append(g.Items, b.ID)
			}
		}
	}
	out := make([]BookGroup, 0, len(byName))
	for _, g := range byName {
		out = append(out, *g)
	}
	sort.Slice(out, func(i, j int) bool { return SortTitle(out[i].Name) < SortTitle(out[j].Name) })
	return out
}
