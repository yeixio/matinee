package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yeixio/matinee/internal/store"
	"github.com/yeixio/matinee/internal/stream"
)

// A station is added to a music library by its stream, plays through the
// server with its song titles taken out, says what's playing, survives a
// scan, and is left out of the library's other lists.
func TestRadioStations(t *testing.T) {
	stream.AllowHomeNetwork.Store(true)
	t.Cleanup(func() { stream.AllowHomeNetwork.Store(false) })
	station := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/listen.pls" {
			w.Header().Set("Content-Type", "audio/x-scpls")
			io.WriteString(w, "[playlist]\nFile1=/live\n")
			return
		}
		w.Header().Set("Content-Type", "audio/mpeg")
		w.Header().Set("Icy-Name", "Deep Test FM")
		if r.Header.Get("Icy-MetaData") == "1" {
			w.Header().Set("Icy-Metaint", "4")
			title := "StreamTitle='Lane 8 - Road';"
			io.WriteString(w, "AAAA"+string(rune(2))+title+strings.Repeat("\x00", 32-len(title))+"BBBB")
			return
		}
		io.WriteString(w, "AAAABBBB")
	}))
	defer station.Close()

	c, tmp := newTestServer(t)
	var login struct{ Token string }
	c.do("POST", "/api/setup", credentials{"admin", "correct horse"}, &login)
	c.token = login.Token
	os.MkdirAll(filepath.Join(tmp, "music"), 0o755)
	var lib libraryView
	c.do("POST", "/api/v1/libraries", map[string]any{"name": "Music", "kind": "music", "paths": []string{filepath.Join(tmp, "music")}}, &lib)
	waitForScan(t, c)

	var st store.Item
	if code := c.do("POST", "/api/v1/stations", map[string]any{"libraryId": lib.ID, "url": station.URL + "/listen.pls"}, &st); code != http.StatusCreated {
		t.Fatalf("add station: %d", code)
	}
	if st.Kind != "station" || st.Title != "Deep Test FM" || !st.Playable {
		t.Fatalf("station: %+v", st)
	}
	if code := c.do("POST", "/api/v1/stations", map[string]any{"libraryId": lib.ID, "url": station.URL + "/live"}, nil); code != http.StatusConflict {
		t.Fatalf("the same stream again: %d", code)
	}
	if code := c.do("POST", "/api/v1/stations", map[string]any{"libraryId": lib.ID, "url": "not a url at all"}, nil); code != http.StatusBadRequest {
		t.Fatalf("a bad address: %d", code)
	}

	// Playing it relays the audio without the title, and keeps the title.
	res, err := http.Get(c.base + "/api/v1/items/" + itoa(st.ID) + "/file?token=" + c.token)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if string(body) != "AAAABBBB" || res.Header.Get("Content-Type") != "audio/mpeg" {
		t.Fatalf("stream: %q %s", body, res.Header.Get("Content-Type"))
	}
	var now struct{ Title string }
	c.do("GET", "/api/v1/items/"+itoa(st.ID)+"/now", nil, &now)
	if now.Title != "Lane 8 - Road" {
		t.Fatalf("now playing: %+v", now)
	}

	// A scan leaves it, and the library's titles don't include it.
	c.do("POST", "/api/v1/libraries/"+itoa(lib.ID)+"/scan", nil, nil)
	waitForScan(t, c)
	var list []store.Item
	c.do("GET", "/api/v1/libraries/"+itoa(lib.ID)+"/stations", nil, &list)
	if len(list) != 1 || list[0].ID != st.ID {
		t.Fatalf("stations after a scan: %+v", list)
	}
	var top []store.Item
	c.do("GET", "/api/v1/libraries/"+itoa(lib.ID)+"/items", nil, &top)
	if len(top) != 0 {
		t.Fatalf("library titles: %+v", top)
	}

	var renamed store.Item
	if code := c.do("PUT", "/api/v1/stations/"+itoa(st.ID), map[string]any{"name": "Deep"}, &renamed); code != http.StatusOK || renamed.Title != "Deep" {
		t.Fatalf("rename: %d %+v", code, renamed)
	}
	if code := c.do("DELETE", "/api/v1/stations/"+itoa(st.ID), nil, nil); code != http.StatusNoContent {
		t.Fatalf("delete: %d", code)
	}
	c.do("GET", "/api/v1/libraries/"+itoa(lib.ID)+"/stations", nil, &list)
	if len(list) != 0 {
		t.Fatalf("stations after delete: %+v", list)
	}
}

func TestStationDirectory(t *testing.T) {
	dir := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("tag") != "trance" {
			io.WriteString(w, "[]")
			return
		}
		io.WriteString(w, `[{"name":"Trance FM","url_resolved":"https://t.example/live","tags":"trance","hls":0}]`)
	}))
	defer dir.Close()
	stream.AllowHomeNetwork.Store(true)
	t.Cleanup(func() { stream.AllowHomeNetwork.Store(false) })
	c, _ := newTestServer(t, func(s *Server) { s.RadioBase = dir.URL })
	var login struct{ Token string }
	c.do("POST", "/api/setup", credentials{"admin", "correct horse"}, &login)
	c.token = login.Token
	var found []struct{ Name, URL string }
	if code := c.do("GET", "/api/v1/stations/directory?tag=trance", nil, &found); code != http.StatusOK || len(found) != 1 || found[0].Name != "Trance FM" {
		t.Fatalf("directory: %d %+v", code, found)
	}
	if code := c.do("GET", "/api/v1/stations/directory", nil, nil); code != http.StatusBadRequest {
		t.Fatalf("no query: %d", code)
	}
}
