package metadata

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// CoverOption is a cover an admin can pick for an album or book.
type CoverOption struct {
	URL    string `json:"url"`   // the full-size image
	Thumb  string `json:"thumb"` // a small one, to show in the list
	Source string `json:"source"`
	Title  string `json:"title"`            // what the source calls it
	Detail string `json:"detail,omitempty"` // which edition: year, country, format
}

// Covers finds covers to pick from: every edition's on the Cover Art
// Archive, and Apple's, Deezer's and Open Library's store and catalog
// covers. None of them needs a key. Each source answers what it can; one
// that's down leaves the others.
type Covers struct {
	MusicBrainz *MusicBrainz // for albums; nil leaves out the Cover Art Archive
	OpenLibrary *OpenLibrary // for books; nil leaves out Open Library
	ITunesURL   string       // defaults to the public services; tests point them elsewhere
	DeezerURL   string
	UserAgent   string
	Client      *http.Client
}

func NewCovers(version string, mb *MusicBrainz, ol *OpenLibrary) *Covers {
	if mb == nil {
		mb = NewMusicBrainz(version)
	}
	if ol == nil {
		ol = NewOpenLibrary(version)
	}
	return &Covers{
		MusicBrainz: mb,
		OpenLibrary: ol,
		ITunesURL:   "https://itunes.apple.com",
		DeezerURL:   "https://api.deezer.com",
		UserAgent:   "Matinee/" + version + " ( https://github.com/yeixio/matinee )",
		Client:      &http.Client{Timeout: 15 * time.Second},
	}
}

// perSource is how many covers each source offers at most.
const perSource = 12

// Find lists covers for an album (q.Kind "album", with its artist) or a
// book ("book", with its author), best sources first.
func (c *Covers) Find(ctx context.Context, q Query) []CoverOption {
	var sources []func(context.Context, Query) ([]CoverOption, error)
	switch q.Kind {
	case "album":
		sources = append(sources, c.coverArtArchive, c.appleAlbums, c.deezer)
	case "book":
		sources = append(sources, c.openLibrary, c.appleBooks)
	}
	found := make([][]CoverOption, len(sources))
	var wg sync.WaitGroup
	for i, find := range sources {
		wg.Go(func() {
			found[i], _ = find(ctx, q)
		})
	}
	wg.Wait()
	var out []CoverOption
	seen := map[string]bool{}
	for _, opts := range found {
		for i, o := range opts {
			if i == perSource {
				break
			}
			if o.URL != "" && !seen[o.URL] {
				seen[o.URL] = true
				out = append(out, o)
			}
		}
	}
	return out
}

// coverArtArchive offers the front cover of each edition of the album
// MusicBrainz has, found by the ids in its tags or the match made earlier,
// or else by searching for it.
func (c *Covers) coverArtArchive(ctx context.Context, q Query) ([]CoverOption, error) {
	m := c.MusicBrainz
	group := q.ReleaseGroupID
	if group == "" && q.ReleaseID != "" {
		var rel struct {
			ReleaseGroup releaseGroup `json:"release-group"`
		}
		if err := m.mb(ctx, "/release/"+url.PathEscape(q.ReleaseID), url.Values{"inc": {"release-groups"}}, &rel); err == nil {
			group = rel.ReleaseGroup.ID
		}
	}
	if group == "" {
		for _, title := range albumTitles(q.Title, q.Artist) {
			rg, err := m.releaseGroup(ctx, title, q.Artist)
			if err != nil {
				return nil, err
			}
			if rg != nil {
				group = rg.ID
				break
			}
		}
	}
	if group == "" {
		return nil, nil
	}
	var res struct {
		Releases []struct {
			ID             string `json:"id"`
			Title          string `json:"title"`
			Date           string `json:"date"`
			Country        string `json:"country"`
			Disambiguation string `json:"disambiguation"`
			CAA            struct {
				Front bool `json:"front"`
			} `json:"cover-art-archive"`
			Media []struct {
				Format string `json:"format"`
			} `json:"media"`
		} `json:"releases"`
	}
	if err := m.mb(ctx, "/release", url.Values{"release-group": {group}, "inc": {"media"}, "limit": {"50"}}, &res); err != nil {
		return nil, err
	}
	var out []CoverOption
	for _, r := range res.Releases {
		if !r.CAA.Front {
			continue
		}
		var detail []string
		for _, s := range []string{r.Date, r.Country, r.Disambiguation} {
			if s != "" {
				detail = append(detail, s)
			}
		}
		if len(r.Media) > 0 && r.Media[0].Format != "" {
			detail = append(detail, r.Media[0].Format)
		}
		base := m.CoverURL + "/release/" + r.ID + "/front"
		out = append(out, CoverOption{URL: base + "-1200", Thumb: base + "-250", Source: "Cover Art Archive",
			Title: r.Title, Detail: strings.Join(detail, " · ")})
	}
	return out, nil
}

// appleAlbums offers the Apple Music store's covers for albums by this name.
func (c *Covers) appleAlbums(ctx context.Context, q Query) ([]CoverOption, error) {
	return c.apple(ctx, "album", plainAlbumTitle(q.Title), q.Artist)
}

// appleBooks offers the Apple Books store's audiobook covers.
func (c *Covers) appleBooks(ctx context.Context, q Query) ([]CoverOption, error) {
	return c.apple(ctx, "audiobook", plainBookTitle(q.Title), q.Artist)
}

func (c *Covers) apple(ctx context.Context, entity, title, artist string) ([]CoverOption, error) {
	if title == "" {
		return nil, nil
	}
	var res struct {
		Results []struct {
			Collection string `json:"collectionName"`
			Artist     string `json:"artistName"`
			Artwork    string `json:"artworkUrl100"`
			Released   string `json:"releaseDate"`
		} `json:"results"`
	}
	term := strings.TrimSpace(artist + " " + title)
	params := url.Values{"term": {term}, "entity": {entity}, "media": {map[string]string{"album": "music", "audiobook": "audiobook"}[entity]}, "limit": {"15"}}
	if err := c.get(ctx, c.ITunesURL+"/search?"+params.Encode(), &res); err != nil {
		return nil, err
	}
	var out []CoverOption
	for _, r := range res.Results {
		// The artwork URL names its size, and Apple makes any size asked for.
		if !strings.Contains(r.Artwork, "100x100") {
			continue
		}
		out = append(out, CoverOption{
			URL:    strings.Replace(r.Artwork, "100x100", "1200x1200", 1),
			Thumb:  strings.Replace(r.Artwork, "100x100", "300x300", 1),
			Source: "Apple", Title: r.Collection + " · " + r.Artist, Detail: year(r.Released),
		})
	}
	return out, nil
}

// deezer offers Deezer's covers for albums by this name and artist.
func (c *Covers) deezer(ctx context.Context, q Query) ([]CoverOption, error) {
	title := plainAlbumTitle(q.Title)
	if title == "" {
		return nil, nil
	}
	query := `album:"` + strings.ReplaceAll(withoutArtist(title, q.Artist), `"`, "") + `"`
	if q.Artist != "" && !strings.EqualFold(q.Artist, "Various Artists") {
		query = `artist:"` + strings.ReplaceAll(q.Artist, `"`, "") + `" ` + query
	}
	var res struct {
		Data []struct {
			Title  string `json:"title"`
			Cover  string `json:"cover_xl"`
			Thumb  string `json:"cover_medium"`
			Artist struct {
				Name string `json:"name"`
			} `json:"artist"`
		} `json:"data"`
	}
	if err := c.get(ctx, c.DeezerURL+"/search/album?"+url.Values{"q": {query}, "limit": {"15"}}.Encode(), &res); err != nil {
		return nil, err
	}
	var out []CoverOption
	for _, d := range res.Data {
		if d.Cover != "" {
			out = append(out, CoverOption{URL: d.Cover, Thumb: d.Thumb, Source: "Deezer", Title: d.Title + " · " + d.Artist.Name})
		}
	}
	return out, nil
}

// openLibrary offers the covers of the printed editions Open Library has.
func (c *Covers) openLibrary(ctx context.Context, q Query) ([]CoverOption, error) {
	o := c.OpenLibrary
	params := url.Values{"title": {plainBookTitle(q.Title)}, "limit": {"15"}, "fields": {"title,author_name,cover_i,first_publish_year"}}
	if q.Artist != "" {
		params.Set("author", q.Artist)
	}
	var res struct {
		Docs []struct {
			Title     string   `json:"title"`
			Authors   []string `json:"author_name"`
			CoverID   int64    `json:"cover_i"`
			FirstYear int      `json:"first_publish_year"`
		} `json:"docs"`
	}
	if err := o.get(ctx, "/search.json", params, &res); err != nil {
		return nil, err
	}
	var out []CoverOption
	for _, d := range res.Docs {
		if d.CoverID == 0 {
			continue
		}
		base := fmt.Sprintf("%s/b/id/%d", o.CoverURL, d.CoverID)
		title := d.Title
		if len(d.Authors) > 0 {
			title += " · " + d.Authors[0]
		}
		detail := ""
		if d.FirstYear > 0 {
			detail = strconv.Itoa(d.FirstYear)
		}
		out = append(out, CoverOption{URL: base + "-L.jpg", Thumb: base + "-M.jpg", Source: "Open Library", Title: title, Detail: detail})
	}
	return out, nil
}

func (c *Covers) get(ctx context.Context, u string, out any) error {
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", c.UserAgent)
	req.Header.Set("Accept", "application/json")
	resp, err := c.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %s", req.URL.Host, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// year is the year at the start of a date like "2006-06-05T07:00:00Z".
func year(date string) string {
	if len(date) >= 4 {
		if _, err := strconv.Atoi(date[:4]); err == nil {
			return date[:4]
		}
	}
	return ""
}
