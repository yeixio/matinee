package arr

import (
	"context"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Box Office (github.com/yeixio/boxoffice) answers as Radarr and Sonarr,
// and adds a feed of what happens to its downloads, so Matinee can tell
// people when what they asked for is on its way or ready.

// BoxOfficeNotification is one entry in Box Office's feed.
type BoxOfficeNotification struct {
	ID    int64  `json:"id"`
	Event string `json:"event"` // download.grabbed, download.imported, download.completed, download.failed, download.quarantined…
	Level string `json:"level"` // info, warning, critical
	// Audience is "everyone" (the people who asked for the title, and
	// admins) or "admins".
	Audience string          `json:"audience"`
	Title    string          `json:"title"`
	Body     string          `json:"body,omitempty"`
	Media    *BoxOfficeMedia `json:"media,omitempty"`
	At       time.Time       `json:"at"`
}

// BoxOfficeMedia is the title a notification is about.
type BoxOfficeMedia struct {
	Kind     string `json:"kind"` // movie or series
	Title    string `json:"title"`
	Year     int    `json:"year,omitempty"`
	TMDBID   int    `json:"tmdbId,omitempty"`
	TVDBID   int    `json:"tvdbId,omitempty"`
	IMDBID   string `json:"imdbId,omitempty"`
	Season   int    `json:"season,omitempty"`
	Episodes []int  `json:"episodes,omitempty"`
	Poster   string `json:"poster,omitempty"`
}

// BoxOfficeFeed is a page of the feed.
type BoxOfficeFeed struct {
	BoxOfficeID   string                  `json:"boxOfficeId"`
	Last          int64                   `json:"last"` // the newest id there is
	Notifications []BoxOfficeNotification `json:"notifications"`
}

// BoxOfficeNotifications reads Box Office's feed after an id, oldest
// first. after=-1 reads only where the feed is (Last), skipping what's
// there already.
func (c *Client) BoxOfficeNotifications(ctx context.Context, after int64) (BoxOfficeFeed, error) {
	var f BoxOfficeFeed
	err := c.do(ctx, "GET", "/boxoffice/notifications", url.Values{"after": {strconv.FormatInt(after, 10)}, "limit": {"200"}}, nil, &f)
	return f, err
}

// Box Office's Discover rows, which Matinee's Discover tab shows in place
// of Overseerr's when Box Office is connected: what's trending, popular and
// coming soon on TMDB, and titles like a given one. Its status says
// "discover" when it has them.

// BoxOfficeTitle is a title in one of Box Office's Discover rows.
type BoxOfficeTitle struct {
	Kind     string  `json:"kind"` // movie or series
	TMDBID   int     `json:"tmdbId"`
	Title    string  `json:"title"`
	Year     int     `json:"year,omitempty"`
	Overview string  `json:"overview,omitempty"`
	Poster   string  `json:"poster,omitempty"`
	Backdrop string  `json:"backdrop,omitempty"`
	Rating   float64 `json:"rating,omitempty"`
	// Status is where Box Office has it: available (on disk), partial (a
	// show with some episodes), wanted, paused, or empty when it doesn't.
	Status string `json:"status,omitempty"`
}

// BoxOfficeRow is one of Box Office's Discover rows.
type BoxOfficeRow struct {
	ID    string           `json:"id"`
	Title string           `json:"title"`
	Items []BoxOfficeTitle `json:"items"`
	More  bool             `json:"more,omitempty"`
	// Page and Pages are set on a row read by itself.
	Page  int `json:"page,omitempty"`
	Pages int `json:"pages,omitempty"`
}

// BoxOfficeDiscover is Box Office's Discover home.
type BoxOfficeDiscover struct {
	Rows []BoxOfficeRow `json:"rows"`
}

// BoxOfficeDiscoverHome reads Box Office's Discover rows.
func (c *Client) BoxOfficeDiscoverHome(ctx context.Context) (BoxOfficeDiscover, error) {
	var d BoxOfficeDiscover
	err := c.do(ctx, "GET", "/boxoffice/discover", nil, nil, &d)
	return d, err
}

// BoxOfficeDiscoverRow reads a page (from 1) of one row: trending,
// popular-movies, popular-series, upcoming-movies, upcoming-series,
// for-you, or like-{movie,series}-{tmdb id}.
func (c *Client) BoxOfficeDiscoverRow(ctx context.Context, id string, page int) (BoxOfficeRow, error) {
	var r BoxOfficeRow
	err := c.do(ctx, "GET", "/boxoffice/discover/rows/"+url.PathEscape(id), url.Values{"page": {strconv.Itoa(max(page, 1))}}, nil, &r)
	return r, err
}

// BoxOfficeDiscoverSearch finds movies and shows by name.
func (c *Client) BoxOfficeDiscoverSearch(ctx context.Context, q string) ([]BoxOfficeTitle, error) {
	out := []BoxOfficeTitle{}
	err := c.do(ctx, "GET", "/boxoffice/discover/search", url.Values{"q": {q}}, nil, &out)
	return out, err
}

// BoxOfficeRequest is a request on Box Office's Requests page, as Box
// Office has it.
type BoxOfficeRequest struct {
	ID     int64  `json:"id"`
	Kind   string `json:"kind"` // movie or series
	TMDBID int    `json:"tmdbId"`
	Title  string `json:"title"`
	// Status is pending, approved or declined, and Reply the admin's words
	// for a declined one.
	Status string `json:"status"`
	Reply  string `json:"reply,omitempty"`
	// State is how far it's got: pending, declined, searching,
	// downloading, partial, available, paused or removed.
	State string `json:"state"`
}

// BoxOfficeAsk files a request on Box Office's Requests page for the
// Matinee account named user. approved says it's to be added at once
// (Matinee lets this person add titles); profile is a quality profile, or
// 0. Box Office's rules, such as weekly limits, come back as an *Error
// with Status 422 and the words for the person in Reason.
func (c *Client) BoxOfficeAsk(ctx context.Context, user string, tmdbID int, approved bool, profile int) (BoxOfficeRequest, error) {
	var r BoxOfficeRequest
	err := c.do(ctx, "POST", "/boxoffice/requests", nil, map[string]any{"user": user, "tmdbId": tmdbID, "approved": approved, "qualityProfileId": profile}, &r)
	return r, err
}

// BoxOfficeRequests is where requests on Box Office stand, by id. Ones Box
// Office no longer has are left out.
func (c *Client) BoxOfficeRequests(ctx context.Context, ids []int64) ([]BoxOfficeRequest, error) {
	s := make([]string, len(ids))
	for i, id := range ids {
		s[i] = strconv.FormatInt(id, 10)
	}
	var out []BoxOfficeRequest
	err := c.do(ctx, "GET", "/boxoffice/requests", url.Values{"ids": {strings.Join(s, ",")}}, nil, &out)
	return out, err
}

// BoxOfficeApprove approves a request on Box Office, with a quality
// profile or 0 for Box Office's.
func (c *Client) BoxOfficeApprove(ctx context.Context, id int64, profile int) (BoxOfficeRequest, error) {
	var r BoxOfficeRequest
	err := c.do(ctx, "POST", "/boxoffice/requests/"+strconv.FormatInt(id, 10)+"/approve", nil, map[string]any{"qualityProfileId": profile}, &r)
	return r, err
}

// BoxOfficeDecline declines a request on Box Office.
func (c *Client) BoxOfficeDecline(ctx context.Context, id int64, reply string) (BoxOfficeRequest, error) {
	var r BoxOfficeRequest
	err := c.do(ctx, "POST", "/boxoffice/requests/"+strconv.FormatInt(id, 10)+"/decline", nil, map[string]any{"reply": reply}, &r)
	return r, err
}

// BoxOfficeWithdraw takes back a request on Box Office that's still
// waiting there.
func (c *Client) BoxOfficeWithdraw(ctx context.Context, id int64) error {
	return c.do(ctx, "DELETE", "/boxoffice/requests/"+strconv.FormatInt(id, 10), nil, nil, nil)
}
