package metadata

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/yeixio/matinee/internal/store"
)

// MusicBrainz identifies artists and albums on musicbrainz.org, with album
// covers from the Cover Art Archive and artist photos and biographies from
// Wikidata and Wikipedia. None of them need an API key.
type MusicBrainz struct {
	BaseURL      string // defaults to the public services; tests point them elsewhere
	CoverURL     string
	WikidataURL  string
	WikipediaURL string
	UserAgent    string // MusicBrainz asks every client to name itself
	Client       *http.Client
	// Interval spaces requests to MusicBrainz, which allows one a second.
	Interval time.Duration

	mu   sync.Mutex
	last time.Time
}

func NewMusicBrainz(version string) *MusicBrainz {
	return &MusicBrainz{
		BaseURL:      "https://musicbrainz.org/ws/2",
		CoverURL:     "https://coverartarchive.org",
		WikidataURL:  "https://www.wikidata.org",
		WikipediaURL: "https://en.wikipedia.org",
		UserAgent:    "Matinee/" + version + " ( https://github.com/yeixio/matinee )",
		Client:       &http.Client{Timeout: 15 * time.Second},
		Interval:     1100 * time.Millisecond,
	}
}

func (m *MusicBrainz) Name() string    { return "musicbrainz" }
func (m *MusicBrainz) Kinds() []string { return []string{"artist", "album"} }

func (m *MusicBrainz) Lookup(ctx context.Context, q Query) (*store.Metadata, error) {
	switch q.Kind {
	case "artist":
		return m.artist(ctx, q.Title)
	case "album":
		return m.album(ctx, q)
	}
	return nil, nil
}

// minScore is how sure MusicBrainz's search must be (0-100) before we take its answer.
const minScore = 90

func (m *MusicBrainz) artist(ctx context.Context, name string) (*store.Metadata, error) {
	var res struct {
		Artists []struct {
			ID    string `json:"id"`
			Name  string `json:"name"`
			Score int    `json:"score"`
		} `json:"artists"`
	}
	params := url.Values{"query": {"artist:" + luceneQuote(name)}, "limit": {"5"}}
	if err := m.mb(ctx, "/artist", params, &res); err != nil {
		return nil, err
	}
	// Among confident matches, prefer the exact name over a near miss.
	id := ""
	for _, a := range res.Artists {
		if a.Score >= minScore && strings.EqualFold(a.Name, name) {
			id = a.ID
			break
		}
	}
	if id == "" && len(res.Artists) > 0 && res.Artists[0].Score >= minScore {
		id = res.Artists[0].ID
	}
	if id == "" {
		return nil, nil
	}
	md := &store.Metadata{MBID: id}
	// The photo and biography are extras: a failure there still keeps the match.
	if qid, err := m.wikidataID(ctx, id); err == nil && qid != "" {
		md.Poster, md.Overview = m.wikidata(ctx, qid)
	}
	return md, nil
}

func (m *MusicBrainz) album(ctx context.Context, q Query) (*store.Metadata, error) {
	// "OK Computer (Deluxe Edition) [Remastered]" is filed as "OK Computer".
	titles := []string{q.Title}
	if c := plainAlbumTitle(q.Title); c != "" && c != q.Title {
		titles = append(titles, c)
	}
	for _, title := range titles {
		rg, err := m.releaseGroup(ctx, title, q.Artist)
		if err != nil {
			return nil, err
		}
		if rg == nil {
			continue
		}
		y, _ := strconv.Atoi(strings.SplitN(rg.FirstRelease, "-", 2)[0])
		// Local covers win over this (see Store.ApplyMetadata).
		return &store.Metadata{MBID: rg.ID, Year: y, Poster: m.cover(ctx, rg.ID)}, nil
	}
	return nil, nil
}

type releaseGroup struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	Score        int    `json:"score"`
	FirstRelease string `json:"first-release-date"`
}

// releaseGroup searches for an album by its title and artist. A confident
// answer is taken, and so is a less confident one with exactly this title,
// since search scores drop for short or common titles.
func (m *MusicBrainz) releaseGroup(ctx context.Context, title, artist string) (*releaseGroup, error) {
	query := "releasegroup:" + luceneQuote(title)
	if artist != "" {
		query += " AND artist:" + luceneQuote(artist)
	}
	var res struct {
		ReleaseGroups []releaseGroup `json:"release-groups"`
	}
	if err := m.mb(ctx, "/release-group", url.Values{"query": {query}, "limit": {"5"}}, &res); err != nil {
		return nil, err
	}
	if len(res.ReleaseGroups) == 0 {
		return nil, nil
	}
	if res.ReleaseGroups[0].Score >= minScore {
		return &res.ReleaseGroups[0], nil
	}
	if artist == "" {
		return nil, nil // a title alone is too little to go on
	}
	for _, rg := range res.ReleaseGroups {
		if rg.Score >= 60 && sameName(rg.Title, title) {
			return &rg, nil
		}
	}
	return nil, nil
}

// cover returns the Cover Art Archive's front cover for an album. Not
// every album has one for the album as a whole, so when it doesn't, the
// first of its releases with a front cover is used; "" when none has one.
// If the archive can't be reached, the album's cover URL is kept, and the
// image endpoint tries it again when it's shown.
func (m *MusicBrainz) cover(ctx context.Context, rgid string) string {
	front := m.CoverURL + "/release-group/" + rgid + "/front-500"
	switch ok, err := m.exists(ctx, front); {
	case err != nil || ok:
		return front
	}
	var res struct {
		Releases []struct {
			ID  string `json:"id"`
			CAA struct {
				Front bool `json:"front"`
			} `json:"cover-art-archive"`
		} `json:"releases"`
	}
	if err := m.mb(ctx, "/release", url.Values{"release-group": {rgid}, "limit": {"50"}}, &res); err != nil {
		return ""
	}
	for _, r := range res.Releases {
		if r.CAA.Front {
			return m.CoverURL + "/release/" + r.ID + "/front-500"
		}
	}
	return ""
}

// exists asks the Cover Art Archive whether it has an image, without
// downloading it: it answers with a redirect to the file or a 404.
func (m *MusicBrainz) exists(ctx context.Context, u string) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, "HEAD", u, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("User-Agent", m.UserAgent)
	c := *m.Client
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := c.Do(req)
	if err != nil {
		return false, err
	}
	resp.Body.Close()
	switch {
	case resp.StatusCode < 400:
		return true, nil
	case resp.StatusCode == http.StatusNotFound:
		return false, nil
	}
	return false, fmt.Errorf("%s: %s", req.URL.Host, resp.Status)
}

// editionRe matches a bracketed note on an album title about its edition,
// such as "(Deluxe Edition)", "[2011 Remaster]" or "(Disc 1)".
var editionRe = regexp.MustCompile(`(?i)\s*[(\[][^)\]]*\b(deluxe|remaster(ed)?|expanded|edition|anniversary|bonus|special|explicit|version|mono|stereo|reissue|limited|collector'?s|disc|cd|disk)\b[^)\]]*[)\]]\s*$`)

// discSuffixRe matches " CD1" or " - Disc 2" at the end of an album title.
var discSuffixRe = regexp.MustCompile(`(?i)[\s-]+(cd|dis[ck])\s*\d{1,2}$`)

// plainAlbumTitle takes edition and disc notes off an album title.
func plainAlbumTitle(t string) string {
	for {
		c := discSuffixRe.ReplaceAllString(editionRe.ReplaceAllString(t, ""), "")
		if c == t {
			return strings.TrimSpace(c)
		}
		t = c
	}
}

// sameName compares titles ignoring case, punctuation and spacing.
func sameName(a, b string) bool {
	norm := func(s string) string {
		return strings.Map(func(r rune) rune {
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				return unicode.ToLower(r)
			}
			return -1
		}, s)
	}
	return norm(a) != "" && norm(a) == norm(b)
}

// wikidataID finds the artist's Wikidata entity among its MusicBrainz links.
func (m *MusicBrainz) wikidataID(ctx context.Context, mbid string) (string, error) {
	var res struct {
		Relations []struct {
			Type string `json:"type"`
			URL  struct {
				Resource string `json:"resource"`
			} `json:"url"`
		} `json:"relations"`
	}
	if err := m.mb(ctx, "/artist/"+mbid, url.Values{"inc": {"url-rels"}}, &res); err != nil {
		return "", err
	}
	for _, r := range res.Relations {
		if r.Type == "wikidata" {
			return path.Base(r.URL.Resource), nil
		}
	}
	return "", nil
}

// wikidata returns the entity's photo URL and English Wikipedia summary.
func (m *MusicBrainz) wikidata(ctx context.Context, qid string) (photo, bio string) {
	var ent struct {
		Entities map[string]struct {
			Claims map[string][]struct {
				Mainsnak struct {
					Datavalue struct {
						Value json.RawMessage `json:"value"`
					} `json:"datavalue"`
				} `json:"mainsnak"`
			} `json:"claims"`
			Sitelinks map[string]struct {
				Title string `json:"title"`
			} `json:"sitelinks"`
		} `json:"entities"`
	}
	if err := m.getJSON(ctx, m.WikidataURL+"/wiki/Special:EntityData/"+url.PathEscape(qid)+".json", &ent); err != nil {
		return "", ""
	}
	e, ok := ent.Entities[qid]
	if !ok {
		return "", ""
	}
	// P18 is Wikidata's "image" property: a file name on Wikimedia Commons.
	if images := e.Claims["P18"]; len(images) > 0 {
		var file string
		if json.Unmarshal(images[0].Mainsnak.Datavalue.Value, &file) == nil && file != "" {
			photo = "https://commons.wikimedia.org/wiki/Special:FilePath/" + url.PathEscape(file) + "?width=1000"
		}
	}
	if wp, ok := e.Sitelinks["enwiki"]; ok {
		var sum struct {
			Extract string `json:"extract"`
		}
		title := url.PathEscape(strings.ReplaceAll(wp.Title, " ", "_"))
		if m.getJSON(ctx, m.WikipediaURL+"/api/rest_v1/page/summary/"+title, &sum) == nil {
			bio = sum.Extract
		}
	}
	return photo, bio
}

// mb calls the MusicBrainz API, keeping to its rate limit.
func (m *MusicBrainz) mb(ctx context.Context, p string, params url.Values, out any) error {
	m.mu.Lock()
	wait := time.Until(m.last.Add(m.Interval))
	if wait > 0 {
		select {
		case <-ctx.Done():
			m.mu.Unlock()
			return ctx.Err()
		case <-time.After(wait):
		}
	}
	m.last = time.Now()
	m.mu.Unlock()
	params.Set("fmt", "json")
	return m.getJSON(ctx, m.BaseURL+p+"?"+params.Encode(), out)
}

func (m *MusicBrainz) getJSON(ctx context.Context, u string, out any) error {
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", m.UserAgent)
	req.Header.Set("Accept", "application/json")
	resp, err := m.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %s", req.URL.Host, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// luceneQuote makes s one exact phrase in a MusicBrainz search query.
func luceneQuote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}
