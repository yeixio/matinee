package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yeixio/matinee/internal/store"
	"github.com/yeixio/matinee/internal/stream"
)

// testFeed serves a podcast's feed and its episodes' audio. episodes is how
// many the feed lists, newest first; more can be added while it runs.
type testFeed struct {
	srv      *httptest.Server
	title    string
	episodes atomic.Int32
	fetches  atomic.Int32
}

func newTestFeed(t *testing.T, episodes int) *testFeed {
	// The test's feeds are on this machine, which shows can't use otherwise.
	stream.AllowHomeNetwork.Store(true)
	t.Cleanup(func() { stream.AllowHomeNetwork.Store(false) })
	f := &testFeed{title: "Night Shift Radio"}
	f.episodes.Store(int32(episodes))
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/feed.xml":
			f.fetches.Add(1)
			n := int(f.episodes.Load())
			etag := fmt.Sprintf(`"v%d"`, n)
			if r.Header.Get("If-None-Match") == etag {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			w.Header().Set("ETag", etag)
			w.Header().Set("Content-Type", "application/rss+xml")
			fmt.Fprint(w, f.xml(n))
		case strings.HasPrefix(r.URL.Path, "/audio/"):
			w.Header().Set("Content-Type", "application/octet-stream")
			http.ServeContent(w, r, "", time.Time{}, strings.NewReader(strings.Repeat("ID3audio", 1000)))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *testFeed) url() string { return f.srv.URL + "/feed.xml" }

func (f *testFeed) xml(n int) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:itunes="http://www.itunes.com/dtds/podcast-1.0.dtd" xmlns:content="http://purl.org/rss/1.0/modules/content/">
<channel>
<title>` + f.title + `</title>
<link>https://example.com/night</link>
<description>&lt;p&gt;Stories after &lt;b&gt;dark&lt;/b&gt;.&lt;/p&gt;</description>
<language>en-us</language>
<itunes:author>Jane Doe</itunes:author>
<itunes:image href="` + f.srv.URL + `/art.jpg"/>
<itunes:category text="Fiction"><itunes:category text="Drama"/></itunes:category>
`)
	day := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	for i := n; i >= 1; i-- {
		fmt.Fprintf(&b, `<item>
<title>Episode %d</title>
<guid isPermaLink="false">ep-%d</guid>
<pubDate>%s</pubDate>
<itunes:duration>10:00</itunes:duration>
<itunes:episode>%d</itunes:episode>
<content:encoded><![CDATA[<p>Show notes for %d.</p><ul><li>One</li></ul>]]></content:encoded>
<enclosure url="%s/audio/%d.mp3?source=feed" type="audio/mpeg" length="8000"/>
</item>
`, i, i, day.AddDate(0, 0, i-n).Format(time.RFC1123Z), i, i, f.srv.URL, i)
	}
	b.WriteString(`<item><title>Video only</title><guid>video</guid><enclosure url="` + f.srv.URL + `/v.mp4" type="video/mp4" length="1"/></item>
</channel></rss>`)
	return b.String()
}

// A podcast library follows shows by their feeds: adding one lists its
// episodes newest first, plays them through the server, keeps each
// person's place, and new episodes turn up on Home for those who follow it.
func TestPodcasts(t *testing.T) {
	c, _ := newTestServer(t)
	var login struct{ Token string }
	c.do("POST", "/api/setup", credentials{"admin", "correct horse"}, &login)
	c.token = login.Token

	var lib libraryView
	if code := c.do("POST", "/api/v1/libraries", map[string]any{"name": "Podcasts", "kind": "podcasts"}, &lib); code != http.StatusCreated {
		t.Fatalf("create library: %d", code)
	}
	waitForScan(t, c)

	feed := newTestFeed(t, 3)
	var show store.Item
	if code := c.do("POST", "/api/v1/podcasts", map[string]any{"libraryId": lib.ID, "feedUrl": feed.url()}, &show); code != http.StatusCreated {
		t.Fatalf("add podcast: %d", code)
	}
	if show.Kind != "podcast" || show.Title != "Night Shift Radio" || show.Author != "Jane Doe" || !show.HasPoster || show.ChildCount != 3 {
		t.Fatalf("show: %+v", show)
	}
	// Adding it again finds it.
	var again store.Item
	c.do("POST", "/api/v1/podcasts", map[string]any{"libraryId": lib.ID, "feedUrl": feed.url()}, &again)
	if again.ID != show.ID {
		t.Fatalf("added twice: %d and %d", show.ID, again.ID)
	}
	if code := c.do("POST", "/api/v1/podcasts", map[string]any{"libraryId": lib.ID, "feedUrl": feed.srv.URL + "/nothing"}, nil); code != http.StatusBadGateway {
		t.Fatalf("a feed that isn't there: %d", code)
	}

	var page itemView
	c.do("GET", "/api/v1/items/"+itoa(show.ID), nil, &page)
	if page.Podcast == nil || !page.Podcast.Following || page.Podcast.FeedURL != feed.url() || page.Podcast.Unplayed != 3 ||
		page.Overview != "Stories after dark." || len(page.Genres) != 2 {
		t.Fatalf("show page: %+v %+v", page.Item, page.Podcast)
	}

	var eps []store.Item
	c.do("GET", "/api/v1/items/"+itoa(show.ID)+"/children", nil, &eps)
	if len(eps) != 3 || eps[0].Title != "Episode 3" || eps[2].Title != "Episode 1" {
		t.Fatalf("episodes: %+v", eps)
	}
	ep := eps[0]
	if !ep.Playable || ep.Kind != "podcastEpisode" || ep.Podcast != "Night Shift Radio" || ep.Released != "2026-10-30" ||
		ep.DurationMs == nil || *ep.DurationMs != 600_000 || ep.Overview != "Show notes for 3.\n• One" {
		t.Fatalf("episode: %+v", ep)
	}
	var epPage itemView
	c.do("GET", "/api/v1/items/"+itoa(ep.ID), nil, &epPage)
	if epPage.MimeType != "audio/mpeg" || epPage.Episode == nil || epPage.Episode.Type != "full" || len(epPage.Parents) != 1 {
		t.Fatalf("episode page: %+v", epPage)
	}

	// The server passes the audio through, ranges and all.
	req, _ := http.NewRequest("GET", c.base+"/api/v1/items/"+itoa(ep.ID)+"/file", nil)
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Range", "bytes=0-7")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent || resp.Header.Get("Content-Type") != "audio/mpeg" || resp.ContentLength != 8 {
		t.Fatalf("file: %d %s %d", resp.StatusCode, resp.Header.Get("Content-Type"), resp.ContentLength)
	}

	// New episodes of shows someone follows are on Home, until they start one.
	var home struct{ Rows []HomeRow }
	c.do("GET", "/api/v1/home", nil, &home)
	if len(home.Rows) == 0 || home.Rows[0].ID != "newEpisodes" || len(home.Rows[0].Items) != 3 {
		t.Fatalf("home: %+v", home.Rows)
	}
	c.do("PUT", "/api/v1/items/"+itoa(ep.ID)+"/progress", map[string]any{"positionMs": 120_000}, nil)
	c.do("GET", "/api/v1/home", nil, &home)
	if home.Rows[0].ID != "continueListening" || home.Rows[0].Items[0].ID != ep.ID || home.Rows[1].ID != "newEpisodes" || len(home.Rows[1].Items) != 2 {
		t.Fatalf("home after starting one: %+v", home.Rows)
	}
	// The last minute finishes it.
	c.do("PUT", "/api/v1/items/"+itoa(ep.ID)+"/progress", map[string]any{"positionMs": 560_000}, nil)
	c.do("GET", "/api/v1/items/"+itoa(ep.ID), nil, &epPage)
	if !epPage.Played || epPage.PositionMs != 0 {
		t.Fatalf("finished: %+v", epPage.Item)
	}
	// A watch's place, sent later, finishes an episode the same way.
	sync := func(id, ms int64) {
		events := []map[string]any{{"itemId": id, "positionMs": ms, "played": false, "at": time.Now().Unix()}}
		if code := c.do("POST", "/api/v1/sync/progress", map[string]any{"events": events}, nil); code != 200 {
			t.Fatalf("sync: %d", code)
		}
		epPage = itemView{}
		c.do("GET", "/api/v1/items/"+itoa(id), nil, &epPage)
	}
	if sync(eps[1].ID, 500_000); epPage.Played || epPage.PositionMs != 500_000 {
		t.Fatalf("synced place: %+v", epPage.Item)
	}
	if sync(eps[1].ID, 545_000); !epPage.Played || epPage.PositionMs != 0 {
		t.Fatalf("synced finish: %+v", epPage.Item)
	}
	c.do("PUT", "/api/v1/items/"+itoa(eps[1].ID)+"/progress", map[string]any{"positionMs": 0, "played": false}, nil)

	// A refresh finds a new episode, and only asks for changes.
	feed.episodes.Store(4)
	var refreshed struct{ Added int }
	if code := c.do("POST", "/api/v1/podcasts/"+itoa(show.ID)+"/refresh", nil, &refreshed); code != 200 || refreshed.Added != 1 {
		t.Fatalf("refresh: %d %+v", code, refreshed)
	}
	c.do("POST", "/api/v1/podcasts/"+itoa(show.ID)+"/refresh", nil, &refreshed)
	if refreshed.Added != 0 {
		t.Fatalf("refresh again: %+v", refreshed)
	}
	// Scanning the library refreshes it too.
	fetches := feed.fetches.Load()
	c.do("POST", "/api/v1/libraries/"+itoa(lib.ID)+"/scan", nil, nil)
	for deadline := time.Now().Add(5 * time.Second); feed.fetches.Load() == fetches; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("scanning didn't check the feed")
		}
	}

	var following []followedShow
	c.do("GET", "/api/v1/podcasts/following", nil, &following)
	if len(following) != 1 || following[0].Unplayed != 3 {
		t.Fatalf("following: %+v", following)
	}
	c.do("PUT", "/api/v1/podcasts/"+itoa(show.ID)+"/played", map[string]any{"played": true}, nil)
	c.do("GET", "/api/v1/podcasts/following", nil, &following)
	if following[0].Unplayed != 0 {
		t.Fatalf("all played: %+v", following)
	}
	c.do("DELETE", "/api/v1/podcasts/"+itoa(show.ID)+"/follow", nil, nil)
	c.do("GET", "/api/v1/podcasts/following", nil, &following)
	if len(following) != 0 {
		t.Fatalf("unfollowed: %+v", following)
	}

	// Shows move between apps as OPML.
	opml := get(t, c, "/api/v1/libraries/"+itoa(lib.ID)+"/opml")
	if !strings.Contains(opml, `xmlUrl="`+feed.url()+`"`) {
		t.Fatalf("opml: %s", opml)
	}
	other := newTestFeed(t, 1)
	other.title = "Day Shift"
	importBody := `<?xml version="1.0"?><opml version="2.0"><body><outline text="Shows">
		<outline type="rss" text="Night Shift Radio" xmlUrl="` + feed.url() + `"/>
		<outline type="rss" text="Other" xmlUrl="` + other.url() + `"/>
		<outline type="rss" text="Gone" xmlUrl="` + other.srv.URL + `/gone.xml"/>
	</outline></body></opml>`
	req, _ = http.NewRequest("POST", c.base+"/api/v1/libraries/"+itoa(lib.ID)+"/opml", strings.NewReader(importBody))
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var imported struct {
		Added    int
		Problems []importProblem
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(&imported); err != nil {
		t.Fatal(err)
	}
	if imported.Added != 2 || len(imported.Problems) != 1 || imported.Problems[0].Title != "Gone" {
		t.Fatalf("import: %+v", imported)
	}

	// Admins remove shows, episodes and all.
	if code := c.do("DELETE", "/api/v1/podcasts/"+itoa(show.ID), nil, nil); code != http.StatusNoContent {
		t.Fatalf("delete: %d", code)
	}
	if code := c.do("GET", "/api/v1/items/"+itoa(ep.ID), nil, nil); code != http.StatusNotFound {
		t.Fatalf("episode after delete: %d", code)
	}
}

// Podcast Index finds shows once an admin enters a key, and marks the ones
// a library has.
func TestPodcastDirectory(t *testing.T) {
	feed := newTestFeed(t, 1)
	var auth, asked atomic.Value
	index := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked.Store(r.URL.Path + "?" + r.URL.RawQuery)
		auth.Store(r.Header.Get("X-Auth-Key") + " " + r.Header.Get("Authorization"))
		if r.Header.Get("X-Auth-Key") != "GOODKEY123" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		fmt.Fprintf(w, `{"status":"true","feeds":[{"id":7,"title":"Night Shift Radio","url":%q,"author":"Jane Doe",
			"artwork":"https://example.com/a.jpg","description":"<p>Stories</p>","language":"en","categories":{"9":"Drama","2":"Fiction"},"episodeCount":1},
			{"id":8,"title":"Dead show","url":"https://example.com/dead.xml","dead":1}]}`, feed.url())
	}))
	defer index.Close()
	c, _ := newTestServer(t, func(s *Server) { s.PodcastIndexBase = index.URL })
	var login struct{ Token string }
	c.do("POST", "/api/setup", credentials{"admin", "correct horse"}, &login)
	c.token = login.Token

	var dir struct {
		Connected  bool
		Shows      []directoryShow
		Categories []struct{ ID, Name string }
	}
	c.do("GET", "/api/v1/podcasts/directory?q=night", nil, &dir)
	if dir.Connected || len(dir.Shows) != 0 {
		t.Fatalf("without a key: %+v", dir)
	}
	if code := c.do("PUT", "/api/v1/integrations/podcastindex", map[string]string{"key": "BADKEY", "secret": "s"}, nil); code != http.StatusBadRequest {
		t.Fatalf("bad key: %d", code)
	}
	var v podcastIndexView
	if code := c.do("PUT", "/api/v1/integrations/podcastindex", map[string]string{"key": "GOODKEY123", "secret": "s"}, &v); code != 200 || !v.Connected || v.Hint != "Y123" {
		t.Fatalf("good key: %d %+v", code, v)
	}
	var lib libraryView
	c.do("POST", "/api/v1/libraries", map[string]any{"name": "Podcasts", "kind": "podcasts"}, &lib)
	var show store.Item
	c.do("POST", "/api/v1/podcasts", map[string]any{"libraryId": lib.ID, "feedUrl": feed.url()}, &show)

	c.do("GET", "/api/v1/podcasts/directory?q=night", nil, &dir)
	if !dir.Connected || len(dir.Shows) != 1 || dir.Shows[0].ItemID != show.ID || dir.Shows[0].Description != "Stories" ||
		strings.Join(dir.Shows[0].Categories, ",") != "Fiction,Drama" {
		t.Fatalf("search: %+v", dir)
	}
	c.do("GET", "/api/v1/podcasts/directory?lang=de-AT&category=trueCrime", nil, &dir)
	if a, _ := asked.Load().(string); a != "/podcasts/trending?cat=103&lang=de&max=40" || len(dir.Shows) != 1 ||
		len(dir.Categories) < 10 || dir.Categories[0].ID != "arts" || dir.Categories[0].Name != "Arts" {
		t.Fatalf("trending in a category: %s %+v", a, dir)
	}
	c.do("GET", "/api/v1/podcasts/directory?lang=de&category=nope", nil, &dir)
	if a, _ := asked.Load().(string); a != "/podcasts/trending?lang=de&max=40" {
		t.Fatalf("unknown category: %s", a)
	}
	if a, _ := auth.Load().(string); !strings.HasPrefix(a, "GOODKEY123 ") || len(a) != len("GOODKEY123 ")+40 {
		t.Fatalf("auth headers: %q", a)
	}
	c.do("DELETE", "/api/v1/integrations/podcastindex", nil, nil)
	c.do("GET", "/api/v1/integrations/podcastindex", nil, &v)
	if v.Connected {
		t.Fatal("still connected")
	}
}
