package api

import (
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yeixio/matinee/internal/store"
)

// An album's songs play as one stream, and the household can say which
// albums do.
func TestGapless(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	c, tmp := newTestServer(t)
	var login struct{ Token string }
	c.do("POST", "/api/v1/setup", credentials{"admin", "correct horse"}, &login)
	c.token = login.Token

	song := func(album, title, track string) {
		makeMedia(t, filepath.Join(tmp, "music", album, title+".mp3"),
			"-f", "lavfi", "-i", "sine=duration=3", "-metadata", "artist=DJ", "-metadata", "album="+album,
			"-metadata", "title="+title, "-metadata", "track="+track)
	}
	song("Night Mix", "One", "1")
	song("Night Mix", "Two", "2")
	song("Other", "Three", "1")
	var lib store.Library
	if code := c.do("POST", "/api/v1/libraries", map[string]any{"name": "Music", "kind": "music", "paths": []string{filepath.Join(tmp, "music")}}, &lib); code != http.StatusCreated {
		t.Fatalf("create library: %d", code)
	}
	waitForScan(t, c)
	byTitle := map[string]store.Item{}
	for _, kind := range []string{"album", "track"} {
		var items []store.Item
		c.do("GET", "/api/v1/libraries/"+itoa(lib.ID)+"/items?kind="+kind, nil, &items)
		for _, it := range items {
			byTitle[it.Title] = it
		}
	}
	album, one, two, three := byTitle["Night Mix"], byTitle["One"], byTitle["Two"], byTitle["Three"]
	if album.ID == 0 || one.ID == 0 || two.ID == 0 || three.ID == 0 {
		t.Fatalf("scanned: %v", byTitle)
	}
	if album.Gapless || album.GaplessChoice != nil {
		t.Fatalf("new album: %+v", album)
	}

	// Turned on, the album and its songs say so; null hands it back to Matinee.
	var got store.Item
	if code := c.do("PUT", "/api/v1/items/"+itoa(album.ID)+"/gapless", map[string]any{"gapless": true}, &got); code != 200 {
		t.Fatalf("turn on: %d", code)
	}
	if !got.Gapless || got.GaplessChoice == nil || !*got.GaplessChoice {
		t.Fatalf("after turning on: %+v", got)
	}
	var tracks []store.Item
	c.do("GET", "/api/v1/items/"+itoa(album.ID)+"/tracks", nil, &tracks)
	if len(tracks) != 2 || !tracks[0].Gapless || !tracks[1].Gapless {
		t.Fatalf("tracks: %+v", tracks)
	}
	got = store.Item{}
	c.do("PUT", "/api/v1/items/"+itoa(album.ID)+"/gapless", map[string]any{"gapless": nil}, &got)
	if got.Gapless || got.GaplessChoice != nil {
		t.Fatalf("back to automatic: %+v", got)
	}
	if code := c.do("PUT", "/api/v1/items/"+itoa(one.ID)+"/gapless", map[string]any{"gapless": true}, nil); code != http.StatusBadRequest {
		t.Fatalf("a song isn't an album: %d", code)
	}

	// The stream: one playlist, and where each song starts on its clock.
	var g struct {
		URL      string
		StartSec float64
		Tracks   []struct {
			ID       int64
			StartSec float64
		}
	}
	if code := c.do("POST", "/api/v1/gapless", map[string]any{"tracks": []int64{one.ID, two.ID}, "start": 1}, &g); code != 200 {
		t.Fatalf("start: %d", code)
	}
	if !strings.HasSuffix(g.URL, "/index.m3u8") || g.StartSec != 1 || len(g.Tracks) != 2 || g.Tracks[0].StartSec != -1 ||
		g.Tracks[1].ID != two.ID || g.Tracks[1].StartSec < 1.9 || g.Tracks[1].StartSec > 2.1 {
		t.Fatalf("answer: %+v", g)
	}
	if body := get(t, c, g.URL); !strings.Contains(body, "seg00000.ts") {
		t.Fatalf("playlist: %s", body)
	}
	if code := c.do("POST", "/api/v1/gapless", map[string]any{"tracks": []int64{one.ID, three.ID}}, nil); code != http.StatusBadRequest {
		t.Fatalf("songs from two albums: %d", code)
	}
	if code := c.do("POST", "/api/v1/gapless", map[string]any{"tracks": []int64{album.ID}}, nil); code != http.StatusBadRequest {
		t.Fatalf("an album isn't a song: %d", code)
	}
}
