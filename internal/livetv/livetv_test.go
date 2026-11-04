package livetv

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestParsePlaylist(t *testing.T) {
	in := "\ufeff#EXTM3U x-tvg-url=\"https://guide.example/a.xml.gz,https://b\"\n" +
		`#EXTINF:-1 tvg-id="News1.us@SD" tvg-logo="https://img/n.png" group-title="News;General",News One (720p)` + "\n" +
		"#EXTVLCOPT:http-referrer=https://news.example/\n" +
		"#EXTVLCOPT:http-user-agent=Box/1\n" +
		"https://cdn.example/news/index.m3u8\n" +
		`#EXTINF:-1 tvg-chno="7" group-title="Talk, Late",Seven` + "\n" +
		"live/7.ts|User-Agent=Thing%2F2\n" +
		"#EXTINF:-1,Not playable\n" +
		"rtmp://x/y\n" +
		"#EXTINF:-1,\n" +
		"http://host.example:8080/c\n"
	p, err := ParsePlaylist(strings.NewReader(in), "http://lists.example/tv/list.m3u")
	if err != nil {
		t.Fatal(err)
	}
	if p.GuideURL != "https://guide.example/a.xml.gz" {
		t.Errorf("guide %q", p.GuideURL)
	}
	if len(p.Entries) != 3 {
		t.Fatalf("%d entries: %+v", len(p.Entries), p.Entries)
	}
	e := p.Entries[0]
	if e.Name != "News One (720p)" || e.GuideID != "News1.us@SD" || e.Logo != "https://img/n.png" || e.Referrer != "https://news.example/" ||
		e.UserAgent != "Box/1" || len(e.Groups) != 2 || e.Groups[1] != "General" {
		t.Errorf("%+v", e)
	}
	if e := p.Entries[1]; e.URL != "http://lists.example/tv/live/7.ts" || e.Number != "7" || e.UserAgent != "Thing/2" || e.Groups[0] != "Talk, Late" {
		t.Errorf("%+v", e)
	}
	if e := p.Entries[2]; e.Name != "host.example:8080" {
		t.Errorf("%+v", e)
	}
}

func TestParseGuide(t *testing.T) {
	xmlText := `<?xml version="1.0" encoding="ISO-8859-1"?>
<tv>
 <channel id="News1.us"><display-name>News One</display-name><icon src="https://img/1.png"/></channel>
 <programme start="20261104100000 +0100" stop="20261104110000 +0100" channel="News1.us">
  <title lang="en">Morning &amp; More</title><sub-title>Part 2</sub-title><desc>The day's news.</desc><category>News</category>
 </programme>
 <programme start="20261101100000 +0000" stop="20261101110000 +0000" channel="News1.us"><title>Old</title></programme>
 <programme start="20261104100000 +0000" stop="20261104110000 +0000" channel="Other"><title>Elsewhere</title></programme>
</tv>`
	var gz bytes.Buffer
	w := gzip.NewWriter(&gz)
	w.Write([]byte(xmlText))
	w.Close()
	from := time.Date(2026, 11, 4, 0, 0, 0, 0, time.UTC)
	var chans []GuideChannel
	var progs []Program
	err := ParseGuide(&gz, from, from.Add(48*time.Hour), func(id string) bool { return id == "News1.us" },
		func(c GuideChannel) { chans = append(chans, c) }, func(p Program) { progs = append(progs, p) })
	if err != nil {
		t.Fatal(err)
	}
	if len(chans) != 1 || chans[0].Names[0] != "News One" || chans[0].Logo != "https://img/1.png" {
		t.Errorf("%+v", chans)
	}
	if len(progs) != 1 {
		t.Fatalf("%+v", progs)
	}
	p := progs[0]
	if p.Title != "Morning & More" || p.Subtitle != "Part 2" || p.Category != "News" || !p.Start.Equal(time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC)) {
		t.Errorf("%+v", p)
	}
}

func TestRewrite(t *testing.T) {
	base, _ := url.Parse("https://cdn.example/live/master.m3u8")
	in := "#EXTM3U\n#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID=\"a\",URI=\"audio/en.m3u8\"\n#EXT-X-STREAM-INF:BANDWIDTH=1\nhi/index\n#EXT-X-KEY:METHOD=AES-128,URI=\"/key\"\nseg1.ts\n"
	out := string(Rewrite([]byte(in), base, func(abs string, list bool) string { return fmt.Sprintf("<%s %v>", abs, list) }))
	for _, want := range []string{
		`URI="<https://cdn.example/live/audio/en.m3u8 true>"`,
		"<https://cdn.example/live/hi/index true>",
		`URI="<https://cdn.example/key false>"`,
		"<https://cdn.example/live/seg1.ts false>",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s in\n%s", want, out)
		}
	}
}

func TestSigner(t *testing.T) {
	s := Signer{Key: []byte("k")}
	sig := s.Sign("c1", "https://a/b")
	if !s.Verify("c1", "https://a/b", sig) || s.Verify("c2", "https://a/b", sig) || s.Verify("c1", "https://a/c", sig) {
		t.Error("signatures")
	}
	enc := EncodeURL("https://a/b?x=1")
	if d, _ := DecodeURL(enc); d != "https://a/b?x=1" {
		t.Error(d)
	}
}

func TestCheckAndLoad(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/ok/master.m3u8", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=1\nlow.m3u8\n"))
	})
	mux.HandleFunc("/ok/low.m3u8", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Referer") != "https://ref/" {
			http.Error(w, "no", http.StatusForbidden)
			return
		}
		w.Write([]byte("#EXTM3U\n#EXTINF:4,\n1.ts\n"))
	})
	mux.HandleFunc("/empty.m3u8", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("#EXTM3U\n")) })
	mux.HandleFunc("/ts", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "video/mp2t")
		w.Write([]byte{0x47, 1, 2})
	})
	mux.HandleFunc("/page", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("<html>")) })
	srv := httptest.NewServer(mux)
	defer srv.Close()
	ctx := context.Background()
	if err := Check(ctx, srv.Client(), Stream{URL: srv.URL + "/ok/master.m3u8", Referrer: "https://ref/"}); err != nil {
		t.Error(err)
	}
	if err := Check(ctx, srv.Client(), Stream{URL: srv.URL + "/ok/master.m3u8"}); err == nil {
		t.Error("needs its referrer")
	}
	if err := Check(ctx, srv.Client(), Stream{URL: srv.URL + "/empty.m3u8"}); err == nil {
		t.Error("empty")
	}
	if err := Check(ctx, srv.Client(), Stream{URL: srv.URL + "/ts"}); err != nil {
		t.Error(err)
	}
	if err := Check(ctx, srv.Client(), Stream{URL: srv.URL + "/page"}); err == nil {
		t.Error("a page")
	}
}

func TestLoadIPTVOrg(t *testing.T) {
	list := func(urls ...string) string {
		var b strings.Builder
		b.WriteString("#EXTM3U\n")
		for _, u := range urls {
			fmt.Fprintf(&b, "#EXTINF:-1 tvg-id=\"%s\",%s\nhttps://s.example/%s.m3u8\n", u, u, u)
		}
		return b.String()
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/iptv/countries/us.m3u", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(list("a", "b", "c"))) })
	mux.HandleFunc("/iptv/countries/ca.m3u", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(list("d"))) })
	mux.HandleFunc("/iptv/categories/news.m3u", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(list("b", "d", "e"))) })
	srv := httptest.NewServer(mux)
	defer srv.Close()
	p, err := Load(context.Background(), srv.Client(), Spec{IPTVOrg: &Filter{Countries: []string{"US", "CA", "ZZ"}, Categories: []string{"news"}}, Base: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range p.Entries {
		got = append(got, e.Name)
	}
	if strings.Join(got, ",") != "b,d" {
		t.Errorf("%v", got)
	}
	if _, err := Load(context.Background(), srv.Client(), Spec{Playlist: srv.URL + "/iptv/countries/zz.m3u"}); err == nil {
		t.Error("missing playlist")
	}
}

func TestParseTime(t *testing.T) {
	for in, want := range map[string]string{
		"20261104183000 +0000": "2026-11-04T18:30:00Z",
		"20261104183000 -0500": "2026-11-04T23:30:00Z",
		"20261104183000+0200":  "2026-11-04T16:30:00Z",
		"202611041830":         "2026-11-04T18:30:00Z",
	} {
		got, ok := parseTime(in)
		if !ok || got.Format(time.RFC3339) != want {
			t.Errorf("%s: %v %v", in, got, ok)
		}
	}
}
