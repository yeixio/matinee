package metadata

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/yeixio/matinee/internal/store"
)

// TMDB looks up movies and TV on themoviedb.org. It takes either a v3 API key
// or a v4 read access token.
type TMDB struct {
	Key      string
	BaseURL  string // defaults to the public API; tests point it elsewhere
	ImageURL string
	Client   *http.Client
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
	}, nil
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
