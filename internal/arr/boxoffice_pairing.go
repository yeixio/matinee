package arr

import (
	"context"
	"net/url"
	"strconv"
	"time"
)

// What else Box Office offers Matinee, each named in its status's
// boxOfficeFeatures: "progress" (the downloads on their way, for titles
// coming soon), "editions" (other editions of a movie, on /radarr) and
// "upgrades" (a better copy, or another for a bad one). Radarr and Sonarr
// have none of them.

// BoxOfficeActivity is what Box Office is downloading, and what just
// finished or went wrong.
type BoxOfficeActivity struct {
	BoxOfficeID string                  `json:"boxOfficeId"`
	Speed       int64                   `json:"speed"` // bytes a second
	Waiting     string                  `json:"waiting,omitempty"`
	Items       []BoxOfficeActivityItem `json:"items"`
}

// BoxOfficeActivityItem is one download.
type BoxOfficeActivityItem struct {
	ID       int64  `json:"id"`
	Kind     string `json:"kind"` // movie or series
	TMDBID   int    `json:"tmdbId,omitempty"`
	TVDBID   int    `json:"tvdbId,omitempty"`
	IMDBID   string `json:"imdbId,omitempty"`
	Title    string `json:"title"`
	Year     int    `json:"year,omitempty"`
	Poster   string `json:"poster,omitempty"`
	Episodes []struct {
		Season  int `json:"seasonNumber"`
		Episode int `json:"episodeNumber"`
	} `json:"episodes,omitempty"`
	// Stage is queued, downloading, paused, waiting, checking, repairing,
	// unpacking, importing, waitingToMove, imported, failed or quarantined.
	Stage     string    `json:"stage"`
	Percent   float64   `json:"percent"`
	Size      int64     `json:"size"`
	DoneBytes int64     `json:"doneBytes"`
	ETA       int64     `json:"etaSeconds,omitempty"`
	Quality   string    `json:"quality,omitempty"`
	Edition   string    `json:"edition,omitempty"`
	Mode      string    `json:"mode,omitempty"`
	Message   string    `json:"message,omitempty"`
	AddedAt   time.Time `json:"addedAt"`
	UpdatedAt time.Time `json:"updatedAt,omitzero"`
}

// BoxOfficeActivity reads what Box Office is downloading.
func (c *Client) BoxOfficeActivity(ctx context.Context) (BoxOfficeActivity, error) {
	var a BoxOfficeActivity
	err := c.do(ctx, "GET", "/boxoffice/activity", nil, nil, &a)
	return a, err
}

// BoxOfficeEdition is one edition of a movie the indexers have.
type BoxOfficeEdition struct {
	Edition  string `json:"edition"`
	Releases int    `json:"releases"`
	Quality  string `json:"quality,omitempty"`
	Size     int64  `json:"size,omitempty"`
	Owned    bool   `json:"owned,omitempty"`
}

// BoxOfficeEditions is a movie's editions: those found, the one its copy
// is ("" when its name doesn't say), and those kept next to it.
type BoxOfficeEditions struct {
	MovieID   int                `json:"movieId"`
	HasFile   bool               `json:"hasFile"`
	Current   string             `json:"current,omitempty"`
	Alongside []string           `json:"alongside,omitempty"`
	Editions  []BoxOfficeEdition `json:"editions"`
	Errors    []string           `json:"errors,omitempty"`
}

// BoxOfficeEditions searches for a movie's editions, by TMDB id. It takes
// a few seconds: Box Office asks its indexers.
func (c *Client) BoxOfficeEditions(ctx context.Context, tmdbID int) (BoxOfficeEditions, error) {
	var e BoxOfficeEditions
	err := c.do(ctx, "GET", "/boxoffice/editions", url.Values{"tmdbId": {strconv.Itoa(tmdbID)}}, nil, &e)
	return e, err
}

// BoxOfficeAskEdition has Box Office get an edition of a movie, to replace
// its copy (mode "replace") or go alongside it ("alongside").
func (c *Client) BoxOfficeAskEdition(ctx context.Context, tmdbID int, edition, mode string) error {
	return c.do(ctx, "POST", "/boxoffice/editions", nil, map[string]any{"tmdbId": tmdbID, "edition": edition, "mode": mode}, nil)
}

// BoxOfficeUpgrade is what to ask Box Office for a title in the library.
type BoxOfficeUpgrade struct {
	TMDBID  int    `json:"tmdbId,omitempty"` // a movie
	TVDBID  int    `json:"tvdbId,omitempty"` // a show's episode, with Season and Episode
	Season  int    `json:"seasonNumber,omitempty"`
	Episode int    `json:"episodeNumber,omitempty"`
	Want    string `json:"want"` // better, or replace (the copy there is bad)
	Reason  string `json:"reason,omitempty"`
	// Problem is what someone reported with the copy, for want replace,
	// to a Box Office with "problems": wrong, language, video or audio;
	// Language is the language (ISO 639-1) the sound should be in, for
	// language; ReportedBy is who reported it.
	Problem    string `json:"problem,omitempty"`
	Language   string `json:"language,omitempty"`
	ReportedBy string `json:"reportedBy,omitempty"`
	// FileName is the reported file's name, so Box Office leaves its copy
	// alone when it isn't that file (the title matched wrong here).
	FileName string `json:"fileName,omitempty"`
}

// BoxOfficeAskUpgrade has Box Office look for a better copy, or another
// one for a bad one.
func (c *Client) BoxOfficeAskUpgrade(ctx context.Context, u BoxOfficeUpgrade) error {
	return c.do(ctx, "POST", "/boxoffice/upgrade", nil, u, nil)
}
