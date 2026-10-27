package api

import (
	"cmp"
	"context"
	"math/rand/v2"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/yeixio/matinee/internal/store"
)

// Holiday rows: in the weeks before a holiday, Home has a row of titles for
// it, such as Halloween's spooky picks or Christmas movies. A title is for a
// holiday when TMDB tagged it with one of the holiday's keywords, or the
// holiday is named in its title, its overview or one of its episodes'
// titles. Some holidays fill out their row from a fitting genre. An admin
// can turn the rows off.

// settingHolidayRows is "false" when an admin turned holiday rows off.
const settingHolidayRows = "holiday_rows"

const holidayLeadDays = 30 // days before a holiday its row shows

type holiday struct {
	ID    string
	Title string
	date  func(year int) time.Time
	lead  int // days before the day its row shows; holidayLeadDays if 0
	// Words are matched in titles and overviews, so none may be part of
	// other words (as "easter" is of "eastern").
	Words []string
	// Keywords are TMDB keyword names, looked up once (see keywordIDs).
	Keywords []string
	Genre    string // fills out the row after the titles that are about the day
}

func fixed(month time.Month, day int) func(int) time.Time {
	return func(y int) time.Time { return time.Date(y, month, day, 0, 0, 0, 0, time.UTC) }
}

// thanksgiving is the fourth Thursday of November (the US one).
func thanksgiving(y int) time.Time {
	first := time.Date(y, time.November, 1, 0, 0, 0, 0, time.UTC)
	return first.AddDate(0, 0, (int(time.Thursday)-int(first.Weekday())+7)%7+21)
}

// easter is Western Easter Sunday (the anonymous Gregorian algorithm).
func easter(y int) time.Time {
	a, b, c := y%19, y/100, y%100
	d, e := b/4, b%4
	f := (b + 8) / 25
	g := (b - f + 1) / 3
	h := (19*a + b - d - g + 15) % 30
	i, k := c/4, c%4
	l := (32 + 2*e + 2*i - h - k) % 7
	m := (a + 11*h + 22*l) / 451
	month := (h + l - 7*m + 114) / 31
	day := (h+l-7*m+114)%31 + 1
	return time.Date(y, time.Month(month), day, 0, 0, 0, 0, time.UTC)
}

var holidays = []holiday{
	{ID: "valentines", Title: "For Valentine's Day", date: fixed(time.February, 14),
		Words: []string{"valentine"}, Keywords: []string{"valentine's day"}, Genre: "Romance"},
	{ID: "easter", Title: "For Easter", date: easter,
		Words: []string{"easter bunny", "easter egg", "easter sunday"}, Keywords: []string{"easter", "easter bunny"}},
	{ID: "halloween", Title: "Spooky season", date: fixed(time.October, 31),
		Words:    []string{"halloween", "trick-or-treat", "trick or treat", "jack-o'-lantern", "haunted house"},
		Keywords: []string{"halloween", "haunted house", "trick or treating", "jack-o'-lantern", "witch", "ghost", "haunting"},
		Genre:    "Horror"},
	{ID: "thanksgiving", Title: "For Thanksgiving", date: thanksgiving,
		Words: []string{"thanksgiving"}, Keywords: []string{"thanksgiving"}},
	{ID: "christmas", Title: "Christmas favorites", date: fixed(time.December, 25),
		Words:    []string{"christmas", "xmas", "santa claus", "santa's", "grinch", "scrooge", "yuletide", "north pole"},
		Keywords: []string{"christmas", "santa claus", "christmas eve", "christmas party", "christmas tree", "christmas carol"}},
	// A week, so it doesn't sit beside Christmas all December.
	{ID: "newyear", Title: "Ring in the new year", date: fixed(time.December, 31), lead: 7,
		Words: []string{"new year's eve"}, Keywords: []string{"new year's eve"}},
}

// activeHolidays are the holidays whose rows show on the given day, the
// soonest first. A row shows from its lead days before through the day itself.
func activeHolidays(now time.Time) []holiday {
	y, m, d := now.Date()
	day := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	type upcoming struct {
		h    holiday
		days int
	}
	var on []upcoming
	for _, h := range holidays {
		for _, year := range []int{y, y + 1} {
			days := int(h.date(year).Sub(day).Hours() / 24)
			if days >= 0 && days <= cmp.Or(h.lead, holidayLeadDays) {
				on = append(on, upcoming{h, days})
			}
		}
	}
	slices.SortStableFunc(on, func(a, b upcoming) int { return a.days - b.days })
	out := make([]holiday, len(on))
	for i, u := range on {
		out[i] = u.h
	}
	return out
}

func (s *Server) holidayRowsOn(ctx context.Context) (bool, error) {
	v, err := s.Store.Setting(ctx, settingHolidayRows)
	return v != "false", err
}

// holidayItems are the titles for a holiday: the ones about it, then, if
// there are few, others in its genre. shuffle, if set, mixes each part.
func (s *Server) holidayItems(ctx context.Context, userID int64, h holiday, limit int, shuffle func([]store.Item, uint64)) ([]store.Item, error) {
	m := store.HolidayMatch{Words: h.Words, Keywords: s.keywordIDs(h.Keywords)}
	items, err := s.Store.HolidayItems(ctx, userID, m, limit)
	if err != nil {
		return nil, err
	}
	if shuffle != nil {
		shuffle(items, 101)
	}
	if h.Genre != "" && len(items) < genreRowItems {
		more, err := s.Store.InGenreExcept(ctx, userID, h.Genre, m, limit)
		if err != nil {
			return nil, err
		}
		if shuffle != nil {
			shuffle(more, 102)
		}
		items = append(items, more...)
	}
	return items, nil
}

// holidayRows are the rows for the holidays coming up, if any.
func (s *Server) holidayRows(ctx context.Context, userID int64, now time.Time) ([]HomeRow, error) {
	rows := []HomeRow{}
	active := activeHolidays(now)
	if len(active) == 0 {
		return rows, nil
	}
	if on, err := s.holidayRowsOn(ctx); err != nil || !on {
		return rows, err
	}
	day := dayNumber(now)
	for _, h := range active {
		// A different order each day, but the ones about the day first.
		items, err := s.holidayItems(ctx, userID, h, genreRowItems*3, func(list []store.Item, seed uint64) {
			rand.New(rand.NewPCG(day, seed)).Shuffle(len(list), func(a, b int) { list[a], list[b] = list[b], list[a] })
		})
		if err != nil {
			return nil, err
		}
		if len(items) < genreRowMin {
			continue
		}
		row := HomeRow{ID: "holiday:" + h.ID, Title: h.Title, Holiday: h.ID}
		row.Items, row.More = cut(items, genreRowItems)
		rows = append(rows, row)
	}
	return rows, nil
}

// KeywordSource finds TMDB's keywords by name. *metadata.TMDB is one.
type KeywordSource interface {
	Keywords(ctx context.Context, name string) ([]int64, error)
}

// holidayKeywords keeps TMDB's keyword ids by name. They're looked up in the
// background the first time a holiday needs them; until then (or without a
// TMDB key) holidays match on words alone.
type holidayKeywords struct {
	mu      sync.Mutex
	ids     map[string][]string
	looking map[string]bool
}

func (s *Server) keywordIDs(names []string) []string {
	k := &s.holidayKeywords
	k.mu.Lock()
	defer k.mu.Unlock()
	var ids, missing []string
	for _, n := range names {
		if got, ok := k.ids[n]; ok {
			ids = append(ids, got...)
		} else if !k.looking[n] {
			missing = append(missing, n)
		}
	}
	if len(missing) == 0 || s.Keywords == nil {
		return ids
	}
	if k.looking == nil {
		k.looking, k.ids = map[string]bool{}, map[string][]string{}
	}
	for _, n := range missing {
		k.looking[n] = true
	}
	ctx := cmp.Or(s.BackgroundCtx, context.Background())
	go func() {
		for _, n := range missing {
			found, err := s.Keywords.Keywords(ctx, n)
			k.mu.Lock()
			delete(k.looking, n)
			if err != nil {
				// Tried again the next time Home is asked for.
				s.Log.Warn("looking up a holiday keyword", "keyword", n, "err", err)
			} else {
				ids := []string{}
				for _, id := range found {
					ids = append(ids, strconv.FormatInt(id, 10))
				}
				k.ids[n] = ids
			}
			k.mu.Unlock()
		}
	}()
	return ids
}
