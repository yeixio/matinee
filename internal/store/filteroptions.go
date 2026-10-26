package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// filterTermKinds are the item_terms kinds kept only for filters; Related
// leaves them out.
var filterTermKinds = []string{"studio", "country", "language", "certification"}

// filterTerms are the details a filter can pick titles by, as item_terms.
func filterTerms(d Details) []Term {
	var out []Term
	for _, s := range append(slices.Clone(d.Studios), d.Networks...) {
		out = append(out, Term{"studio", s})
	}
	for _, c := range d.Countries {
		out = append(out, Term{"country", c})
	}
	if d.OriginalLanguage != "" {
		out = append(out, Term{"language", d.OriginalLanguage})
	}
	if d.Certification != "" {
		out = append(out, Term{"certification", d.Certification})
	}
	return out
}

func releaseDate(d Details) any {
	if !dateRE.MatchString(d.ReleaseDate) {
		return nil
	}
	return d.ReleaseDate
}

// filterBackfill is bumped when filterTerms or releaseDate change, so titles
// whose details were stored before get them on the next start.
const filterBackfill = "1"

// backfillFilters fills in release dates and filter terms from details
// stored before Matinee kept them, once.
func (s *Store) backfillFilters(ctx context.Context) error {
	if v, err := s.Setting(ctx, "backfill:filters"); err != nil || v == filterBackfill {
		return err
	}
	rows, err := s.db.QueryContext(ctx, "SELECT id, kind, details FROM items WHERE details IS NOT NULL AND details <> ''")
	if err != nil {
		return err
	}
	type row struct {
		id      int64
		kind    string
		details Details
	}
	var all []row
	for rows.Next() {
		var r row
		var js string
		if err := rows.Scan(&r.id, &r.kind, &js); err != nil {
			rows.Close()
			return err
		}
		if json.Unmarshal([]byte(js), &r.details) == nil {
			all = append(all, r)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	t, err := s.db.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer t.Rollback()
	for _, r := range all {
		if _, err := t.ExecContext(ctx, "UPDATE items SET released = ? WHERE id = ?", releaseDate(r.details), r.id); err != nil {
			return err
		}
		if r.kind != "movie" && r.kind != "show" {
			continue
		}
		if _, err := t.ExecContext(ctx, "DELETE FROM item_terms WHERE item_id = ? AND kind IN ("+placeholders(len(filterTermKinds))+")",
			append([]any{r.id}, anyArgs(filterTermKinds)...)...); err != nil {
			return err
		}
		seen := map[Term]bool{}
		for _, term := range filterTerms(r.details) {
			if seen[term] || term.Term == "" {
				continue
			}
			seen[term] = true
			if _, err := t.ExecContext(ctx, "INSERT INTO item_terms (item_id, kind, term) VALUES (?, ?, ?)", r.id, term.Kind, term.Term); err != nil {
				return err
			}
		}
	}
	if _, err := t.ExecContext(ctx, `INSERT INTO settings (key, value) VALUES ('backfill:filters', ?)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value`, filterBackfill); err != nil {
		return err
	}
	return t.Commit()
}

// FilterValue is one value a filter field can take in a library, and how
// many titles have it.
type FilterValue struct {
	Value string `json:"value"`
	Label string `json:"label,omitempty"` // when the value is an id
	Count int    `json:"count"`
}

// FilterPerson is someone credited in a library.
type FilterPerson struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	HasPhoto bool   `json:"hasPhoto"`
	Count    int    `json:"count"` // titles they're in
}

// FilterOptions says what a library's titles can be filtered by: the values
// present, most common first, and the range of years, ratings and runtimes.
type FilterOptions struct {
	Genres         []FilterValue  `json:"genres"`
	Tags           []FilterValue  `json:"tags"`
	Studios        []FilterValue  `json:"studios"`
	Countries      []FilterValue  `json:"countries"`
	Languages      []FilterValue  `json:"languages"`
	Certifications []FilterValue  `json:"certifications"`
	Collections    []FilterValue  `json:"collections"`
	Directors      []FilterPerson `json:"directors"` // the most frequent
	Cast           []FilterPerson `json:"cast"`      // the most frequent
	YearMin        int            `json:"yearMin,omitempty"`
	YearMax        int            `json:"yearMax,omitempty"`
	HasRatings     bool           `json:"hasRatings"`
	HasRuntimes    bool           `json:"hasRuntimes"`
	HasReleased    bool           `json:"hasReleased"`
}

func sortedValues(counts map[string]int, labels map[string]string) []FilterValue {
	out := []FilterValue{}
	for v, n := range counts {
		out = append(out, FilterValue{Value: v, Label: labels[v], Count: n})
	}
	sort.Slice(out, func(a, b int) bool {
		if out[a].Count != out[b].Count {
			return out[a].Count > out[b].Count
		}
		return strings.ToLower(out[a].Value) < strings.ToLower(out[b].Value)
	})
	return out
}

// FilterOptions lists what a library's top level can be filtered by.
func (s *Store) FilterOptions(ctx context.Context, libraryID int64) (FilterOptions, error) {
	var o FilterOptions
	items, err := s.TopLevel(ctx, 0, libraryID)
	if err != nil {
		return o, err
	}
	genres, tags := map[string]int{}, map[string]int{}
	for _, it := range items {
		for _, g := range it.Genres {
			genres[g]++
		}
		for _, t := range it.MediaTags {
			if t != "unreadable" {
				tags[t]++
			}
		}
		if it.Year != nil {
			if o.YearMin == 0 || *it.Year < o.YearMin {
				o.YearMin = *it.Year
			}
			o.YearMax = max(o.YearMax, *it.Year)
		}
		o.HasRatings = o.HasRatings || it.Rating != nil
		o.HasRuntimes = o.HasRuntimes || it.DurationMs != nil || it.Kind == "show"
	}
	o.Genres, o.Tags = sortedValues(genres, nil), sortedValues(tags, nil)

	terms := map[string]map[string]int{}
	rows, err := s.db.QueryContext(ctx, `
		SELECT t.kind, t.term, COUNT(*) FROM item_terms t JOIN items i ON i.id = t.item_id
		WHERE i.library_id = ? AND i.parent_id IS NULL AND t.kind IN ('studio', 'country', 'language', 'certification', 'collection')
		GROUP BY t.kind, t.term`, libraryID)
	if err != nil {
		return o, err
	}
	for rows.Next() {
		var kind, term string
		var n int
		if err := rows.Scan(&kind, &term, &n); err != nil {
			rows.Close()
			return o, err
		}
		if terms[kind] == nil {
			terms[kind] = map[string]int{}
		}
		terms[kind][term] = n
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return o, err
	}
	o.Studios, o.Countries = sortedValues(terms["studio"], nil), sortedValues(terms["country"], nil)
	o.Languages, o.Certifications = sortedValues(terms["language"], nil), sortedValues(terms["certification"], nil)
	o.Collections = sortedValues(terms["collection"], s.collectionNames(ctx, libraryID))

	err = s.db.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM items WHERE library_id = ? AND released IS NOT NULL)", libraryID).Scan(&o.HasReleased)
	if err != nil {
		return o, err
	}
	if o.Directors, err = s.FilterPeople(ctx, libraryID, "director", "", 30); err != nil {
		return o, err
	}
	o.Cast, err = s.FilterPeople(ctx, libraryID, "cast", "", 30)
	return o, err
}

// collectionNames maps the TMDB collection ids of a library's movies to
// their names, from the movies' details.
func (s *Store) collectionNames(ctx context.Context, libraryID int64) map[string]string {
	names := map[string]string{}
	rows, err := s.db.QueryContext(ctx, `
		SELECT details FROM items WHERE library_id = ? AND parent_id IS NULL AND kind = 'movie' AND details LIKE '%"collection"%'`, libraryID)
	if err != nil {
		return names
	}
	defer rows.Close()
	for rows.Next() {
		var js string
		var d Details
		if rows.Scan(&js) == nil && json.Unmarshal([]byte(js), &d) == nil && d.Collection != nil {
			names[strconv.FormatInt(d.Collection.ID, 10)] = d.Collection.Name
		}
	}
	return names
}

// FilterPeople lists people credited in a library's titles (or their
// episodes), in the most titles first. role is cast, director or empty for
// any; q, when given, matches the start of any word of their name.
func (s *Store) FilterPeople(ctx context.Context, libraryID int64, role, q string, limit int) ([]FilterPerson, error) {
	cond := ""
	switch role {
	case "cast":
		cond = " AND c.kind IN ('cast', 'guest')"
	case "director":
		cond = " AND c.kind = 'crew' AND c.role IN ('Director', 'Creator')"
	}
	args := []any{libraryID}
	for _, w := range strings.Fields(strings.ToLower(q)) {
		cond += ` AND (LOWER(pe.name) LIKE ? ESCAPE '\' OR LOWER(pe.name) LIKE ? ESCAPE '\')`
		args = append(args, likeEscape(w)+"%", "% "+likeEscape(w)+"%")
	}
	args = append(args, limit)
	// Credits on seasons and episodes count toward their show.
	rows, err := s.db.QueryContext(ctx, `
		SELECT pe.id, pe.name, pe.photo IS NOT NULL AND pe.photo <> '', COUNT(DISTINCT COALESCE(sh.id, se.id, i.id)) AS n
		FROM credits c
		JOIN people pe ON pe.id = c.person_id
		JOIN items i ON i.id = c.item_id
		LEFT JOIN items se ON se.id = i.parent_id
		LEFT JOIN items sh ON sh.id = se.parent_id
		WHERE i.library_id = ?`+cond+`
		GROUP BY pe.id, pe.name, pe.photo
		ORDER BY n DESC, pe.name LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []FilterPerson{}
	for rows.Next() {
		var p FilterPerson
		var photo sql.NullBool
		if err := rows.Scan(&p.ID, &p.Name, &photo, &p.Count); err != nil {
			return nil, err
		}
		p.HasPhoto = photo.Bool
		out = append(out, p)
	}
	return out, rows.Err()
}
