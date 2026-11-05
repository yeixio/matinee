package arr

import (
	"context"
	"net/url"
	"strconv"
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
