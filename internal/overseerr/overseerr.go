// Package overseerr talks to Overseerr, the app people run next to their
// media server to discover titles and ask for them. Jellyseerr serves the
// same API, so it works too.
//
// Matinee uses it for the Discover tab: trending and popular titles,
// recommendations based on what someone watched, and asking for a title,
// which Overseerr then approves and hands to Radarr or Sonarr by its own rules.
//
// Everything Matinee needs from Overseerr goes through Client, so the rest
// of the server doesn't know its API.
package overseerr

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/yeixio/matinee/internal/arr"
)

// Config is how Matinee reaches Overseerr, set by an admin. The API key
// stays on the server.
type Config struct {
	URL    string `json:"url"`
	APIKey string `json:"apiKey"`
	// RequestAs is the Overseerr user that requests are made as for people
	// without an Overseerr account of the same name; 0 means the API key's
	// owner.
	RequestAs int `json:"requestAs"`
}

// Client calls Overseerr's API (v1).
type Client struct {
	base *url.URL
	key  string
	http *http.Client
}

// ErrNotFound means Overseerr doesn't know the title.
var ErrNotFound = errors.New("not found")

// Error is a failed call, with a message fit to show an admin.
type Error struct {
	Status  int
	Message string
}

func (e *Error) Error() string { return e.Message }

func New(c Config) (*Client, error) {
	raw, err := arr.NormalizeURL(c.URL)
	if err != nil {
		return nil, err
	}
	u, _ := url.Parse(raw)
	if strings.TrimSpace(c.APIKey) == "" {
		return nil, errors.New("enter the API key")
	}
	return &Client{
		base: u, key: strings.TrimSpace(c.APIKey),
		http: &http.Client{
			Timeout: 20 * time.Second,
			// The API key goes in a header that redirects would carry along,
			// possibly to another host, so redirects aren't followed.
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}, nil
}

// URL is Overseerr's address.
func (c *Client) URL() string { return c.base.String() }

// escape encodes a query value with %20 for spaces: Overseerr's request
// checks turn down searches with a +.
func escape(v url.Values) string {
	return strings.ReplaceAll(v.Encode(), "+", "%20")
}

func (c *Client) do(ctx context.Context, method, path string, query url.Values, body, out any) error {
	u := *c.base
	u.Path += "/api/v1" + path
	u.RawQuery = escape(query)
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), r)
	if err != nil {
		return err
	}
	req.Header.Set("X-Api-Key", c.key)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.http.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err // the URL is in the message already, without it
		}
		return &Error{Message: fmt.Sprintf("couldn't reach Overseerr at %s: %v", c.base.Redacted(), err)}
	}
	defer res.Body.Close()
	switch {
	case res.StatusCode >= 300 && res.StatusCode < 400:
		return &Error{Status: res.StatusCode, Message: fmt.Sprintf("Overseerr at %s redirects to %s; try that address", c.base.Redacted(), res.Header.Get("Location"))}
	case res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden:
		if path == "/request" {
			return &Error{Status: res.StatusCode, Message: "Overseerr: " + errorMessage(res)}
		}
		return &Error{Status: res.StatusCode, Message: "Overseerr didn't accept the API key"}
	case res.StatusCode == http.StatusNotFound:
		return ErrNotFound
	case res.StatusCode >= 300:
		return &Error{Status: res.StatusCode, Message: "Overseerr: " + errorMessage(res)}
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 16<<20)).Decode(out); err != nil {
		if strings.HasPrefix(res.Header.Get("Content-Type"), "text/html") {
			// A web page where the API should be: usually the wrong address or port.
			return &Error{Message: fmt.Sprintf("%s doesn't look like Overseerr's address", c.base.Redacted())}
		}
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	return nil
}

// errorMessage pulls the reason out of an error response: {"message": …}.
func errorMessage(res *http.Response) string {
	b, _ := io.ReadAll(io.LimitReader(res.Body, 64<<10))
	var one struct{ Message string }
	if json.Unmarshal(b, &one) == nil && one.Message != "" {
		return one.Message
	}
	return "HTTP " + strconv.Itoa(res.StatusCode)
}

// Status is what Overseerr says about itself.
type Status struct {
	Version string `json:"version"`
}

// User is an Overseerr account.
type User struct {
	ID               int    `json:"id"`
	Email            string `json:"email,omitempty"`
	Username         string `json:"username,omitempty"`
	PlexUsername     string `json:"plexUsername,omitempty"`
	JellyfinUsername string `json:"jellyfinUsername,omitempty"`
	DisplayName      string `json:"displayName,omitempty"`
}

// Name is how the user shows up in Overseerr.
func (u User) Name() string {
	for _, n := range []string{u.DisplayName, u.Username, u.PlexUsername, u.JellyfinUsername, u.Email} {
		if n != "" {
			return n
		}
	}
	return "user " + strconv.Itoa(u.ID)
}

// Matches says whether name is one of this user's names, or their email
// before the @.
func (u User) Matches(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	local, _, _ := strings.Cut(u.Email, "@")
	for _, n := range []string{u.DisplayName, u.Username, u.PlexUsername, u.JellyfinUsername, u.Email, local} {
		if n != "" && strings.EqualFold(n, name) {
			return true
		}
	}
	return false
}

// Status checks the address and key reach Overseerr, and returns the
// key's owner.
func (c *Client) Status(ctx context.Context) (Status, User, error) {
	var s Status
	var me User
	err := c.do(ctx, "GET", "/status", nil, nil, &s)
	if errors.Is(err, ErrNotFound) || err == nil && s.Version == "" {
		return s, me, &Error{Message: fmt.Sprintf("%s doesn't look like Overseerr's address", c.base.Redacted())}
	}
	if err != nil {
		return s, me, err
	}
	// The status is public; this checks the key.
	err = c.do(ctx, "GET", "/auth/me", nil, nil, &me)
	return s, me, err
}

// Users lists Overseerr's accounts.
func (c *Client) Users(ctx context.Context) ([]User, error) {
	var page struct {
		Results []User `json:"results"`
	}
	err := c.do(ctx, "GET", "/user", url.Values{"take": {"500"}, "skip": {"0"}}, nil, &page)
	if page.Results == nil {
		page.Results = []User{}
	}
	return page.Results, err
}

// Media statuses, as Overseerr tracks them.
const (
	StatusUnknown    = 1
	StatusPending    = 2 // asked for, waiting for approval
	StatusProcessing = 3 // approved, on its way
	StatusPartial    = 4 // some seasons are available
	StatusAvailable  = 5
)

// Title is a movie or a show as Overseerr lists it.
type Title struct {
	Kind     string  `json:"kind"` // movie or show
	TMDBID   int     `json:"tmdbId"`
	Title    string  `json:"title"`
	Year     int     `json:"year,omitempty"`
	Overview string  `json:"overview,omitempty"`
	Poster   string  `json:"poster,omitempty"`
	Backdrop string  `json:"backdrop,omitempty"`
	Rating   float64 `json:"rating,omitempty"` // TMDB's, out of 10
	// MediaStatus is where Overseerr has it (see the Status constants); 0
	// when nobody has asked for it.
	MediaStatus int `json:"-"`
}

type result struct {
	ID           int     `json:"id"`
	MediaType    string  `json:"mediaType"`
	Title        string  `json:"title"`
	Name         string  `json:"name"`
	ReleaseDate  string  `json:"releaseDate"`
	FirstAirDate string  `json:"firstAirDate"`
	Overview     string  `json:"overview"`
	PosterPath   string  `json:"posterPath"`
	BackdropPath string  `json:"backdropPath"`
	VoteAverage  float64 `json:"voteAverage"`
	MediaInfo    *struct {
		Status int `json:"status"`
	} `json:"mediaInfo"`
}

const imageBase = "https://image.tmdb.org/t/p/"

func image(size, path string) string {
	if path == "" {
		return ""
	}
	return imageBase + size + path
}

// title reads a result; ok is false for people, who also show up in
// trending and search.
func (r result) title(kind string) (Title, bool) {
	kind = map[string]string{"movie": "movie", "tv": "show"}[cmp.Or(r.MediaType, kind)]
	if kind == "" || r.ID == 0 {
		return Title{}, false
	}
	t := Title{
		Kind: kind, TMDBID: r.ID, Title: cmp.Or(r.Title, r.Name), Overview: r.Overview,
		Poster: image("w342", r.PosterPath), Backdrop: image("w780", r.BackdropPath), Rating: r.VoteAverage,
	}
	if d := cmp.Or(r.ReleaseDate, r.FirstAirDate); len(d) >= 4 {
		t.Year, _ = strconv.Atoi(d[:4])
	}
	if r.MediaInfo != nil {
		t.MediaStatus = r.MediaInfo.Status
	}
	return t, true
}

// list reads a page of results. kind is "movie" or "tv" for lists that
// leave each result's type out.
func (c *Client) list(ctx context.Context, path, kind string, query url.Values) ([]Title, error) {
	var page struct {
		Results []result `json:"results"`
	}
	if query == nil {
		query = url.Values{}
	}
	query.Set("page", "1")
	if err := c.do(ctx, "GET", path, query, nil, &page); err != nil {
		return nil, err
	}
	out := []Title{}
	for _, r := range page.Results {
		if t, ok := r.title(kind); ok {
			out = append(out, t)
		}
	}
	return out, nil
}

// Lists Overseerr's discover pages offer, by the name Matinee uses.
var Lists = map[string]struct{ Path, Kind string }{
	"trending":        {"/discover/trending", ""},
	"popular-movies":  {"/discover/movies", "movie"},
	"popular-shows":   {"/discover/tv", "tv"},
	"upcoming-movies": {"/discover/movies/upcoming", "movie"},
	"upcoming-shows":  {"/discover/tv/upcoming", "tv"},
}

// Discover lists one of Lists.
func (c *Client) Discover(ctx context.Context, list string) ([]Title, error) {
	l, ok := Lists[list]
	if !ok {
		return nil, ErrNotFound
	}
	return c.list(ctx, l.Path, l.Kind, nil)
}

func apiKind(kind string) string {
	if kind == "show" {
		return "tv"
	}
	return "movie"
}

// Recommendations lists titles like a movie or show (kind), by its TMDB id.
func (c *Client) Recommendations(ctx context.Context, kind string, tmdbID int) ([]Title, error) {
	return c.list(ctx, "/"+apiKind(kind)+"/"+strconv.Itoa(tmdbID)+"/recommendations", apiKind(kind), nil)
}

// Search finds movies and shows by name.
func (c *Client) Search(ctx context.Context, q string) ([]Title, error) {
	return c.list(ctx, "/search", "", url.Values{"query": {q}})
}

// Lookup is a movie or show by its TMDB id.
func (c *Client) Lookup(ctx context.Context, kind string, tmdbID int) (Title, []int, error) {
	var r struct {
		result
		Seasons []struct {
			SeasonNumber int `json:"seasonNumber"`
		} `json:"seasons"`
	}
	if err := c.do(ctx, "GET", "/"+apiKind(kind)+"/"+strconv.Itoa(tmdbID), nil, nil, &r); err != nil {
		return Title{}, nil, err
	}
	t, _ := r.title(apiKind(kind))
	t.TMDBID = tmdbID
	var seasons []int
	for _, s := range r.Seasons {
		if s.SeasonNumber > 0 { // not specials
			seasons = append(seasons, s.SeasonNumber)
		}
	}
	return t, seasons, nil
}

// Request asks for a movie, or every season of a show, as an Overseerr
// user (0 for the key's owner). Overseerr approves it or not by that
// user's permissions, and returns where the request stands.
func (c *Client) Request(ctx context.Context, kind string, tmdbID int, seasons []int, userID int) (int, error) {
	body := map[string]any{"mediaType": apiKind(kind), "mediaId": tmdbID}
	if kind == "show" {
		if len(seasons) == 0 {
			return 0, &Error{Message: "Overseerr has no seasons of this show to ask for"}
		}
		body["seasons"] = seasons
	}
	if userID != 0 {
		body["userId"] = userID
	}
	var out struct {
		Status int `json:"status"` // the request's: 1 pending, 2 approved, 3 declined
	}
	err := c.do(ctx, "POST", "/request", nil, body, &out)
	return out.Status, err
}

// Request states, as Overseerr returns them.
const (
	RequestPending  = 1
	RequestApproved = 2
	RequestDeclined = 3
)
