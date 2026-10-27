package metadata

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/yeixio/matinee/i18n"
	"github.com/yeixio/matinee/internal/store"
)

// TMDB looks up movies and TV on themoviedb.org. It takes either a v3 API key
// or a v4 read access token.
type TMDB struct {
	Key      string
	BaseURL  string // defaults to the public API; tests point it elsewhere
	ImageURL string
	Client   *http.Client
	// Region picks which country's age ratings to show; defaults to US.
	Region string
}

func NewTMDB(key string) *TMDB {
	return &TMDB{
		Key:      key,
		BaseURL:  "https://api.themoviedb.org/3",
		ImageURL: "https://image.tmdb.org/t/p/",
		Client:   &http.Client{Timeout: 15 * time.Second},
	}
}

func (t *TMDB) Name() string    { return "tmdb" }
func (t *TMDB) Kinds() []string { return []string{"movie", "show", "season", "episode"} }

func (t *TMDB) Lookup(ctx context.Context, q Query) (*store.Metadata, error) {
	switch q.Kind {
	case "movie":
		return t.search(ctx, "movie", q.Title, q.Year)
	case "show":
		return t.search(ctx, "tv", q.Title, q.Year)
	case "season":
		return t.season(ctx, q)
	case "episode":
		return t.episode(ctx, q)
	}
	return nil, nil
}

type searchResult struct {
	ID           int64   `json:"id"`
	Overview     string  `json:"overview"`
	VoteAverage  float64 `json:"vote_average"`
	PosterPath   string  `json:"poster_path"`
	BackdropPath string  `json:"backdrop_path"`
	ReleaseDate  string  `json:"release_date"`
	FirstAirDate string  `json:"first_air_date"`
	GenreIDs     []int   `json:"genre_ids"`
}

func (t *TMDB) search(ctx context.Context, kind, title string, year int) (*store.Metadata, error) {
	params := url.Values{"query": {title}, "include_adult": {"false"}}
	if year > 0 {
		if kind == "movie" {
			params.Set("year", strconv.Itoa(year))
		} else {
			params.Set("first_air_date_year", strconv.Itoa(year))
		}
	}
	var res struct {
		Results []searchResult `json:"results"`
	}
	if err := t.get(ctx, "/search/"+kind, params, &res); err != nil {
		return nil, err
	}
	if len(res.Results) == 0 {
		if year > 0 {
			// File names sometimes carry the wrong year; try without it.
			return t.search(ctx, kind, title, 0)
		}
		return nil, nil
	}
	r := res.Results[0]
	date := r.ReleaseDate
	if date == "" {
		date = r.FirstAirDate
	}
	y, _ := strconv.Atoi(strings.SplitN(date, "-", 2)[0])
	return &store.Metadata{
		TMDBID: r.ID, Overview: r.Overview, Year: y, Rating: r.VoteAverage,
		Poster: t.image("w500", r.PosterPath), Backdrop: t.image("w1280", r.BackdropPath),
		Genres: genreNames(r.GenreIDs),
	}, nil
}

// Genres looks up the genres of a movie or show matched before Matinee kept them.
func (t *TMDB) Genres(ctx context.Context, kind string, id int64) ([]string, error) {
	path := fmt.Sprintf("/movie/%d", id)
	if kind == "show" {
		path = fmt.Sprintf("/tv/%d", id)
	}
	var res struct {
		Genres []struct {
			ID int `json:"id"`
		} `json:"genres"`
	}
	if err := t.get(ctx, path, nil, &res); err != nil {
		if err == errNotFound {
			return []string{}, nil
		}
		return nil, err
	}
	ids := make([]int, len(res.Genres))
	for i, g := range res.Genres {
		ids[i] = g.ID
	}
	return genreNames(ids), nil
}

// Keywords finds the ids of TMDB's keywords named exactly name, ignoring
// case. There is usually one, sometimes a few duplicates.
func (t *TMDB) Keywords(ctx context.Context, name string) ([]int64, error) {
	var res struct {
		Results []named `json:"results"`
	}
	if err := t.get(ctx, "/search/keyword", url.Values{"query": {name}}, &res); err != nil {
		return nil, err
	}
	ids := []int64{}
	for _, k := range res.Results {
		if strings.EqualFold(k.Name, name) {
			ids = append(ids, k.ID)
		}
	}
	return ids, nil
}

// TMDB's genres, by id, as Matinee names them. TV's combined genres count as
// both (a "Sci-Fi & Fantasy" show is in Science Fiction and in Fantasy), so a
// genre row has the movies and the shows. TV Movie, News and Talk are left out.
var tmdbGenres = map[int][]string{
	28: {"Action"}, 12: {"Adventure"}, 16: {"Animation"}, 35: {"Comedy"}, 80: {"Crime"},
	99: {"Documentary"}, 18: {"Drama"}, 10751: {"Family"}, 14: {"Fantasy"}, 36: {"History"},
	27: {"Horror"}, 10402: {"Music"}, 9648: {"Mystery"}, 10749: {"Romance"}, 878: {"Science Fiction"},
	53: {"Thriller"}, 10752: {"War"}, 37: {"Western"},
	10759: {"Action", "Adventure"}, 10762: {"Family"}, 10764: {"Reality"}, 10765: {"Science Fiction", "Fantasy"},
	10766: {"Drama"}, 10768: {"War"},
}

func genreNames(ids []int) []string {
	out := []string{}
	for _, id := range ids {
		for _, name := range tmdbGenres[id] {
			if !slices.Contains(out, name) {
				out = append(out, name)
			}
		}
	}
	return out
}

func (t *TMDB) episode(ctx context.Context, q Query) (*store.Metadata, error) {
	var res struct {
		ID          int64   `json:"id"`
		Name        string  `json:"name"`
		Overview    string  `json:"overview"`
		VoteAverage float64 `json:"vote_average"`
		StillPath   string  `json:"still_path"`
	}
	path := fmt.Sprintf("/tv/%d/season/%d/episode/%d", q.ShowID, q.Season, q.Episode)
	if err := t.get(ctx, path, nil, &res); err != nil {
		if err == errNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &store.Metadata{
		TMDBID: res.ID, Title: res.Name, Overview: res.Overview, Rating: res.VoteAverage,
		Poster: t.image("w780", res.StillPath),
	}, nil
}

func (t *TMDB) season(ctx context.Context, q Query) (*store.Metadata, error) {
	var res struct {
		ID         int64  `json:"id"`
		Overview   string `json:"overview"`
		PosterPath string `json:"poster_path"`
		AirDate    string `json:"air_date"`
	}
	if err := t.get(ctx, fmt.Sprintf("/tv/%d/season/%d", q.ShowID, q.Season), nil, &res); err != nil {
		if err == errNotFound {
			return nil, nil
		}
		return nil, err
	}
	y, _ := strconv.Atoi(strings.SplitN(res.AirDate, "-", 2)[0])
	return &store.Metadata{TMDBID: res.ID, Overview: res.Overview, Year: y, Poster: t.image("w500", res.PosterPath)}, nil
}

func (t *TMDB) image(size, path string) string {
	if path == "" {
		return ""
	}
	return t.ImageURL + size + path
}

var errNotFound = fmt.Errorf("tmdb: not found")

func (t *TMDB) get(ctx context.Context, path string, params url.Values, out any) error {
	if params == nil {
		params = url.Values{}
	}
	req, err := http.NewRequestWithContext(ctx, "GET", t.BaseURL+path, nil)
	if err != nil {
		return err
	}
	// v4 read access tokens are JWTs; v3 keys are short hex strings.
	if strings.HasPrefix(t.Key, "eyJ") {
		req.Header.Set("Authorization", "Bearer "+t.Key)
	} else {
		params.Set("api_key", t.Key)
	}
	req.URL.RawQuery = params.Encode()
	req.Header.Set("Accept", "application/json")
	res, err := t.Client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	switch {
	case res.StatusCode == http.StatusNotFound:
		return errNotFound
	case res.StatusCode != http.StatusOK:
		return fmt.Errorf("tmdb %s: HTTP %d", path, res.StatusCode)
	}
	return json.NewDecoder(res.Body).Decode(out)
}

// Video is a trailer or teaser TMDB lists for a title.
type Video struct {
	Name string
	Site string // only YouTube for now
	Key  string // the site's video id
	Type string // Trailer or Teaser
}

// Trailers lists a movie's, show's or season's trailers, best first: those
// in the context's language (store.WithLanguage), then English ones; within
// each, official trailers, then other trailers, then teasers. kind is movie
// or show; season is 0 for a whole show.
func (t *TMDB) Trailers(ctx context.Context, kind string, id int64, season int) ([]Video, error) {
	path := fmt.Sprintf("/movie/%d/videos", id)
	switch {
	case kind == "show" && season > 0:
		path = fmt.Sprintf("/tv/%d/season/%d/videos", id, season)
	case kind == "show":
		path = fmt.Sprintf("/tv/%d/videos", id)
	}
	var res struct {
		Results []struct {
			Name        string `json:"name"`
			Site        string `json:"site"`
			Key         string `json:"key"`
			Type        string `json:"type"`
			Official    bool   `json:"official"`
			PublishedAt string `json:"published_at"`
			Language    string `json:"iso_639_1"`
		} `json:"results"`
	}
	languages := "en,null"
	own := strings.Split(i18n.TMDBLanguage(store.LanguageFrom(ctx)), "-")[0]
	if own != "" && own != i18n.Source {
		languages = own + "," + languages
	}
	params := url.Values{"include_video_language": {languages}}
	if err := t.get(ctx, path, params, &res); err != nil {
		if err == errNotFound {
			return nil, nil
		}
		return nil, err
	}
	rank := func(lang, typ string, official bool) int {
		r := 0
		if own != "" && lang != own {
			r += 4
		}
		if typ != "Trailer" {
			r += 2
		}
		if !official {
			r++
		}
		return r
	}
	vids := res.Results[:0]
	for _, v := range res.Results {
		if v.Site == "YouTube" && v.Key != "" && (v.Type == "Trailer" || v.Type == "Teaser") {
			vids = append(vids, v)
		}
	}
	sort.SliceStable(vids, func(i, j int) bool {
		ri, rj := rank(vids[i].Language, vids[i].Type, vids[i].Official), rank(vids[j].Language, vids[j].Type, vids[j].Official)
		if ri != rj {
			return ri < rj
		}
		return vids[i].PublishedAt > vids[j].PublishedAt
	})
	out := []Video{}
	for _, v := range vids {
		out = append(out, Video{Name: v.Name, Site: v.Site, Key: v.Key, Type: v.Type})
	}
	return out, nil
}

// Translations looks up a movie's or show's title, description and tagline
// in every language TMDB has them in, keyed by i18n language.
func (t *TMDB) Translations(ctx context.Context, kind string, id int64) (map[string]store.Translation, error) {
	path := fmt.Sprintf("/movie/%d/translations", id)
	if kind == "show" {
		path = fmt.Sprintf("/tv/%d/translations", id)
	}
	var res struct {
		Translations []struct {
			Language string `json:"iso_639_1"`
			Country  string `json:"iso_3166_1"`
			Data     struct {
				Title    string `json:"title"`
				Name     string `json:"name"`
				Overview string `json:"overview"`
				Tagline  string `json:"tagline"`
			} `json:"data"`
		} `json:"translations"`
	}
	if err := t.get(ctx, path, nil, &res); err != nil {
		if err == errNotFound {
			return map[string]store.Translation{}, nil
		}
		return nil, err
	}
	// A language can have several countries' translations (es-ES, es-MX):
	// each field comes from the best fit that has it.
	type fit struct {
		tr                       store.Translation
		title, overview, tagline int
	}
	best := map[string]*fit{}
	for _, tr := range res.Translations {
		code, score := i18n.FromTMDB(tr.Language, tr.Country)
		if score == 0 {
			continue
		}
		f := best[code]
		if f == nil {
			f = &fit{}
			best[code] = f
		}
		take := func(value string, have *int, field *string) {
			if v := strings.TrimSpace(value); v != "" && score > *have {
				*field, *have = v, score
			}
		}
		take(cmp.Or(tr.Data.Title, tr.Data.Name), &f.title, &f.tr.Title)
		take(tr.Data.Overview, &f.overview, &f.tr.Overview)
		take(tr.Data.Tagline, &f.tagline, &f.tr.Tagline)
	}
	out := map[string]store.Translation{}
	for code, f := range best {
		if f.tr != (store.Translation{}) {
			out[code] = f.tr
		}
	}
	return out, nil
}

// SeasonTranslation looks up a season's name and description in one i18n
// language, with each episode's, keyed by episode number. Text TMDB has no
// translation for is left empty.
func (t *TMDB) SeasonTranslation(ctx context.Context, showID int64, season int, lang string) (store.Translation, map[int]store.Translation, error) {
	var res struct {
		Name     string `json:"name"`
		Overview string `json:"overview"`
		Episodes []struct {
			Number   int    `json:"episode_number"`
			Name     string `json:"name"`
			Overview string `json:"overview"`
		} `json:"episodes"`
	}
	params := url.Values{"language": {i18n.TMDBLanguage(lang)}}
	if err := t.get(ctx, fmt.Sprintf("/tv/%d/season/%d", showID, season), params, &res); err != nil {
		if err == errNotFound {
			return store.Translation{}, nil, nil
		}
		return store.Translation{}, nil, err
	}
	episodes := map[int]store.Translation{}
	for _, e := range res.Episodes {
		tr := store.Translation{Overview: strings.TrimSpace(e.Overview)}
		// Without a translated name TMDB makes one up, such as "Folge 3";
		// it's only worth showing with a translated description.
		if name := strings.TrimSpace(e.Name); name != "" && (tr.Overview != "" || !numbered(name, e.Number)) {
			tr.Title = name
		}
		if tr != (store.Translation{}) {
			episodes[e.Number] = tr
		}
	}
	return store.Translation{Title: strings.TrimSpace(res.Name), Overview: strings.TrimSpace(res.Overview)}, episodes, nil
}

// numbered reports whether an episode's name is only a word or two and its
// number, such as "Episode 3", "Folge 3" or "第 3 集".
func numbered(name string, n int) bool {
	runs := digitRuns.FindAllStringIndex(name, -1)
	if len(runs) != 1 || name[runs[0][0]:runs[0][1]] != strconv.Itoa(n) {
		return false
	}
	return len(strings.Fields(name[:runs[0][0]]+" "+name[runs[0][1]:])) <= 2
}

var digitRuns = regexp.MustCompile(`[0-9]+`)
