package api

import (
	"context"
	"math/rand/v2"
	"slices"
	"time"

	"github.com/yeixio/matinee/internal/store"
)

// The home screen's genre rows: a few genres from the library, a different
// few each day, each with its best-rated titles in a daily order. Continue
// watching, recently added and new releases stay above them.

const (
	genreRows     = 6  // rows a day
	genreRowMin   = 4  // titles a genre needs for a row
	genreRowItems = 20 // titles in a row
	newReleaseYrs = 2  // this year and last count as new
)

// today is the server's date, which picks the day's rows; tests fix it.
var today = time.Now

// rowTitles names a genre's row. Others are named after the genre.
var rowTitles = map[string]string{
	"Action":          "Action-packed",
	"Adventure":       "Adventures",
	"Animation":       "Animated favorites",
	"Comedy":          "Comedies",
	"Crime":           "Crime stories",
	"Documentary":     "Documentaries",
	"Drama":           "Dramas",
	"Family":          "For the whole family",
	"Fantasy":         "Fantasy worlds",
	"Horror":          "Horror",
	"Mystery":         "Mysteries",
	"Romance":         "Romance",
	"Science Fiction": "Sci-fi favorites",
	"Thriller":        "Thrillers",
	"War":             "War stories",
	"Western":         "Westerns",
}

// HomeRow is one of the home screen's rows: a holiday's, a pinned collection
// or a genre.
type HomeRow struct {
	ID    string `json:"id"` // e.g. "genre:Science Fiction"
	Title string `json:"title"`
	Genre string `json:"genre"`
	// CollectionID is set on the rows of collections pinned to Home.
	CollectionID string `json:"collectionId,omitempty"`
	// Holiday is set on the row for a holiday coming up, e.g. "halloween"
	// (see holidays.go).
	Holiday string       `json:"holiday,omitempty"`
	Items   []store.Item `json:"items"`
}

// dayNumber counts days, so each date picks the same rows all day.
func dayNumber(t time.Time) uint64 {
	y, m, d := t.Date()
	return uint64(time.Date(y, m, d, 0, 0, 0, 0, time.UTC).Unix() / 86400)
}

// pickGenres chooses the day's genres from those with enough titles.
func pickGenres(counts map[string]int, day uint64, n int) []string {
	var eligible []string
	for g, c := range counts {
		if c >= genreRowMin {
			eligible = append(eligible, g)
		}
	}
	slices.Sort(eligible)
	rand.New(rand.NewPCG(day, 1)).Shuffle(len(eligible), func(i, j int) { eligible[i], eligible[j] = eligible[j], eligible[i] })
	return eligible[:min(n, len(eligible))]
}

func (s *Server) genreRows(ctx context.Context, userID int64, now time.Time) ([]HomeRow, error) {
	counts, err := s.Store.GenreCounts(ctx, userID)
	if err != nil {
		return nil, err
	}
	day := dayNumber(now)
	rows := []HomeRow{}
	for i, g := range pickGenres(counts, day, genreRows) {
		// The best rated, in a different order each day.
		items, err := s.Store.InGenre(ctx, userID, g, genreRowItems*3)
		if err != nil {
			return nil, err
		}
		rand.New(rand.NewPCG(day, uint64(i)+2)).Shuffle(len(items), func(a, b int) { items[a], items[b] = items[b], items[a] })
		title := rowTitles[g]
		if title == "" {
			title = g
		}
		rows = append(rows, HomeRow{ID: "genre:" + g, Title: title, Genre: g, Items: items[:min(genreRowItems, len(items))]})
	}
	return rows, nil
}
