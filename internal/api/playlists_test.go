package api

import (
	"net/http"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/yeixio/matinee/internal/store"
)

func TestPlaylists(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	c, tmp := newTestServer(t)
	var login struct{ Token string }
	c.do("POST", "/api/v1/setup", credentials{"admin", "correct horse"}, &login)
	c.token = login.Token

	song := func(album, title, track string) {
		makeMedia(t, filepath.Join(tmp, "music", album, title+".flac"),
			"-f", "lavfi", "-i", "sine=duration=1", "-metadata", "artist=Band", "-metadata", "album="+album,
			"-metadata", "title="+title, "-metadata", "track="+track)
	}
	song("First", "One", "1")
	song("First", "Two", "2")
	song("Second", "Three", "1")
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
	first, three := byTitle["First"], byTitle["Three"]
	if first.ID == 0 || three.ID == 0 {
		t.Fatalf("scanned: %v", byTitle)
	}

	titles := func(p playlistView) []string {
		var out []string
		for _, e := range p.Entries {
			out = append(out, e.Title)
		}
		return out
	}
	same := func(got []string, want ...string) bool {
		if len(got) != len(want) {
			return false
		}
		for i := range got {
			if got[i] != want[i] {
				return false
			}
		}
		return true
	}

	// An album adds its songs in order; a song can go on twice.
	var p playlistView
	if code := c.do("POST", "/api/v1/playlists", map[string]any{"name": "  Café Mix ", "items": []int64{first.ID}}, &p); code != http.StatusCreated {
		t.Fatalf("create: %d", code)
	}
	if p.Name != "Café Mix" || !same(titles(p), "One", "Two") || p.Count != 2 || len(p.AlbumIDs) != 1 {
		t.Fatalf("created %+v %v", p.Playlist, titles(p))
	}
	c.do("POST", "/api/v1/playlists/"+p.ID+"/items", map[string]any{"items": []int64{three.ID, p.Entries[0].ID}}, &p)
	if !same(titles(p), "One", "Two", "Three", "One") || p.Count != 4 || len(p.AlbumIDs) != 2 || p.DurationMs <= 0 {
		t.Fatalf("after adding: %+v %v", p.Playlist, titles(p))
	}
	if p.Entries[0].EntryID == p.Entries[3].EntryID {
		t.Fatal("the same song twice shares an entry id")
	}

	// Reordering by entry, leaving one out: it goes last.
	e := p.Entries
	c.do("PUT", "/api/v1/playlists/"+p.ID+"/order", map[string]any{"entries": []string{e[2].EntryID, e[0].EntryID, "nope", e[3].EntryID}}, &p)
	if !same(titles(p), "Three", "One", "One", "Two") {
		t.Fatalf("after ordering: %v", titles(p))
	}
	c.do("DELETE", "/api/v1/playlists/"+p.ID+"/entries/"+p.Entries[1].EntryID, nil, &p)
	if !same(titles(p), "Three", "One", "Two") {
		t.Fatalf("after removing: %v", titles(p))
	}

	if code := c.do("POST", "/api/v1/playlists", map[string]any{"name": "Road trip"}, nil); code != http.StatusCreated {
		t.Fatalf("create empty: %d", code)
	}
	if code := c.do("POST", "/api/v1/playlists", map[string]any{"name": " "}, nil); code != http.StatusBadRequest {
		t.Errorf("no name: %d", code)
	}
	if code := c.do("POST", "/api/v1/playlists/"+p.ID+"/items", map[string]any{"items": []int64{999999}}, nil); code != http.StatusNotFound {
		t.Errorf("adding a missing item: %d", code)
	}

	// Finding one by name, as someone would say it.
	find := func(q string) []string {
		var found []store.Playlist
		c.do("GET", "/api/v1/playlists?q="+q, nil, &found)
		var names []string
		for _, f := range found {
			names = append(names, f.Name)
		}
		return names
	}
	if got := find("cafe%20mix"); !same(got, "Café Mix") {
		t.Errorf("find cafe mix: %v", got)
	}
	if got := find("trip"); !same(got, "Road trip") {
		t.Errorf("find trip: %v", got)
	}
	if got := find("polka"); len(got) != 0 {
		t.Errorf("find polka: %v", got)
	}
	var all []store.Playlist
	c.do("GET", "/api/v1/playlists", nil, &all)
	if len(all) != 2 || all[0].Name != "Road trip" {
		t.Fatalf("list: %+v", all)
	}

	// Playlists are each person's own.
	c.do("POST", "/api/v1/users", map[string]any{"username": "ellen", "password": "password123"}, nil)
	ellen := signIn(t, c, "ellen", "password123", "Phone")
	var hers []store.Playlist
	ellen.do("GET", "/api/v1/playlists", nil, &hers)
	if len(hers) != 0 {
		t.Fatalf("ellen sees %+v", hers)
	}
	if code := ellen.do("GET", "/api/v1/playlists/"+p.ID, nil, nil); code != http.StatusNotFound {
		t.Errorf("ellen opening admin's playlist: %d", code)
	}
	if code := ellen.do("DELETE", "/api/v1/playlists/"+p.ID, nil, nil); code != http.StatusNotFound {
		t.Errorf("ellen deleting admin's playlist: %d", code)
	}

	c.do("PUT", "/api/v1/playlists/"+p.ID, map[string]any{"name": "Morning"}, &p)
	if p.Name != "Morning" || len(p.Entries) != 3 {
		t.Fatalf("renamed: %+v", p.Playlist)
	}
	if code := c.do("DELETE", "/api/v1/playlists/"+p.ID, nil, nil); code != http.StatusNoContent {
		t.Fatalf("delete: %d", code)
	}
	if code := c.do("GET", "/api/v1/playlists/"+p.ID, nil, nil); code != http.StatusNotFound {
		t.Errorf("deleted playlist: %d", code)
	}
}

func TestFoldName(t *testing.T) {
	for in, want := range map[string]string{"Café  Mix!": "cafe mix", "  rock'n'roll ": "rock n roll", "Ünïcødé": "unicøde"} {
		if got := foldName(in); got != want {
			t.Errorf("foldName(%q) = %q, want %q", in, got, want)
		}
	}
}
