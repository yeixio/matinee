package store

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// ItemFilter picks titles by their metadata. The same filter travels as URL
// query parameters (?genre=Comedy&yearMin=1980&sort=year) and as JSON, with
// the same names, so a library page's filter can be saved as it is, such as
// for a smart collection. Within one field any value matches, except tag
// and person, where every value must. Fields that are relative to today,
// such as addedWithin, are worked out each time the filter runs.
type ItemFilter struct {
	Library []int64 `json:"library,omitempty"` // none: every library
	// Kind limits results to movie, show, artist, album, episode or track.
	// Without it, a library's top level is listed: movies, shows or artists.
	Kind []string `json:"kind,omitempty"`
	Q    string   `json:"q,omitempty"` // words in the title

	Genre    []string `json:"genre,omitempty"`
	NotGenre []string `json:"notGenre,omitempty"`

	YearMin int `json:"yearMin,omitempty"`
	YearMax int `json:"yearMax,omitempty"`
	// Release dates are YYYY-MM-DD; shows use their first air date. Titles
	// without one count as released on January 1st of their year.
	ReleasedAfter  string `json:"releasedAfter,omitempty"`
	ReleasedBefore string `json:"releasedBefore,omitempty"`
	ReleasedWithin int    `json:"releasedWithin,omitempty"` // days

	// When a title was added to the library; a show counts as added when
	// its newest episode was.
	AddedAfter  string `json:"addedAfter,omitempty"`  // YYYY-MM-DD
	AddedBefore string `json:"addedBefore,omitempty"` // YYYY-MM-DD
	AddedWithin int    `json:"addedWithin,omitempty"` // days

	RatingMin float64 `json:"ratingMin,omitempty"` // 0–10
	RatingMax float64 `json:"ratingMax,omitempty"`
	// Runtime in minutes; a show's is its average episode's.
	RuntimeMin int `json:"runtimeMin,omitempty"`
	RuntimeMax int `json:"runtimeMax,omitempty"`

	// Watched is unwatched (something left to play), inProgress (started,
	// not finished), watched (all played) or notStarted (none played), for
	// the person asking.
	Watched string `json:"watched,omitempty"`

	Tag    []string `json:"tag,omitempty"` // media tags, such as 4k or atmos (see Item.MediaTags)
	NotTag []string `json:"notTag,omitempty"`

	Person   []int64 `json:"person,omitempty"`   // TMDB person ids credited in any role
	Director []int64 `json:"director,omitempty"` // directed or created it (or one of its episodes)

	Studio        []string `json:"studio,omitempty"`        // studios, and networks for shows
	Country       []string `json:"country,omitempty"`       // production countries
	Language      []string `json:"language,omitempty"`      // original language, e.g. Japanese
	Certification []string `json:"certification,omitempty"` // e.g. PG-13 or TV-MA
	Collection    []int64  `json:"collection,omitempty"`    // TMDB collection ids

	WatchList bool `json:"watchList,omitempty"` // only titles on the person's watch list

	Sort  string `json:"sort,omitempty"`  // see SortFields; title by default
	Order string `json:"order,omitempty"` // asc or desc; each sort has its own default
	Limit int    `json:"limit,omitempty"`
}

// SortFields are the orders a filter can list titles in.
var SortFields = []string{"title", "added", "released", "year", "rating", "runtime", "lastPlayed"}

// WatchedStates are the values ItemFilter.Watched takes.
var WatchedStates = []string{"unwatched", "inProgress", "watched", "notStarted"}

var dateRE = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// ParseItemFilter reads a filter from URL query parameters. Parameters it
// doesn't know are left alone; a known one with a bad value is an error.
func ParseItemFilter(q url.Values) (ItemFilter, error) {
	var f ItemFilter
	var err error
	ints := func(name string) []int64 {
		var out []int64
		for _, v := range q[name] {
			for _, part := range strings.Split(v, ",") {
				if part = strings.TrimSpace(part); part == "" {
					continue
				}
				n, e := strconv.ParseInt(part, 10, 64)
				if e != nil && err == nil {
					err = fmt.Errorf("%s: %q is not a number", name, part)
				}
				out = append(out, n)
			}
		}
		return out
	}
	strs := func(name string) []string {
		var out []string
		for _, v := range q[name] {
			if v = strings.TrimSpace(v); v != "" {
				out = append(out, v)
			}
		}
		return out
	}
	num := func(name string) int {
		v := q.Get(name)
		if v == "" {
			return 0
		}
		n, e := strconv.Atoi(v)
		if (e != nil || n < 0) && err == nil {
			err = fmt.Errorf("%s: %q is not a whole number", name, v)
		}
		return n
	}
	float := func(name string) float64 {
		v := q.Get(name)
		if v == "" {
			return 0
		}
		n, e := strconv.ParseFloat(v, 64)
		if (e != nil || n < 0) && err == nil {
			err = fmt.Errorf("%s: %q is not a number", name, v)
		}
		return n
	}
	f.Library = ints("library")
	f.Kind = strs("kind")
	f.Q = strings.TrimSpace(q.Get("q"))
	f.Genre, f.NotGenre = strs("genre"), strs("notGenre")
	f.YearMin, f.YearMax = num("yearMin"), num("yearMax")
	f.ReleasedAfter, f.ReleasedBefore, f.ReleasedWithin = q.Get("releasedAfter"), q.Get("releasedBefore"), num("releasedWithin")
	f.AddedAfter, f.AddedBefore, f.AddedWithin = q.Get("addedAfter"), q.Get("addedBefore"), num("addedWithin")
	f.RatingMin, f.RatingMax = float("ratingMin"), float("ratingMax")
	f.RuntimeMin, f.RuntimeMax = num("runtimeMin"), num("runtimeMax")
	f.Watched = q.Get("watched")
	f.Tag, f.NotTag = strs("tag"), strs("notTag")
	f.Person, f.Director = ints("person"), ints("director")
	f.Studio, f.Country, f.Language, f.Certification = strs("studio"), strs("country"), strs("language"), strs("certification")
	f.Collection = ints("collection")
	f.WatchList = q.Get("watchList") == "true" || q.Get("watchList") == "1"
	f.Sort, f.Order, f.Limit = q.Get("sort"), q.Get("order"), num("limit")
	if err != nil {
		return f, err
	}
	return f, f.Validate()
}

// Validate checks the values that must be one of a few, and the dates.
func (f ItemFilter) Validate() error {
	for name, d := range map[string]string{"releasedAfter": f.ReleasedAfter, "releasedBefore": f.ReleasedBefore, "addedAfter": f.AddedAfter, "addedBefore": f.AddedBefore} {
		if d != "" && !dateRE.MatchString(d) {
			return fmt.Errorf("%s: %q is not a YYYY-MM-DD date", name, d)
		}
	}
	for _, k := range f.Kind {
		if !slices.Contains([]string{"movie", "show", "season", "episode", "artist", "album", "track"}, k) {
			return fmt.Errorf("kind: unknown kind %q", k)
		}
	}
	if f.Watched != "" && !slices.Contains(WatchedStates, f.Watched) {
		return fmt.Errorf("watched: use one of %s", strings.Join(WatchedStates, ", "))
	}
	if f.Sort != "" && !slices.Contains(SortFields, f.Sort) {
		return fmt.Errorf("sort: use one of %s", strings.Join(SortFields, ", "))
	}
	if f.Order != "" && f.Order != "asc" && f.Order != "desc" {
		return fmt.Errorf("order: use asc or desc")
	}
	return nil
}

// Values is the filter as URL query parameters, the reverse of ParseItemFilter.
func (f ItemFilter) Values() url.Values {
	q := url.Values{}
	ints := func(name string, v []int64) {
		for _, n := range v {
			q.Add(name, strconv.FormatInt(n, 10))
		}
	}
	strs := func(name string, v []string) {
		for _, s := range v {
			q.Add(name, s)
		}
	}
	str := func(name, v string) {
		if v != "" {
			q.Set(name, v)
		}
	}
	num := func(name string, v int) {
		if v != 0 {
			q.Set(name, strconv.Itoa(v))
		}
	}
	float := func(name string, v float64) {
		if v != 0 {
			q.Set(name, strconv.FormatFloat(v, 'f', -1, 64))
		}
	}
	ints("library", f.Library)
	strs("kind", f.Kind)
	str("q", f.Q)
	strs("genre", f.Genre)
	strs("notGenre", f.NotGenre)
	num("yearMin", f.YearMin)
	num("yearMax", f.YearMax)
	str("releasedAfter", f.ReleasedAfter)
	str("releasedBefore", f.ReleasedBefore)
	num("releasedWithin", f.ReleasedWithin)
	str("addedAfter", f.AddedAfter)
	str("addedBefore", f.AddedBefore)
	num("addedWithin", f.AddedWithin)
	float("ratingMin", f.RatingMin)
	float("ratingMax", f.RatingMax)
	num("runtimeMin", f.RuntimeMin)
	num("runtimeMax", f.RuntimeMax)
	str("watched", f.Watched)
	strs("tag", f.Tag)
	strs("notTag", f.NotTag)
	ints("person", f.Person)
	ints("director", f.Director)
	strs("studio", f.Studio)
	strs("country", f.Country)
	strs("language", f.Language)
	strs("certification", f.Certification)
	ints("collection", f.Collection)
	if f.WatchList {
		q.Set("watchList", "true")
	}
	str("sort", f.Sort)
	str("order", f.Order)
	num("limit", f.Limit)
	return q
}

// The SQL below runs inside itemFrom, where i is the title and me.id the
// person asking.
const (
	// family is the ids of the title and everything under it: a show's
	// seasons and episodes, an artist's albums and tracks. Spelled out case
	// by case so each part uses an index.
	family = `(SELECT i.id UNION ALL SELECT fc.id FROM items fc WHERE fc.parent_id = i.id
		UNION ALL SELECT fg.id FROM items fc JOIN items fg ON fg.parent_id = fc.id WHERE fc.parent_id = i.id)`
	// files is family limited to what can be played, as f.
	files = `f.path IS NOT NULL AND f.id IN ` + family

	addedExpr = `CASE WHEN i.path IS NOT NULL THEN i.added_at ELSE COALESCE((SELECT MAX(f.added_at) FROM items f WHERE ` + files + `), i.added_at) END`
	// A show's runtime is its average episode's, an album's its whole length.
	runtimeExpr = `CASE WHEN i.path IS NOT NULL THEN i.duration_ms WHEN i.kind IN ('album', 'artist') THEN (SELECT SUM(f.duration_ms) FROM items f WHERE ` + files + `)
		ELSE (SELECT AVG(f.duration_ms) FROM items f WHERE ` + files + `) END`
	releasedExpr   = `COALESCE(i.released, CAST(i.year AS TEXT) || '-01-01')`
	lastPlayedExpr = `(SELECT MAX(fp.updated_at) FROM progress fp WHERE fp.user_id = me.id AND fp.item_id IN ` + family + `)`

	anyUnplayed = `EXISTS (SELECT 1 FROM items f LEFT JOIN progress fp ON fp.item_id = f.id AND fp.user_id = me.id WHERE ` + files + ` AND COALESCE(fp.played, 0) = 0)`
	anyStarted  = `EXISTS (SELECT 1 FROM progress fp WHERE fp.user_id = me.id AND fp.item_id IN ` + family + ` AND (fp.played = 1 OR fp.position_ms > 0))`
	anyFile     = `EXISTS (SELECT 1 FROM items f WHERE ` + files + `)`
)

// sortExprs map each sort to its SQL and whether it runs high to low by default.
var sortExprs = map[string]struct {
	expr string
	desc bool
}{
	"title":      {"i.sort_title", false},
	"added":      {addedExpr, true},
	"released":   {releasedExpr, true},
	"year":       {"i.year", true},
	"rating":     {"i.rating", true},
	"runtime":    {runtimeExpr, false},
	"lastPlayed": {lastPlayedExpr, true},
}

// likeEscape makes s match itself in a LIKE pattern with ESCAPE '\'.
func likeEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?, ", n), ", ")
}

func anyArgs[T any](v []T) []any {
	out := make([]any, len(v))
	for i, x := range v {
		out[i] = x
	}
	return out
}

// sql turns the filter into a WHERE clause, ORDER BY and their arguments
// (which follow the user id). today is the date relative fields count from.
func (f ItemFilter) sql(today time.Time) (where, order string, args []any) {
	var conds []string
	add := func(cond string, a ...any) {
		conds = append(conds, cond)
		args = append(args, a...)
	}
	if len(f.Library) > 0 {
		add("i.library_id IN ("+placeholders(len(f.Library))+")", anyArgs(f.Library)...)
	}
	if len(f.Kind) > 0 {
		add("i.kind IN ("+placeholders(len(f.Kind))+")", anyArgs(f.Kind)...)
	} else {
		add("i.parent_id IS NULL")
	}
	for _, w := range strings.Fields(strings.ToLower(f.Q)) {
		add(`LOWER(i.title) LIKE ? ESCAPE '\'`, "%"+likeEscape(w)+"%")
	}
	if len(f.Genre) > 0 {
		var or []string
		for _, g := range f.Genre {
			or = append(or, `i.genres LIKE ? ESCAPE '\'`)
			args = append(args, "%|"+likeEscape(g)+"|%")
		}
		conds = append(conds, "("+strings.Join(or, " OR ")+")")
	}
	for _, g := range f.NotGenre {
		add(`COALESCE(i.genres, '') NOT LIKE ? ESCAPE '\'`, "%|"+likeEscape(g)+"|%")
	}
	if f.YearMin > 0 {
		add("i.year >= ?", f.YearMin)
	}
	if f.YearMax > 0 {
		add("i.year <= ?", f.YearMax)
	}
	if f.ReleasedAfter != "" {
		add(releasedExpr+" >= ?", f.ReleasedAfter)
	}
	if f.ReleasedBefore != "" {
		add(releasedExpr+" <= ?", f.ReleasedBefore)
	}
	if f.ReleasedWithin > 0 {
		add(releasedExpr+" BETWEEN ? AND ?", today.AddDate(0, 0, -f.ReleasedWithin).Format(time.DateOnly), today.Format(time.DateOnly))
	}
	day := func(d string) int64 {
		t, _ := time.ParseInLocation(time.DateOnly, d, today.Location())
		return t.Unix()
	}
	if f.AddedAfter != "" {
		add(addedExpr+" >= ?", day(f.AddedAfter))
	}
	if f.AddedBefore != "" {
		add(addedExpr+" < ?", day(f.AddedBefore)+86400)
	}
	if f.AddedWithin > 0 {
		add(addedExpr+" >= ?", today.AddDate(0, 0, -f.AddedWithin).Unix())
	}
	if f.RatingMin > 0 {
		add("i.rating >= ?", f.RatingMin)
	}
	if f.RatingMax > 0 {
		add("i.rating <= ?", f.RatingMax)
	}
	if f.RuntimeMin > 0 {
		add(runtimeExpr+" >= ?", f.RuntimeMin*60000)
	}
	if f.RuntimeMax > 0 {
		add(runtimeExpr+" <= ?", f.RuntimeMax*60000)
	}
	switch f.Watched {
	case "unwatched":
		add(anyUnplayed)
	case "watched":
		add(anyFile + " AND NOT " + anyUnplayed)
	case "inProgress":
		add(anyStarted + " AND " + anyUnplayed)
	case "notStarted":
		add("NOT " + anyStarted)
	}
	hasTag := `EXISTS (SELECT 1 FROM items f WHERE ` + files + ` AND ',' || COALESCE(f.media_tags, '') || ',' LIKE ? ESCAPE '\')`
	for _, t := range f.Tag {
		add(hasTag, "%,"+likeEscape(t)+",%")
	}
	for _, t := range f.NotTag {
		add("NOT "+hasTag, "%,"+likeEscape(t)+",%")
	}
	for _, p := range f.Person {
		add(`EXISTS (SELECT 1 FROM credits c WHERE c.person_id = ? AND c.item_id IN `+family+`)`, p)
	}
	if len(f.Director) > 0 {
		add(`EXISTS (SELECT 1 FROM credits c WHERE c.person_id IN (`+placeholders(len(f.Director))+`)
			AND c.kind = 'crew' AND c.role IN ('Director', 'Creator') AND c.item_id IN `+family+`)`, anyArgs(f.Director)...)
	}
	terms := func(kind string, values []any) {
		if len(values) > 0 {
			add(`EXISTS (SELECT 1 FROM item_terms t WHERE t.item_id = i.id AND t.kind = ? AND t.term IN (`+placeholders(len(values))+`))`,
				append([]any{kind}, values...)...)
		}
	}
	terms("studio", anyArgs(f.Studio))
	terms("country", anyArgs(f.Country))
	terms("language", anyArgs(f.Language))
	terms("certification", anyArgs(f.Certification))
	var cols []any
	for _, c := range f.Collection {
		cols = append(cols, strconv.FormatInt(c, 10))
	}
	terms("collection", cols)
	if f.WatchList {
		add("EXISTS (SELECT 1 FROM watchlist wl WHERE wl.user_id = me.id AND wl.item_id = i.id)")
	}

	by, ok := sortExprs[f.Sort]
	if !ok {
		by = sortExprs["title"]
	}
	desc := by.desc
	if f.Order != "" {
		desc = f.Order == "desc"
	}
	dir := "ASC"
	if desc {
		dir = "DESC"
	}
	// Titles missing the sort field go last either way.
	order = "CASE WHEN (" + by.expr + ") IS NULL THEN 1 ELSE 0 END, " + by.expr + " " + dir + ", i.sort_title, i.id"
	if f.Limit > 0 {
		order += " LIMIT ?"
		args = append(args, f.Limit)
	}
	return strings.Join(conds, " AND "), order, args
}

// FilterItems lists the titles the filter picks, for the given person.
func (s *Store) FilterItems(ctx context.Context, userID int64, f ItemFilter) ([]Item, error) {
	if err := f.Validate(); err != nil {
		return nil, err
	}
	where, order, args := f.sql(time.Now())
	items, err := s.queryItems(ctx, where, order, append([]any{userID}, args...)...)
	if err != nil {
		return nil, err
	}
	return items, s.fillContainerTags(ctx, items)
}
