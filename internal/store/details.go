package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// Details is the longer description of a movie, show or episode that the
// details pages show: everything beyond the title, overview and artwork.
type Details struct {
	Tagline          string      `json:"tagline,omitempty"`
	OriginalTitle    string      `json:"originalTitle,omitempty"`
	OriginalLanguage string      `json:"originalLanguage,omitempty"` // a language name, e.g. Japanese
	ReleaseDate      string      `json:"releaseDate,omitempty"`      // YYYY-MM-DD; first air date for shows
	EndDate          string      `json:"endDate,omitempty"`          // last air date of a show that ended
	Status           string      `json:"status,omitempty"`           // e.g. Released, Returning Series, Ended
	Certification    string      `json:"certification,omitempty"`    // e.g. PG-13 or TV-MA
	Genres           []string    `json:"genres,omitempty"`
	Studios          []string    `json:"studios,omitempty"`
	Networks         []string    `json:"networks,omitempty"`
	Countries        []string    `json:"countries,omitempty"`
	Languages        []string    `json:"languages,omitempty"` // spoken languages
	Budget           int64       `json:"budget,omitempty"`    // US dollars
	Revenue          int64       `json:"revenue,omitempty"`   // worldwide box office, US dollars
	Collection       *Collection `json:"collection,omitempty"`
	Seasons          int         `json:"seasons,omitempty"`
	Episodes         int         `json:"episodes,omitempty"`
	IMDbID           string      `json:"imdbId,omitempty"`
	WikidataID       string      `json:"wikidataId,omitempty"`
	// Awards are the wins and nominations Wikidata knows of, wins first.
	Awards []Award `json:"awards,omitempty"`
	// AwardSummary is one line for the main details pane, e.g. "Won 4
	// Academy Awards" or "12 wins and 30 nominations".
	AwardSummary string `json:"awardSummary,omitempty"`
}

// Collection is a series of movies, such as a franchise's films.
type Collection struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type Award struct {
	Name  string   `json:"name"` // e.g. Academy Award for Best Picture
	Year  int      `json:"year,omitempty"`
	Won   bool     `json:"won"`
	Names []string `json:"names,omitempty"` // the people it went to, when not the title itself
}

// Credit is one person's part in a title: a character they play, or a job.
type Credit struct {
	PersonID int64  `json:"personId"`
	Name     string `json:"name"`
	Role     string `json:"role,omitempty"` // character, or job such as Director
	// Department groups crew: Directing, Writing, Sound, Camera, Production…
	Department string `json:"department,omitempty"`
	Episodes   int    `json:"episodes,omitempty"` // for a show's cast: how many episodes they're in
	HasPhoto   bool   `json:"hasPhoto"`

	Photo string `json:"-"` // URL
}

// Credits are the people in a title, in billing order.
type Credits struct {
	Cast []Credit `json:"cast"`
	// Guests are an episode's guest stars.
	Guests []Credit `json:"guests,omitempty"`
	Crew   []Credit `json:"crew"`
}

// Term is something titles have in common, for finding related titles:
// a genre, keyword, collection or recommendation.
type Term struct {
	Kind string // genre, keyword, collection or rec
	Term string
}

// DetailsUpdate is what a provider knows about a title beyond Metadata.
type DetailsUpdate struct {
	Details Details
	Cast    []Credit
	Guests  []Credit
	Crew    []Credit
	Terms   []Term
	// RatingLevel is the level of Details.Certification (see the rating package).
	RatingLevel int
}

// NeedsDetails lists a library's matched movies, shows and seasons whose
// details were never fetched or were fetched before olderThan (Unix seconds).
func (s *Store) NeedsDetails(ctx context.Context, libraryID, olderThan int64) ([]Item, error) {
	return s.queryItems(ctx,
		"i.library_id = ? AND i.kind IN ('movie', 'show', 'season') AND i.tmdb_id IS NOT NULL AND COALESCE(i.details_at, 0) < ?",
		"CASE i.kind WHEN 'show' THEN 0 WHEN 'season' THEN 1 ELSE 2 END, i.id", 0, libraryID, olderThan)
}

// MarkDetails records that an item's details were looked up and nothing was
// found, so it isn't asked for again until the next refresh.
func (s *Store) MarkDetails(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, "UPDATE items SET details_at = ? WHERE id = ?", now(), id)
	return err
}

// ApplyDetails replaces an item's details, credits and terms.
func (s *Store) ApplyDetails(ctx context.Context, id int64, u DetailsUpdate) error {
	js, err := json.Marshal(u.Details)
	if err != nil {
		return err
	}
	t, err := s.db.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer t.Rollback()
	if _, err := t.ExecContext(ctx, "UPDATE items SET details = ?, details_at = ?, released = ?, rating_level = ? WHERE id = ?",
		string(js), now(), releaseDate(u.Details), nullInt(u.RatingLevel), id); err != nil {
		return err
	}
	if err := applyCredits(ctx, t, id, u); err != nil {
		return err
	}
	if _, err := t.ExecContext(ctx, "DELETE FROM item_terms WHERE item_id = ?", id); err != nil {
		return err
	}
	seen := map[Term]bool{}
	for _, term := range append(u.Terms, filterTerms(u.Details)...) {
		if seen[term] || term.Term == "" {
			continue
		}
		seen[term] = true
		if _, err := t.ExecContext(ctx, "INSERT INTO item_terms (item_id, kind, term) VALUES (?, ?, ?)", id, term.Kind, term.Term); err != nil {
			return err
		}
	}
	return t.Commit()
}

// ApplyEpisodeDetails stores an episode's air date, guest stars and crew,
// which arrive with its season's.
func (s *Store) ApplyEpisodeDetails(ctx context.Context, id int64, u DetailsUpdate) error {
	js, err := json.Marshal(u.Details)
	if err != nil {
		return err
	}
	t, err := s.db.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer t.Rollback()
	if _, err := t.ExecContext(ctx, "UPDATE items SET details = ?, details_at = ?, released = ? WHERE id = ?",
		string(js), now(), releaseDate(u.Details), id); err != nil {
		return err
	}
	if err := applyCredits(ctx, t, id, u); err != nil {
		return err
	}
	return t.Commit()
}

func applyCredits(ctx context.Context, t *tx, id int64, u DetailsUpdate) error {
	if _, err := t.ExecContext(ctx, "DELETE FROM credits WHERE item_id = ?", id); err != nil {
		return err
	}
	ord := 0
	for _, list := range []struct {
		kind    string
		credits []Credit
	}{{"cast", u.Cast}, {"guest", u.Guests}, {"crew", u.Crew}} {
		for _, c := range list.credits {
			if c.PersonID == 0 || c.Name == "" {
				continue
			}
			if _, err := t.ExecContext(ctx, `
				INSERT INTO people (id, name, photo) VALUES (?, ?, ?)
				ON CONFLICT (id) DO UPDATE SET name = excluded.name, photo = COALESCE(excluded.photo, people.photo)`,
				c.PersonID, c.Name, nullStr(c.Photo)); err != nil {
				return err
			}
			if _, err := t.ExecContext(ctx, `
				INSERT INTO credits (item_id, person_id, kind, role, department, episodes, ord) VALUES (?, ?, ?, ?, ?, ?, ?)`,
				id, c.PersonID, list.kind, c.Role, c.Department, c.Episodes, ord); err != nil {
				return err
			}
			ord++
		}
	}
	return nil
}

// ItemDetails returns what ApplyDetails stored for an item, or nil.
func (s *Store) ItemDetails(ctx context.Context, id int64) (*Details, error) {
	var js sql.NullString
	err := s.db.QueryRowContext(ctx, "SELECT details FROM items WHERE id = ?", id).Scan(&js)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil || !js.Valid || js.String == "" {
		return nil, err
	}
	var d Details
	if err := json.Unmarshal([]byte(js.String), &d); err != nil {
		return nil, err
	}
	tagline, err := s.TranslatedTagline(ctx, id)
	if tagline != "" {
		d.Tagline = tagline
	}
	return &d, err
}

// ItemCredits returns an item's cast, guest stars and crew in billing order.
func (s *Store) ItemCredits(ctx context.Context, id int64) (Credits, error) {
	out := Credits{Cast: []Credit{}, Crew: []Credit{}}
	rows, err := s.db.QueryContext(ctx, `
		SELECT c.kind, c.person_id, p.name, c.role, c.department, c.episodes, COALESCE(p.photo, '')
		FROM credits c JOIN people p ON p.id = c.person_id
		WHERE c.item_id = ? ORDER BY c.ord`, id)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var kind string
		var c Credit
		if err := rows.Scan(&kind, &c.PersonID, &c.Name, &c.Role, &c.Department, &c.Episodes, &c.Photo); err != nil {
			return out, err
		}
		c.HasPhoto = c.Photo != ""
		switch kind {
		case "cast":
			out.Cast = append(out.Cast, c)
		case "guest":
			out.Guests = append(out.Guests, c)
		default:
			out.Crew = append(out.Crew, c)
		}
	}
	return out, rows.Err()
}

// Person is someone in the cast or crew of titles in the library.
type Person struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	HasPhoto   bool   `json:"hasPhoto"`
	Biography  string `json:"biography,omitempty"`
	Birthday   string `json:"birthday,omitempty"` // YYYY-MM-DD
	Deathday   string `json:"deathday,omitempty"`
	Birthplace string `json:"birthplace,omitempty"`
	KnownFor   string `json:"knownFor,omitempty"` // a department, e.g. Acting or Directing

	Photo  string `json:"-"`
	InfoAt int64  `json:"-"` // when the biography was last looked up; 0 if never
}

// PersonInfo is a person's biography from a provider.
type PersonInfo struct {
	Biography, Birthday, Deathday, Birthplace, KnownFor string
	Photo                                               string // URL
}

func (s *Store) Person(ctx context.Context, id int64) (Person, error) {
	var p Person
	var photo, bio, born, died, place, known sql.NullString
	var at sql.NullInt64
	err := s.db.QueryRowContext(ctx, `
		SELECT id, name, photo, bio, birthday, deathday, birthplace, known_for, info_at FROM people WHERE id = ?`, id).
		Scan(&p.ID, &p.Name, &photo, &bio, &born, &died, &place, &known, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	p.Photo, p.Biography, p.Birthday, p.Deathday = photo.String, bio.String, born.String, died.String
	p.Birthplace, p.KnownFor, p.InfoAt = place.String, known.String, at.Int64
	p.HasPhoto = p.Photo != ""
	return p, err
}

func (s *Store) SetPersonInfo(ctx context.Context, id int64, in PersonInfo) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE people SET bio = ?, birthday = ?, deathday = ?, birthplace = ?, known_for = ?,
			photo = COALESCE(?, photo), info_at = ?
		WHERE id = ?`,
		nullStr(in.Biography), nullStr(in.Birthday), nullStr(in.Deathday), nullStr(in.Birthplace), nullStr(in.KnownFor),
		nullStr(in.Photo), now(), id)
	return err
}

// PersonTitle is a title in the library someone is in, with their parts in it.
type PersonTitle struct {
	Item  Item     `json:"item"`
	Roles []string `json:"roles"`
}

// PersonTitles lists the movies and shows in the library a person is in,
// newest first. Parts in single episodes count toward their show.
func (s *Store) PersonTitles(ctx context.Context, userID, personID int64) ([]PersonTitle, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT CASE i.kind
				WHEN 'episode' THEN (SELECT se.parent_id FROM items se WHERE se.id = i.parent_id)
				WHEN 'season' THEN i.parent_id
				ELSE i.id END,
			c.kind, c.role, c.episodes, i.kind
		FROM credits c JOIN items i ON i.id = c.item_id
		WHERE c.person_id = ?
		ORDER BY c.ord`, personID)
	if err != nil {
		return nil, err
	}
	type part struct {
		roles    []string
		guest    map[string]int // role → episodes
		episodes int
	}
	var order []int64
	parts := map[int64]*part{}
	for rows.Next() {
		var top sql.NullInt64
		var kind, role, itemKind string
		var episodes int
		if err := rows.Scan(&top, &kind, &role, &episodes, &itemKind); err != nil {
			rows.Close()
			return nil, err
		}
		if !top.Valid {
			continue
		}
		p := parts[top.Int64]
		if p == nil {
			p = &part{guest: map[string]int{}}
			parts[top.Int64] = p
			order = append(order, top.Int64)
		}
		if itemKind == "episode" {
			if _, ok := p.guest[role]; !ok {
				p.roles = append(p.roles, "\x00"+role)
			}
			p.guest[role]++
			continue
		}
		if !slices.Contains(p.roles, role) && role != "" {
			p.roles = append(p.roles, role)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := []PersonTitle{}
	for _, id := range order {
		it, err := s.Item(ctx, userID, id)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		p := parts[id]
		roles := []string{}
		for _, r := range p.roles {
			if name, ok := strings.CutPrefix(r, "\x00"); ok {
				r = name + episodesSuffix(p.guest[name])
				if name == "" {
					r = strings.TrimPrefix(episodesSuffix(p.guest[name]), " · ")
				}
			}
			roles = append(roles, r)
		}
		out = append(out, PersonTitle{Item: it, Roles: roles})
	}
	// Newest first; titles without a year last.
	year := func(it Item) int {
		if it.Year == nil {
			return 0
		}
		return *it.Year
	}
	sort.SliceStable(out, func(i, j int) bool { return year(out[i].Item) > year(out[j].Item) })
	return out, nil
}

func episodesSuffix(n int) string {
	if n == 1 {
		return " · 1 episode"
	}
	return " · " + strconv.Itoa(n) + " episodes"
}
