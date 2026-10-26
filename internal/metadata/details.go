package metadata

import (
	"cmp"
	"context"
	"fmt"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/yeixio/matinee/internal/store"
)

// Detailer is a provider that also knows the longer details of movies and
// shows: cast and crew, studios, ratings and what they have in common with
// other titles.
type Detailer interface {
	// Details looks up a movie or show by the provider id its lookup found.
	Details(ctx context.Context, kind string, id int64) (*store.DetailsUpdate, error)
	// SeasonDetails looks up a season's cast, and each episode's guest stars
	// and crew, keyed by episode number.
	SeasonDetails(ctx context.Context, showID int64, season int) (*store.DetailsUpdate, map[int]store.DetailsUpdate, error)
}

// Details are looked up again after this long, so new awards and changed
// casts show up.
const detailsRefresh = 90 * 24 * time.Hour

// FetchDetails fills in cast, crew and details for a library's matched
// movies, shows and seasons, and for the episodes of those seasons.
func (f *Fetcher) FetchDetails(ctx context.Context, lib store.Library) {
	var d Detailer
	for _, p := range f.Providers {
		if pd, ok := p.(Detailer); ok {
			d = pd
			break
		}
	}
	if d == nil {
		return
	}
	items, err := f.Store.NeedsDetails(ctx, lib.ID, time.Now().Add(-detailsRefresh).Unix())
	if err != nil {
		f.Log.Error("list items needing details", "err", err)
		return
	}
	done := 0
	for _, it := range items {
		if ctx.Err() != nil {
			return
		}
		var err error
		switch it.Kind {
		case "movie", "show":
			err = f.itemDetails(ctx, d, it)
		case "season":
			err = f.seasonDetails(ctx, d, it)
		}
		if err != nil {
			// Leave it for the next scan; the provider may be down.
			f.Log.Warn("details lookup", "title", it.Title, "err", err)
			continue
		}
		done++
	}
	if len(items) > 0 {
		f.Log.Info("details done", "library", lib.Name, "looked_up", done)
	}
}

func (f *Fetcher) itemDetails(ctx context.Context, d Detailer, it store.Item) error {
	u, err := d.Details(ctx, it.Kind, *it.TMDBID)
	if err != nil {
		return err
	}
	if u == nil {
		return f.Store.MarkDetails(ctx, it.ID)
	}
	if f.Awards != nil && u.Details.WikidataID != "" {
		awards, err := f.Awards.Awards(ctx, u.Details.WikidataID)
		if err != nil {
			// Details without awards beat none; awards come with the next refresh.
			f.Log.Warn("awards lookup", "title", it.Title, "err", err)
		}
		u.Details.Awards, u.Details.AwardSummary = awards, AwardSummary(awards)
	}
	return f.Store.ApplyDetails(ctx, it.ID, *u)
}

func (f *Fetcher) seasonDetails(ctx context.Context, d Detailer, it store.Item) error {
	if it.ParentID == nil || it.IndexNumber == nil {
		return f.Store.MarkDetails(ctx, it.ID)
	}
	show, err := f.Store.Item(ctx, 0, *it.ParentID)
	if err != nil {
		return err
	}
	if show.TMDBID == nil {
		return f.Store.MarkDetails(ctx, it.ID)
	}
	u, episodes, err := d.SeasonDetails(ctx, *show.TMDBID, *it.IndexNumber)
	if err != nil {
		return err
	}
	if u == nil {
		return f.Store.MarkDetails(ctx, it.ID)
	}
	kids, err := f.Store.Children(ctx, 0, it.ID)
	if err != nil {
		return err
	}
	for _, ep := range kids {
		if ep.IndexNumber == nil {
			continue
		}
		if eu, ok := episodes[*ep.IndexNumber]; ok {
			if err := f.Store.ApplyEpisodeDetails(ctx, ep.ID, eu); err != nil {
				return err
			}
		}
	}
	return f.Store.ApplyDetails(ctx, it.ID, *u)
}

// crewJobs are the jobs worth showing, most important first. TMDB lists
// hundreds of people for a big film; these are the ones people look for.
var crewJobs = []string{
	"Director", "Creator", "Screenplay", "Writer", "Teleplay", "Story", "Novel", "Characters", "Author",
	"Original Music Composer", "Music", "Director of Photography", "Editor",
	"Producer", "Executive Producer", "Casting", "Production Design", "Costume Design",
}

const (
	maxCast      = 30
	maxGuests    = 20
	maxProducers = 4 // of each producer job; big films list dozens
)

type tmdbPerson struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	ProfilePath string `json:"profile_path"`
	Character   string `json:"character"`
	Job         string `json:"job"`
	Department  string `json:"department"`
	Order       int    `json:"order"`
	// aggregate_credits, for shows
	Roles []struct {
		Character    string `json:"character"`
		EpisodeCount int    `json:"episode_count"`
	} `json:"roles"`
	Jobs []struct {
		Job          string `json:"job"`
		EpisodeCount int    `json:"episode_count"`
	} `json:"jobs"`
	TotalEpisodeCount int `json:"total_episode_count"`
}

type named struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	EnglishName string `json:"english_name"`
	ISO639      string `json:"iso_639_1"`
}

type tmdbDetails struct {
	Tagline          string       `json:"tagline"`
	Title            string       `json:"title"`
	Name             string       `json:"name"`
	OriginalTitle    string       `json:"original_title"`
	OriginalName     string       `json:"original_name"`
	OriginalLanguage string       `json:"original_language"`
	ReleaseDate      string       `json:"release_date"`
	FirstAirDate     string       `json:"first_air_date"`
	LastAirDate      string       `json:"last_air_date"`
	Status           string       `json:"status"`
	Genres           []named      `json:"genres"`
	Companies        []named      `json:"production_companies"`
	Networks         []named      `json:"networks"`
	Countries        []named      `json:"production_countries"`
	SpokenLanguages  []named      `json:"spoken_languages"`
	Budget           int64        `json:"budget"`
	Revenue          int64        `json:"revenue"`
	Collection       *named       `json:"belongs_to_collection"`
	Seasons          int          `json:"number_of_seasons"`
	Episodes         int          `json:"number_of_episodes"`
	IMDbID           string       `json:"imdb_id"`
	CreatedBy        []tmdbPerson `json:"created_by"`
	ExternalIDs      struct {
		IMDbID     string `json:"imdb_id"`
		WikidataID string `json:"wikidata_id"`
	} `json:"external_ids"`
	Credits struct {
		Cast []tmdbPerson `json:"cast"`
		Crew []tmdbPerson `json:"crew"`
	} `json:"credits"`
	AggregateCredits struct {
		Cast []tmdbPerson `json:"cast"`
		Crew []tmdbPerson `json:"crew"`
	} `json:"aggregate_credits"`
	ReleaseDates struct {
		Results []struct {
			Country string `json:"iso_3166_1"`
			Dates   []struct {
				Certification string `json:"certification"`
				Type          int    `json:"type"`
			} `json:"release_dates"`
		} `json:"results"`
	} `json:"release_dates"`
	ContentRatings struct {
		Results []struct {
			Country string `json:"iso_3166_1"`
			Rating  string `json:"rating"`
		} `json:"results"`
	} `json:"content_ratings"`
	Keywords struct {
		Keywords []named `json:"keywords"` // movies
		Results  []named `json:"results"`  // shows
	} `json:"keywords"`
	Recommendations struct {
		Results []named `json:"results"`
	} `json:"recommendations"`
}

// Details implements Detailer.
func (t *TMDB) Details(ctx context.Context, kind string, id int64) (*store.DetailsUpdate, error) {
	path, extra, ns := fmt.Sprintf("/movie/%d", id), "credits,release_dates", "movie"
	if kind == "show" {
		path, extra, ns = fmt.Sprintf("/tv/%d", id), "aggregate_credits,content_ratings", "tv"
	}
	var r tmdbDetails
	params := url.Values{"append_to_response": {extra + ",keywords,recommendations,external_ids"}}
	if err := t.get(ctx, path, params, &r); err != nil {
		if err == errNotFound {
			return nil, nil
		}
		return nil, err
	}
	d := store.Details{
		Tagline:  r.Tagline,
		Status:   r.Status,
		Budget:   r.Budget,
		Revenue:  r.Revenue,
		Seasons:  r.Seasons,
		Episodes: r.Episodes,
		IMDbID:   cmp.Or(r.IMDbID, r.ExternalIDs.IMDbID),
		// Wikidata ids are only for looking up awards.
		WikidataID: r.ExternalIDs.WikidataID,
	}
	title, original := cmp.Or(r.Title, r.Name), cmp.Or(r.OriginalTitle, r.OriginalName)
	if original != title {
		d.OriginalTitle = original
	}
	d.OriginalLanguage = languageName(r.OriginalLanguage, r.SpokenLanguages)
	d.ReleaseDate = cmp.Or(r.ReleaseDate, r.FirstAirDate)
	if r.Status == "Ended" || r.Status == "Canceled" {
		d.EndDate = r.LastAirDate
	}
	u := &store.DetailsUpdate{}
	for _, g := range r.Genres {
		d.Genres = append(d.Genres, g.Name)
		u.Terms = append(u.Terms, store.Term{Kind: "genre", Term: ns + ":" + strconv.FormatInt(g.ID, 10)})
	}
	d.Studios = names(r.Companies, 4)
	d.Networks = names(r.Networks, 3)
	d.Countries = names(r.Countries, 4)
	for _, l := range r.SpokenLanguages {
		if n := cmp.Or(l.EnglishName, l.Name); n != "" {
			d.Languages = append(d.Languages, n)
		}
	}
	if r.Collection != nil && r.Collection.ID != 0 {
		d.Collection = &store.Collection{ID: r.Collection.ID, Name: r.Collection.Name}
		u.Terms = append(u.Terms, store.Term{Kind: "collection", Term: strconv.FormatInt(r.Collection.ID, 10)})
	}
	for _, k := range append(r.Keywords.Keywords, r.Keywords.Results...) {
		u.Terms = append(u.Terms, store.Term{Kind: "keyword", Term: strconv.FormatInt(k.ID, 10)})
	}
	// Two titles share a "rec" term when one recommends the other, or both
	// recommend the same third title.
	u.Terms = append(u.Terms, store.Term{Kind: "rec", Term: ns + ":" + strconv.FormatInt(id, 10)})
	for _, rec := range r.Recommendations.Results {
		u.Terms = append(u.Terms, store.Term{Kind: "rec", Term: ns + ":" + strconv.FormatInt(rec.ID, 10)})
	}

	if kind == "show" {
		for _, rc := range r.ContentRatings.Results {
			if rc.Country == t.region() && rc.Rating != "" {
				d.Certification = rc.Rating
			}
		}
		u.Cast = t.showCast(r.AggregateCredits.Cast, maxCast)
		var crew []tmdbPerson
		for _, c := range r.CreatedBy {
			c.Job, c.Department = "Creator", "Writing"
			crew = append(crew, c)
		}
		for _, c := range r.AggregateCredits.Crew {
			for _, j := range c.Jobs {
				cc := c
				cc.Job = j.Job
				crew = append(crew, cc)
			}
		}
		u.Crew = t.crew(crew)
	} else {
		d.Certification = movieCertification(r, t.region())
		u.Cast = t.cast(r.Credits.Cast, maxCast)
		u.Crew = t.crew(r.Credits.Crew)
	}
	u.Details = d
	return u, nil
}

// SeasonDetails implements Detailer.
func (t *TMDB) SeasonDetails(ctx context.Context, showID int64, season int) (*store.DetailsUpdate, map[int]store.DetailsUpdate, error) {
	var r struct {
		AirDate  string `json:"air_date"`
		Episodes []struct {
			Number     int          `json:"episode_number"`
			AirDate    string       `json:"air_date"`
			Crew       []tmdbPerson `json:"crew"`
			GuestStars []tmdbPerson `json:"guest_stars"`
		} `json:"episodes"`
		AggregateCredits struct {
			Cast []tmdbPerson `json:"cast"`
		} `json:"aggregate_credits"`
	}
	params := url.Values{"append_to_response": {"aggregate_credits"}}
	if err := t.get(ctx, fmt.Sprintf("/tv/%d/season/%d", showID, season), params, &r); err != nil {
		if err == errNotFound {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	u := &store.DetailsUpdate{Details: store.Details{ReleaseDate: r.AirDate}, Cast: t.showCast(r.AggregateCredits.Cast, maxCast)}
	episodes := map[int]store.DetailsUpdate{}
	for _, ep := range r.Episodes {
		episodes[ep.Number] = store.DetailsUpdate{
			Details: store.Details{ReleaseDate: ep.AirDate},
			Guests:  t.cast(ep.GuestStars, maxGuests),
			Crew:    t.crew(ep.Crew),
		}
	}
	return u, episodes, nil
}

func (t *TMDB) region() string {
	if t.Region == "" {
		return "US"
	}
	return t.Region
}

func (t *TMDB) credit(p tmdbPerson, role, dept string) store.Credit {
	return store.Credit{PersonID: p.ID, Name: p.Name, Role: role, Department: dept, Photo: t.image("w185", p.ProfilePath)}
}

func (t *TMDB) cast(list []tmdbPerson, limit int) []store.Credit {
	list = slices.Clone(list)
	sort.SliceStable(list, func(i, j int) bool { return list[i].Order < list[j].Order })
	var out []store.Credit
	for _, p := range list {
		if len(out) == limit {
			break
		}
		out = append(out, t.credit(p, p.Character, "Acting"))
	}
	return out
}

// showCast takes a show's aggregate cast, which lists every character
// someone played and in how many episodes.
func (t *TMDB) showCast(list []tmdbPerson, limit int) []store.Credit {
	var out []store.Credit
	for _, p := range list {
		if len(out) == limit {
			break
		}
		var chars []string
		for _, r := range p.Roles {
			if r.Character != "" && !slices.Contains(chars, r.Character) {
				chars = append(chars, r.Character)
			}
		}
		c := t.credit(p, strings.Join(chars, " / "), "Acting")
		c.Episodes = p.TotalEpisodeCount
		out = append(out, c)
	}
	return out
}

// crew keeps the jobs in crewJobs, most important job first, and lists
// each person once per job.
func (t *TMDB) crew(list []tmdbPerson) []store.Credit {
	type key struct {
		id  int64
		job string
	}
	seen := map[key]bool{}
	perJob := map[string]int{}
	var out []store.Credit
	for _, job := range crewJobs {
		for _, p := range list {
			if p.Job != job || seen[key{p.ID, job}] {
				continue
			}
			if strings.Contains(job, "Producer") && perJob[job] == maxProducers {
				continue
			}
			seen[key{p.ID, job}] = true
			perJob[job]++
			out = append(out, t.credit(p, job, p.Department))
		}
	}
	return out
}

// movieCertification prefers the region's theatrical rating, then any of
// its releases' ratings.
func movieCertification(r tmdbDetails, region string) string {
	best := ""
	for _, c := range r.ReleaseDates.Results {
		if c.Country != region {
			continue
		}
		for _, d := range c.Dates {
			if d.Certification == "" {
				continue
			}
			if d.Type == 3 {
				return d.Certification
			}
			if best == "" {
				best = d.Certification
			}
		}
	}
	return best
}

func languageName(code string, spoken []named) string {
	for _, l := range spoken {
		if l.ISO639 == code && l.EnglishName != "" {
			return l.EnglishName
		}
	}
	if n, ok := languageNames[code]; ok {
		return n
	}
	return strings.ToUpper(code)
}

// languageNames covers original languages a title's spoken languages may
// not list.
var languageNames = map[string]string{
	"en": "English", "fr": "French", "es": "Spanish", "de": "German", "it": "Italian", "ja": "Japanese",
	"ko": "Korean", "zh": "Chinese", "cn": "Cantonese", "hi": "Hindi", "ru": "Russian", "pt": "Portuguese",
	"sv": "Swedish", "da": "Danish", "no": "Norwegian", "nl": "Dutch", "pl": "Polish", "tr": "Turkish",
}

func names(list []named, limit int) []string {
	var out []string
	for _, n := range list {
		if len(out) == limit {
			break
		}
		if n.Name != "" {
			out = append(out, n.Name)
		}
	}
	return out
}

// Person looks up someone's biography.
func (t *TMDB) Person(ctx context.Context, id int64) (*store.PersonInfo, error) {
	var r struct {
		Biography    string `json:"biography"`
		Birthday     string `json:"birthday"`
		Deathday     string `json:"deathday"`
		PlaceOfBirth string `json:"place_of_birth"`
		KnownFor     string `json:"known_for_department"`
		ProfilePath  string `json:"profile_path"`
	}
	if err := t.get(ctx, fmt.Sprintf("/person/%d", id), nil, &r); err != nil {
		if err == errNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &store.PersonInfo{
		Biography: r.Biography, Birthday: r.Birthday, Deathday: r.Deathday, Birthplace: r.PlaceOfBirth,
		KnownFor: r.KnownFor, Photo: t.image("w185", r.ProfilePath),
	}, nil
}
