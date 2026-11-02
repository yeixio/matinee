package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/yeixio/matinee/internal/lyrics"
	"github.com/yeixio/matinee/internal/probe"
	"github.com/yeixio/matinee/internal/store"
)

func TestLyrics(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	var asked atomic.Int32
	lrclib := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked.Add(1)
		if r.URL.Query().Get("track_name") == "Plain" && r.URL.Query().Get("artist_name") == "Band" {
			json.NewEncoder(w).Encode(map[string]any{"syncedLyrics": "[00:00.20]Sung online"})
			return
		}
		http.NotFound(w, r)
	}))
	defer lrclib.Close()
	c, tmp := newTestServer(t, func(s *Server) {
		s.Lyrics = &lyrics.Finder{Prober: probe.Prober{Path: "ffprobe"}, Online: &lyrics.LRCLIB{Base: lrclib.URL, Dir: t.TempDir()}}
	})
	var login struct{ Token string }
	c.do("POST", "/api/v1/setup", credentials{"admin", "correct horse"}, &login)
	c.token = login.Token

	dir := filepath.Join(tmp, "music", "Album")
	song := func(title string, tags ...string) {
		args := []string{"-f", "lavfi", "-i", "sine=duration=1", "-metadata", "artist=Band", "-metadata", "album=Album", "-metadata", "title=" + title}
		for _, t := range tags {
			args = append(args, "-metadata", t)
		}
		makeMedia(t, filepath.Join(dir, title+".flac"), args...)
	}
	song("Timed")
	song("Tagged", "lyrics=Words in the tags")
	song("Plain")
	os.WriteFile(filepath.Join(dir, "Timed.lrc"), []byte("[ar:Band]\n[00:00.50]Hello\n[00:00.90]Goodbye\n"), 0o644)
	var lib store.Library
	c.do("POST", "/api/v1/libraries", map[string]any{"name": "Music", "kind": "music", "paths": []string{filepath.Join(tmp, "music")}}, &lib)
	waitForScan(t, c)
	ids := map[string]int64{}
	for _, kind := range []string{"album", "track"} {
		var items []store.Item
		c.do("GET", "/api/v1/libraries/"+itoa(lib.ID)+"/items?kind="+kind, nil, &items)
		for _, it := range items {
			ids[it.Title] = it.ID
		}
	}
	get := func(title string) lyrics.Lyrics {
		t.Helper()
		var l lyrics.Lyrics
		if code := c.do("GET", "/api/v1/items/"+itoa(ids[title])+"/lyrics", nil, &l); code != 200 || l.Lines == nil {
			t.Fatalf("%s: %d %+v", title, code, l)
		}
		return l
	}

	if l := get("Timed"); !l.Synced || l.Source != "file" || len(l.Lines) != 2 || *l.Lines[1].Ms != 900 || l.Lines[1].Text != "Goodbye" {
		t.Fatalf("from the .lrc: %+v", l)
	}
	if l := get("Tagged"); l.Synced || l.Source != "embedded" || l.Lines[0].Text != "Words in the tags" {
		t.Fatalf("from the tags: %+v", l)
	}
	// Online lyrics are off until an admin turns them on.
	if l := get("Plain"); !l.Empty() || asked.Load() != 0 {
		t.Fatalf("online while off: %+v, asked %d times", l, asked.Load())
	}
	var set serverSettings
	if c.do("GET", "/api/v1/settings", nil, &set); set.OnlineLyrics {
		t.Fatal("online lyrics on from the start")
	}
	c.do("PUT", "/api/v1/settings", map[string]any{"onlineLyrics": true}, &set)
	if !set.OnlineLyrics {
		t.Fatal("didn't turn on")
	}
	if l := get("Plain"); !l.Synced || l.Source != "online" || l.Lines[0].Text != "Sung online" {
		t.Fatalf("online: %+v", l)
	}
	// Words of a song only.
	if code := c.do("GET", "/api/v1/items/"+itoa(ids["Album"])+"/lyrics", nil, nil); code != 400 {
		t.Fatalf("an album's lyrics: %d", code)
	}
}
