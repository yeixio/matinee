// Package podcast follows podcasts: it reads shows' RSS feeds, keeps their
// episodes in a podcast library, and finds shows to follow in Podcast Index,
// the open podcast directory (see index.go).
//
// A show's episodes stay where the show publishes them. The server plays
// them from there, passing them through to the apps (see
// stream.ServeRemote), so a podcast library needs no folder of its own.
package podcast

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Feed is what a show's RSS feed says about it and its episodes.
type Feed struct {
	Title       string
	Description string
	Author      string
	Link        string // the show's website
	Image       string
	Language    string
	Categories  []string
	Episodes    []Episode
}

// Episode is one episode in a feed. Only episodes with audio are kept.
type Episode struct {
	GUID        string
	Title       string
	Description string
	URL         string // the audio file
	Type        string // its MIME type, such as audio/mpeg
	Length      int64  // its size in bytes, when the feed says
	Published   time.Time
	DurationMs  int64
	Season      int
	Number      int
	Image       string
	Link        string
	// Kind is full, trailer or bonus (itunes:episodeType).
	Kind string
}

// maxFeedBytes is how big a feed may be. Shows with thousands of episodes
// run to a few megabytes.
const maxFeedBytes = 32 << 20

// ErrNotFeed means the address answered with something that isn't a
// podcast feed, such as a web page.
var ErrNotFeed = errors.New("that isn't a podcast feed")

// Fetched is a feed as fetched: nil Feed with NotModified when it hasn't
// changed since the ETag or Last-Modified given.
type Fetched struct {
	Feed         *Feed
	NotModified  bool
	ETag         string
	LastModified string
	// URL is where the feed is now, after redirects that moved it for good.
	URL string
}

// Fetch gets a feed, asking only for changes since etag or lastModified.
func Fetch(ctx context.Context, client *http.Client, feedURL, etag, lastModified string) (Fetched, error) {
	out := Fetched{URL: feedURL}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, feedURL, nil)
	if err != nil {
		return out, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/rss+xml, application/xml;q=0.9, text/xml;q=0.8, */*;q=0.5")
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	if lastModified != "" {
		req.Header.Set("If-Modified-Since", lastModified)
	}
	moved := ""
	c := *client
	c.CheckRedirect = func(r *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("too many redirects")
		}
		// Only a redirect that moved the feed for good changes its address.
		if resp := r.Response; resp != nil && (resp.StatusCode == http.StatusMovedPermanently || resp.StatusCode == http.StatusPermanentRedirect) && len(via) == 1 {
			moved = r.URL.String()
		}
		return nil
	}
	resp, err := c.Do(req)
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()
	if moved != "" {
		out.URL = moved
	}
	if resp.StatusCode == http.StatusNotModified {
		out.NotModified, out.ETag, out.LastModified = true, etag, lastModified
		return out, nil
	}
	if resp.StatusCode != http.StatusOK {
		return out, fmt.Errorf("the feed answered %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxFeedBytes+1))
	if err != nil {
		return out, err
	}
	if len(body) > maxFeedBytes {
		return out, errors.New("the feed is too big")
	}
	f, err := Parse(body)
	if err != nil {
		return out, err
	}
	out.Feed, out.ETag, out.LastModified = f, resp.Header.Get("ETag"), resp.Header.Get("Last-Modified")
	return out, nil
}

const userAgent = "Matinee/1.0 (+https://matinee.yeix.io)"

const (
	nsITunes  = "http://www.itunes.com/dtds/podcast-1.0.dtd"
	nsContent = "http://purl.org/rss/1.0/modules/content/"
	nsMedia   = "http://search.yahoo.com/mrss/"
)

type rssImage struct {
	URL  string `xml:"url"`
	Href string `xml:"href,attr"`
}

type rssCategory struct {
	Text string        `xml:"text,attr"`
	Sub  []rssCategory `xml:"http://www.itunes.com/dtds/podcast-1.0.dtd category"`
}

type rssDoc struct {
	XMLName xml.Name
	Channel struct {
		Title       string   `xml:"title"`
		Link        []string `xml:"link"`
		Description string   `xml:"description"`
		Language    string   `xml:"language"`
		// image also catches itunes:image, so either may land in either.
		Image         rssImage      `xml:"image"`
		ITunesImage   rssImage      `xml:"http://www.itunes.com/dtds/podcast-1.0.dtd image"`
		ITunesAuthor  string        `xml:"http://www.itunes.com/dtds/podcast-1.0.dtd author"`
		ITunesSummary string        `xml:"http://www.itunes.com/dtds/podcast-1.0.dtd summary"`
		Categories    []rssCategory `xml:"http://www.itunes.com/dtds/podcast-1.0.dtd category"`
		Items         []rssItem     `xml:"item"`
	} `xml:"channel"`
}

type rssItem struct {
	Title       []string `xml:"title"`
	GUID        string   `xml:"guid"`
	Link        string   `xml:"link"`
	Description string   `xml:"description"`
	Encoded     string   `xml:"http://purl.org/rss/1.0/modules/content/ encoded"`
	Summary     string   `xml:"http://www.itunes.com/dtds/podcast-1.0.dtd summary"`
	PubDate     string   `xml:"pubDate"`
	Enclosure   []struct {
		URL    string `xml:"url,attr"`
		Type   string `xml:"type,attr"`
		Length string `xml:"length,attr"`
	} `xml:"enclosure"`
	Media []struct {
		URL      string `xml:"url,attr"`
		Type     string `xml:"type,attr"`
		FileSize string `xml:"fileSize,attr"`
	} `xml:"http://search.yahoo.com/mrss/ content"`
	Duration    string   `xml:"http://www.itunes.com/dtds/podcast-1.0.dtd duration"`
	Season      string   `xml:"http://www.itunes.com/dtds/podcast-1.0.dtd season"`
	Episode     string   `xml:"http://www.itunes.com/dtds/podcast-1.0.dtd episode"`
	EpisodeType string   `xml:"http://www.itunes.com/dtds/podcast-1.0.dtd episodeType"`
	Image       rssImage `xml:"http://www.itunes.com/dtds/podcast-1.0.dtd image"`
}

// Parse reads an RSS podcast feed.
func Parse(data []byte) (*Feed, error) {
	var doc rssDoc
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = false
	dec.Entity = xml.HTMLEntity
	dec.CharsetReader = charsetReader
	if err := dec.Decode(&doc); err != nil || doc.XMLName.Local != "rss" {
		return nil, ErrNotFeed
	}
	ch := doc.Channel
	f := &Feed{
		Title:       clean(ch.Title),
		Description: Text(first(ch.Description, ch.ITunesSummary)),
		Author:      clean(ch.ITunesAuthor),
		Language:    strings.ToLower(strings.TrimSpace(ch.Language)),
		Image:       strings.TrimSpace(first(ch.ITunesImage.Href, ch.Image.Href, ch.Image.URL)),
	}
	for _, l := range ch.Link {
		// atom:link elements share the name and are empty; the RSS link has text.
		if l = strings.TrimSpace(l); l != "" {
			f.Link = l
			break
		}
	}
	seen := map[string]bool{}
	for _, c := range ch.Categories {
		for _, name := range append([]string{c.Text}, subTexts(c.Sub)...) {
			if name = html.UnescapeString(strings.TrimSpace(name)); name != "" && !seen[name] {
				seen[name] = true
				f.Categories = append(f.Categories, name)
			}
		}
	}
	if f.Title == "" {
		return nil, ErrNotFeed
	}
	guids := map[string]bool{}
	for _, it := range ch.Items {
		e, ok := episode(it)
		if !ok || guids[e.GUID] {
			continue
		}
		guids[e.GUID] = true
		f.Episodes = append(f.Episodes, e)
	}
	return f, nil
}

func subTexts(cs []rssCategory) []string {
	var out []string
	for _, c := range cs {
		out = append(out, c.Text)
	}
	return out
}

func episode(it rssItem) (Episode, bool) {
	e := Episode{Link: strings.TrimSpace(it.Link), Kind: strings.ToLower(strings.TrimSpace(it.EpisodeType))}
	for _, t := range it.Title {
		if t = clean(t); t != "" {
			e.Title = t
			break
		}
	}
	for _, enc := range it.Enclosure {
		if u := strings.TrimSpace(enc.URL); u != "" && isAudio(enc.Type, u) {
			e.URL, e.Type = u, audioType(enc.Type, u)
			e.Length, _ = strconv.ParseInt(strings.TrimSpace(enc.Length), 10, 64)
			break
		}
	}
	if e.URL == "" {
		for _, m := range it.Media {
			if u := strings.TrimSpace(m.URL); u != "" && isAudio(m.Type, u) {
				e.URL, e.Type = u, audioType(m.Type, u)
				e.Length, _ = strconv.ParseInt(strings.TrimSpace(m.FileSize), 10, 64)
				break
			}
		}
	}
	if e.URL == "" {
		return e, false
	}
	if u, err := url.Parse(e.URL); err != nil || u.Scheme != "http" && u.Scheme != "https" || u.Host == "" {
		return e, false
	}
	if e.Length < 0 {
		e.Length = 0
	}
	e.GUID = strings.TrimSpace(it.GUID)
	if e.GUID == "" {
		e.GUID = e.URL
	}
	if e.Title == "" {
		e.Title = "Episode"
	}
	// Show notes are often longer in content:encoded, with links and lists.
	e.Description = Text(first(it.Encoded, it.Description, it.Summary))
	e.Published = parseDate(it.PubDate)
	e.DurationMs = parseDuration(it.Duration)
	e.Season, _ = strconv.Atoi(strings.TrimSpace(it.Season))
	e.Number, _ = strconv.Atoi(strings.TrimSpace(it.Episode))
	e.Image = strings.TrimSpace(it.Image.Href)
	if e.Kind != "trailer" && e.Kind != "bonus" {
		e.Kind = "full"
	}
	return e, true
}

// isAudio says whether an enclosure is a sound file. Feeds don't always say
// what type it is, so the file's extension counts too.
func isAudio(typ, u string) bool {
	typ = strings.ToLower(strings.TrimSpace(typ))
	if strings.HasPrefix(typ, "audio/") {
		return true
	}
	if typ != "" && typ != "application/octet-stream" && typ != "binary/octet-stream" {
		return false
	}
	_, ok := audioExt[extOf(u)]
	return ok
}

var audioExt = map[string]string{
	".mp3": "audio/mpeg", ".m4a": "audio/mp4", ".aac": "audio/aac", ".ogg": "audio/ogg",
	".opus": "audio/ogg", ".oga": "audio/ogg", ".wav": "audio/wav", ".flac": "audio/flac",
}

func audioType(typ, u string) string {
	typ = strings.ToLower(strings.TrimSpace(typ))
	if strings.HasPrefix(typ, "audio/") {
		// audio/mp3 and audio/x-m4a are common and not quite right.
		switch typ {
		case "audio/mp3", "audio/mpeg3", "audio/x-mp3", "audio/x-mpeg":
			return "audio/mpeg"
		case "audio/x-m4a", "audio/m4a":
			return "audio/mp4"
		}
		return typ
	}
	if t, ok := audioExt[extOf(u)]; ok {
		return t
	}
	return "audio/mpeg"
}

func extOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	p := strings.ToLower(u.Path)
	if i := strings.LastIndex(p, "."); i >= 0 && !strings.Contains(p[i:], "/") {
		return p[i:]
	}
	return ""
}

var dateLayouts = []string{
	time.RFC1123Z, time.RFC1123, "Mon, 2 Jan 2006 15:04:05 -0700", "Mon, 2 Jan 2006 15:04:05 MST",
	"Mon, 02 Jan 2006 15:04 -0700", "Mon, 2 Jan 2006 15:04 -0700", "2 Jan 2006 15:04:05 -0700", "02 Jan 2006 15:04:05 MST",
	"Mon, 2 Jan 06 15:04:05 -0700", "Mon, 2 January 2006 15:04:05 -0700", time.RFC3339, "2006-01-02T15:04:05", "2006-01-02",
}

// parseDate reads an RSS date. Feeds get these wrong in many ways: day
// names that don't match, missing seconds, long month names.
func parseDate(s string) time.Time {
	s = strings.Join(strings.Fields(s), " ")
	if s == "" {
		return time.Time{}
	}
	for _, l := range dateLayouts {
		if t, err := time.Parse(l, s); err == nil {
			return t.UTC()
		}
	}
	// Drop a day name that doesn't parse, like "Thurs,".
	if _, rest, ok := strings.Cut(s, ", "); ok {
		for _, l := range dateLayouts {
			if i := strings.Index(l, ", "); i >= 0 {
				if t, err := time.Parse(l[i+2:], rest); err == nil {
					return t.UTC()
				}
			}
		}
	}
	return time.Time{}
}

// parseDuration reads itunes:duration: seconds, or MM:SS, or HH:MM:SS.
func parseDuration(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	var total float64
	for _, p := range strings.Split(s, ":") {
		n, err := strconv.ParseFloat(strings.TrimSpace(p), 64)
		if err != nil || n < 0 {
			return 0
		}
		total = total*60 + n
	}
	return int64(total * 1000)
}

func first(vs ...string) string {
	for _, v := range vs {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func clean(s string) string { return strings.Join(strings.Fields(html.UnescapeString(s)), " ") }

var (
	breaks = regexp.MustCompile(`(?i)<\s*(br|/p|/div|/li|/h[1-6])\s*/?>`)
	items  = regexp.MustCompile(`(?i)<\s*li[^>]*>`)
	tags   = regexp.MustCompile(`<[^>]*>`)
	blank  = regexp.MustCompile(`\n{3,}`)
)

// Text turns show notes, often HTML, into plain text with paragraphs.
func Text(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = breaks.ReplaceAllString(s, "\n")
	s = items.ReplaceAllString(s, "• ")
	s = tags.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.Join(strings.Fields(l), " ")
	}
	s = blank.ReplaceAllString(strings.Join(lines, "\n"), "\n\n")
	return strings.TrimSpace(s)
}

// charsetReader reads the few encodings feeds use besides UTF-8: Latin-1
// and Windows-1252, read alike (their difference is a few punctuation marks).
func charsetReader(label string, in io.Reader) (io.Reader, error) {
	switch strings.ToLower(strings.TrimSpace(label)) {
	case "utf-8", "utf8", "us-ascii", "ascii":
		return in, nil
	case "iso-8859-1", "iso8859-1", "latin1", "latin-1", "windows-1252", "cp1252":
		b, err := io.ReadAll(in)
		if err != nil {
			return nil, err
		}
		out := make([]byte, 0, len(b)+len(b)/8)
		for _, c := range b {
			out = utf8.AppendRune(out, rune(c))
		}
		return bytes.NewReader(out), nil
	}
	return nil, fmt.Errorf("unsupported encoding %q", label)
}
