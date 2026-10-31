package podcast

import (
	"strings"
	"testing"
	"time"
)

const sampleFeed = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:itunes="http://www.itunes.com/dtds/podcast-1.0.dtd" xmlns:atom="http://www.w3.org/2005/Atom">
<channel>
<atom:link href="https://example.com/feed.xml" rel="self" type="application/rss+xml"/>
<title>Night  Shift &amp; Radio</title>
<link>https://example.com/night</link>
<description>Stories.</description>
<itunes:author>Jane Doe</itunes:author>
<image><url>https://example.com/small.jpg</url></image>
<itunes:image href="https://example.com/big.jpg"/>
<item>
  <title>One</title>
  <guid>a</guid>
  <pubDate>Thurs, 29 Oct 2026 08:00 +0000</pubDate>
  <itunes:duration>1:02:03</itunes:duration>
  <itunes:season>2</itunes:season>
  <itunes:episode>5</itunes:episode>
  <itunes:episodeType>bonus</itunes:episodeType>
  <enclosure url="https://cdn.example.com/1.m4a?x=1" type="" length="10"/>
</item>
<item>
  <title>No sound</title>
  <enclosure url="https://cdn.example.com/1.pdf" type="application/pdf" length="10"/>
</item>
<item>
  <title>Same</title>
  <guid>a</guid>
  <enclosure url="https://cdn.example.com/2.mp3" type="audio/mp3"/>
</item>
<item>
  <title>No guid</title>
  <pubDate>Wed, 28 Oct 2026 08:00:00 GMT</pubDate>
  <itunes:duration>754</itunes:duration>
  <enclosure url="https://cdn.example.com/3.mp3" type="audio/mp3"/>
</item>
</channel>
</rss>`

func TestParse(t *testing.T) {
	f, err := Parse([]byte(sampleFeed))
	if err != nil {
		t.Fatal(err)
	}
	if f.Title != "Night Shift & Radio" || f.Author != "Jane Doe" || f.Link != "https://example.com/night" || f.Image != "https://example.com/big.jpg" {
		t.Fatalf("feed: %+v", f)
	}
	if len(f.Episodes) != 2 {
		t.Fatalf("episodes: %+v", f.Episodes)
	}
	e := f.Episodes[0]
	if e.GUID != "a" || e.Type != "audio/mp4" || e.DurationMs != 3723_000 || e.Season != 2 || e.Number != 5 || e.Kind != "bonus" ||
		!e.Published.Equal(time.Date(2026, 10, 29, 8, 0, 0, 0, time.UTC)) {
		t.Fatalf("first: %+v", e)
	}
	e = f.Episodes[1]
	if e.GUID != e.URL || e.Type != "audio/mpeg" || e.DurationMs != 754_000 || e.Kind != "full" || e.Published.IsZero() {
		t.Fatalf("second: %+v", e)
	}
	if _, err := Parse([]byte("<html><body>hi</body></html>")); err != ErrNotFeed {
		t.Fatalf("a web page: %v", err)
	}
}

func TestLatin1(t *testing.T) {
	feed := "<?xml version=\"1.0\" encoding=\"ISO-8859-1\"?><rss><channel><title>Caf\xe9</title></channel></rss>"
	f, err := Parse([]byte(feed))
	if err != nil || f.Title != "Café" {
		t.Fatalf("%+v %v", f, err)
	}
}

func TestText(t *testing.T) {
	got := Text("<p>Hello&nbsp;<b>there</b></p><p>Links:</p><ul><li><a href='x'>One</a></li><li>Two</li></ul>")
	if want := "Hello there\nLinks:\n• One\n• Two"; got != want {
		t.Fatalf("%q", got)
	}
}

func TestNormalizeURL(t *testing.T) {
	for in, want := range map[string]string{
		"feed://example.com/rss":         "https://example.com/rss",
		"  example.com/rss  ":            "https://example.com/rss",
		"http://example.com/rss#top":     "http://example.com/rss",
		"PODCAST://Example.com/Feed.xml": "https://Example.com/Feed.xml",
	} {
		if got, err := NormalizeURL(in); err != nil || got != want {
			t.Errorf("%q: %q %v", in, got, err)
		}
	}
	if _, err := NormalizeURL("ftp://x/y"); err != ErrBadURL {
		t.Errorf("ftp: %v", err)
	}
}

func TestOPML(t *testing.T) {
	b := WriteOPML("Podcasts", []OPMLFeed{{"A & B", "https://a.example/rss"}, {"C", "https://c.example/rss"}}, time.Now())
	feeds, err := ParseOPML(b)
	if err != nil || len(feeds) != 2 || feeds[0].Title != "A & B" || feeds[1].URL != "https://c.example/rss" {
		t.Fatalf("%+v %v\n%s", feeds, err, b)
	}
	if !strings.Contains(string(b), `type="rss"`) {
		t.Fatalf("%s", b)
	}
}
