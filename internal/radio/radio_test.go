package radio

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// icyStream is a station sending titles every 8 bytes of audio.
func icyStream(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/listen.pls":
			w.Header().Set("Content-Type", "audio/x-scpls")
			io.WriteString(w, "[playlist]\nNumberOfEntries=1\nFile1=/stream\nTitle1=Test\n")
			return
		case "/hls.m3u8":
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			io.WriteString(w, "#EXTM3U\n#EXT-X-VERSION:3\nchunk.ts\n")
			return
		case "/page":
			w.Header().Set("Content-Type", "text/html")
			io.WriteString(w, "<html></html>")
			return
		}
		w.Header().Set("Content-Type", "audio/mpeg")
		w.Header().Set("Icy-Name", "Deep Test FM")
		w.Header().Set("Icy-Genre", "Deep House")
		if r.Header.Get("Icy-MetaData") != "1" {
			io.WriteString(w, "AAAAAAAABBBBBBBB")
			return
		}
		w.Header().Set("Icy-Metaint", "8")
		meta := func(s string) string {
			n := (len(s) + 15) / 16
			return string(rune(n)) + s + strings.Repeat("\x00", n*16-len(s))
		}
		io.WriteString(w, "AAAAAAAA"+meta("StreamTitle='Lane 8 - Brightest Lights';StreamUrl='';")+"BBBBBBBB"+"\x00"+"CCCC")
	}))
}

func TestProbe(t *testing.T) {
	srv := icyStream(t)
	defer srv.Close()
	ctx := context.Background()
	info, err := Probe(ctx, srv.Client(), srv.URL+"/stream")
	if err != nil || info.Name != "Deep Test FM" || info.Genre != "Deep House" || info.URL != srv.URL+"/stream" {
		t.Fatalf("stream: %+v %v", info, err)
	}
	// A playlist file leads to its stream.
	if info, err := Probe(ctx, srv.Client(), srv.URL+"/listen.pls"); err != nil || info.URL != srv.URL+"/stream" {
		t.Fatalf("playlist: %+v %v", info, err)
	}
	if _, err := Probe(ctx, srv.Client(), srv.URL+"/hls.m3u8"); err != ErrHLS {
		t.Fatalf("hls: %v", err)
	}
	if _, err := Probe(ctx, srv.Client(), srv.URL+"/page"); err != ErrNotAudio {
		t.Fatalf("web page: %v", err)
	}
	if _, err := Probe(ctx, srv.Client(), "ftp://example.com/x"); err != ErrBadURL {
		t.Fatalf("ftp: %v", err)
	}
}

// Relay takes the titles out of the audio and keeps the last one.
func TestRelay(t *testing.T) {
	srv := icyStream(t)
	defer srv.Close()
	var got []string
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/items/1/file", nil)
	Relay(rec, req, srv.Client(), srv.URL+"/stream", func(s string) { got = append(got, s) })
	if body := rec.Body.String(); body != "AAAAAAAABBBBBBBBCCCC" {
		t.Fatalf("audio: %q", body)
	}
	if len(got) != 1 || got[0] != "Lane 8 - Brightest Lights" {
		t.Fatalf("titles: %q", got)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "audio/mpeg" {
		t.Fatalf("type: %q", ct)
	}
}

func TestStreamTitle(t *testing.T) {
	for in, want := range map[string]string{
		"StreamTitle='Above & Beyond - Sun & Moon';StreamUrl='';\x00\x00": "Above & Beyond - Sun & Moon",
		"StreamTitle='It's Late';":                                        "It's Late",
		"StreamTitle='';":                                                 "",
	} {
		if got, ok := StreamTitle(in); !ok || got != want {
			t.Errorf("%q: %q %v", in, got, ok)
		}
	}
	if _, ok := StreamTitle("nothing"); ok {
		t.Error("no title found one")
	}
}

func TestFirstStream(t *testing.T) {
	if got := firstStream("#EXTM3U\n#EXTINF:-1,Test\nhttp://a.example/live\n", "https://b.example/x.m3u"); got != "http://a.example/live" {
		t.Fatalf("m3u: %q", got)
	}
	if got := firstStream("[playlist]\nfile1=relative/stream\n", "https://b.example/dir/x.pls"); got != "https://b.example/dir/relative/stream" {
		t.Fatalf("pls: %q", got)
	}
}

func TestSearch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/json/stations/search" || r.URL.Query().Get("name") != "deep" {
			http.NotFound(w, r)
			return
		}
		io.WriteString(w, `[
			{"name":"Deep FM","url":"http://x.example/pls","url_resolved":"https://x.example/live","favicon":"https://x.example/logo.png","tags":"deep house,electronic","countrycode":"GB","codec":"MP3","bitrate":128,"hls":0},
			{"name":"HLS Only","url_resolved":"https://y.example/live.m3u8","hls":1},
			{"name":"Deep FM again","url_resolved":"https://x.example/live","hls":0},
			{"name":"","url_resolved":"https://z.example/live","hls":0}
		]`)
	}))
	defer srv.Close()
	found, err := Search(context.Background(), srv.Client(), srv.URL, "deep", false)
	if err != nil || len(found) != 1 {
		t.Fatalf("found %+v %v", found, err)
	}
	f := found[0]
	if f.URL != "https://x.example/live" || f.Image != "https://x.example/logo.png" || len(f.Tags) != 2 || f.Bitrate != 128 || strconv.Itoa(f.Bitrate) != "128" {
		t.Fatalf("station: %+v", f)
	}
}
