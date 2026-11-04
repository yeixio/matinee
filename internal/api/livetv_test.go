package api

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yeixio/matinee/internal/rating"
	"github.com/yeixio/matinee/internal/store"
)

// An admin adds a playlist with a guide; its channels list with what's on,
// play through the server's relay (playlists rewritten, pieces passed on
// with the referrer the channel needs), and a channel that stops answering
// is hidden after its checks fail. Kids profiles don't see live TV.
func TestLiveTV(t *testing.T) {
	var down atomic.Bool
	var srvURL string
	now := time.Now().UTC()
	tv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/list.m3u":
			fmt.Fprintf(w, "#EXTM3U url-tvg=\"%s/guide.xml\"\n"+
				"#EXTINF:-1 tvg-id=\"news.one\" group-title=\"News\",News One (720p)\n#EXTVLCOPT:http-referrer=https://news.example/\n%s/news/master.m3u8\n"+
				"#EXTINF:-1 tvg-id=\"flaky\" group-title=\"Movies\",Flaky\n%s/flaky.m3u8\n"+
				"#EXTINF:-1 group-title=\"XXX\",Grown\n%s/x.m3u8\n", srvURL, srvURL, srvURL, srvURL)
		case "/guide.xml":
			fmt.Fprintf(w, `<tv><channel id="news.one"><display-name>News One</display-name></channel>
<programme start="%s +0000" stop="%s +0000" channel="news.one"><title>Evening News</title></programme></tv>`,
				now.Add(-10*time.Minute).Format("20060102150405"), now.Add(50*time.Minute).Format("20060102150405"))
		case "/news/master.m3u8":
			io.WriteString(w, "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=1\nlow/index.m3u8\n")
		case "/news/low/index.m3u8":
			io.WriteString(w, "#EXTM3U\n#EXT-X-TARGETDURATION:4\n#EXTINF:4,\nseg1.ts\n")
		case "/news/low/seg1.ts":
			if r.Header.Get("Referer") != "https://news.example/" {
				http.Error(w, "no", http.StatusForbidden)
				return
			}
			w.Header().Set("Content-Type", "video/mp2t")
			w.Write([]byte{0x47, 'v', 'i', 'd'})
		case "/flaky.m3u8", "/x.m3u8":
			if down.Load() {
				http.NotFound(w, r)
				return
			}
			io.WriteString(w, "#EXTM3U\n#EXTINF:4,\na.ts\n")
		default:
			http.NotFound(w, r)
		}
	}))
	defer tv.Close()
	srvURL = tv.URL

	var srv *Server
	c, _ := newTestServer(t, func(s *Server) { srv = s })
	var login struct{ Token string }
	c.do("POST", "/api/setup", credentials{"admin", "correct horse"}, &login)
	c.token = login.Token

	if code := c.do("POST", "/api/v1/live/sources", map[string]any{"playlistUrl": tv.URL + "/missing.m3u"}, nil); code != http.StatusBadGateway {
		t.Fatalf("a playlist that isn't there: %d", code)
	}
	var src store.LiveSource
	if code := c.do("POST", "/api/v1/live/sources", map[string]any{"name": "Home", "playlistUrl": tv.URL + "/list.m3u"}, &src); code != http.StatusCreated {
		t.Fatalf("add source: %d", code)
	}
	if src.Channels != 2 {
		t.Fatalf("source: %+v", src)
	}
	// The guide and checks run in the background.
	type listing struct {
		ID, Name string
		Now      *store.LiveProgram
	}
	var list struct {
		Channels   []listing
		Categories []string
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		c.do("GET", "/api/v1/live/channels", nil, &list)
		if len(list.Channels) == 2 && list.Channels[0].Now != nil || time.Now().After(deadline) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if len(list.Channels) != 2 || list.Channels[0].Name != "News One (720p)" || list.Channels[0].Now == nil || list.Channels[0].Now.Title != "Evening News" {
		t.Fatalf("channels: %+v", list)
	}
	if strings.Join(list.Categories, ",") != "Movies,News" {
		t.Fatalf("categories: %v", list.Categories)
	}

	// Playing goes through the relay, down to the pieces.
	var play struct{ URL string }
	if code := c.do("POST", "/api/v1/live/channels/"+list.Channels[0].ID+"/play", nil, &play); code != http.StatusOK || !strings.Contains(play.URL, "/hls?") {
		t.Fatalf("play: %d %+v", code, play)
	}
	get := func(path string) string {
		t.Helper()
		sep := "?"
		if strings.Contains(path, "?") {
			sep = "&"
		}
		res, err := http.Get(c.base + path + sep + "token=" + c.token)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		if res.StatusCode != http.StatusOK {
			t.Fatalf("%s: %d %s", path, res.StatusCode, b)
		}
		return string(b)
	}
	link := func(playlist string) string {
		for _, l := range strings.Split(playlist, "\n") {
			if strings.HasPrefix(l, "/api/v1/live/") {
				if !strings.Contains(l, "token=") {
					t.Fatalf("link without the token: %s", l)
				}
				return l
			}
		}
		t.Fatalf("no link in %q", playlist)
		return ""
	}
	master := get(play.URL)
	media := get(link(master))
	if seg := get(link(media)); seg != "\x47vid" {
		t.Fatalf("segment: %q", seg)
	}
	// A link that wasn't signed is refused.
	res, _ := http.Get(c.base + "/api/v1/live/channels/" + list.Channels[0].ID + "/hls?u=aHR0cDovL3JvdXRlcg&s=x&token=" + c.token)
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("unsigned link: %d", res.StatusCode)
	}
	res.Body.Close()

	// A channel that stops answering is hidden after two failed checks.
	down.Store(true)
	lm := srv.liveManager()
	for range 2 {
		lm.CheckNow(t.Context())
	}
	c.do("GET", "/api/v1/live/channels", nil, &list)
	if len(list.Channels) != 1 {
		t.Fatalf("after the checks: %+v", list.Channels)
	}
	var all []store.LiveChannel
	c.do("GET", "/api/v1/live/sources/"+src.ID+"/channels", nil, &all)
	if len(all) != 2 || !all[1].Broken {
		t.Fatalf("admin's list: %+v", all)
	}

	// Hiding one takes it out for everyone.
	if code := c.do("PUT", "/api/v1/live/channels/"+all[0].ID, map[string]any{"hidden": true}, nil); code != http.StatusOK {
		t.Fatalf("hide: %d", code)
	}
	c.do("GET", "/api/v1/live/channels", nil, &list)
	if len(list.Channels) != 0 {
		t.Fatalf("after hiding: %+v", list.Channels)
	}

	// Kids profiles don't see live TV.
	var me store.User
	c.do("GET", "/api/v1/me", nil, &me)
	var kid store.Profile
	c.do("POST", "/api/v1/profiles", map[string]any{"accountId": me.ID, "name": "Sam", "kidsLevel": rating.Kids}, &kid)
	c.do("POST", "/api/v1/profiles/select", map[string]any{"id": kid.ID}, nil)
	if code := c.do("GET", "/api/v1/live/channels", nil, nil); code != http.StatusForbidden {
		t.Fatalf("kids profile: %d", code)
	}
	c.do("POST", "/api/v1/profiles/select", map[string]any{"id": me.ID}, nil)

	if code := c.do("DELETE", "/api/v1/live/sources/"+src.ID, nil, nil); code != http.StatusNoContent {
		t.Fatalf("delete: %d", code)
	}
	var srcs []store.LiveSource
	c.do("GET", "/api/v1/live/sources", nil, &srcs)
	if len(srcs) != 0 {
		t.Fatalf("sources: %+v", srcs)
	}
}
