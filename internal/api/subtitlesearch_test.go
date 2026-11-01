package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/yeixio/matinee/internal/store"
	"github.com/yeixio/matinee/internal/subtitle"
)

// fakeOpenSubtitles stands in for the OpenSubtitles API: key "good-key-1234",
// account mike / pw, two English files and a Brazilian Portuguese one.
type fakeOpenSubtitles struct {
	srv       *httptest.Server
	searches  atomic.Int32
	downloads atomic.Int32
	logins    atomic.Int32
	queries   []string
	quota     atomic.Int32 // downloads left; below 0 refuses
}

func newFakeOpenSubtitles(t *testing.T) *fakeOpenSubtitles {
	f := &fakeOpenSubtitles{}
	f.quota.Store(5)
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/files/42.srt" {
			fmt.Fprint(w, "1\n00:00:01,000 --> 00:00:03,000\nHello from online\n")
			return
		}
		if r.Header.Get("Api-Key") != "good-key-1234" {
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprint(w, `{"message":"You cannot consume this service"}`)
			return
		}
		if !strings.HasPrefix(r.Header.Get("User-Agent"), "Matinee v") {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/infos/formats":
			fmt.Fprint(w, `{"data":{"output_formats":["srt"]}}`)
		case "/login":
			f.logins.Add(1)
			var body struct{ Username, Password string }
			json.NewDecoder(r.Body).Decode(&body)
			if body.Username != "mike" || body.Password != "pw" {
				w.WriteHeader(http.StatusUnauthorized)
				fmt.Fprint(w, `{"message":"Error, invalid username/password","status":401}`)
				return
			}
			fmt.Fprint(w, `{"user":{"allowed_downloads":20},"base_url":"api.opensubtitles.com","token":"tok","status":200}`)
		case "/subtitles":
			f.searches.Add(1)
			f.queries = append(f.queries, r.URL.RawQuery)
			q := r.URL.Query()
			if q.Get("moviehash") != "" {
				// Only the file made for this video matches its hash.
				fmt.Fprint(w, `{"total_count":1,"data":[{"id":"9","attributes":{"language":"en","download_count":10,
					"hearing_impaired":false,"foreign_parts_only":false,"machine_translated":false,"ai_translated":false,"moviehash_match":true,
					"release":"Sub.Test.2020.1080p","files":[{"file_id":42,"file_name":"Sub.Test.2020.1080p.srt"}]}}]}`)
				return
			}
			fmt.Fprint(w, `{"total_count":3,"data":[
				{"id":"9","attributes":{"language":"en","download_count":10,"hearing_impaired":false,"moviehash_match":false,
					"release":"Sub.Test.2020.1080p","files":[{"file_id":42,"file_name":"x.srt"}]}},
				{"id":"10","attributes":{"language":"en","download_count":900,"hearing_impaired":true,"machine_translated":false,
					"release":"Sub.Test.2020.720p","files":[{"file_id":43,"file_name":"y.srt"}]}},
				{"id":"11","attributes":{"language":"pt-BR","download_count":50,"machine_translated":true,
					"release":"","files":[{"file_id":44,"file_name":"Sub.Test.PT.srt"}]}},
				{"id":"12","attributes":{"language":"en","download_count":5,"files":[{"file_id":45},{"file_id":46}]}}]}`)
		case "/download":
			f.downloads.Add(1)
			left := f.quota.Add(-1)
			if left < 0 {
				w.WriteHeader(http.StatusNotAcceptable)
				fmt.Fprint(w, `{"requests":6,"remaining":-1,"message":"You have downloaded your allowed 5 subtitles for 24h."}`)
				return
			}
			fmt.Fprintf(w, `{"link":"%s/files/42.srt","file_name":"x.srt","requests":1,"remaining":%d}`, f.srv.URL, left)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func TestSubtitleSearch(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	os := newFakeOpenSubtitles(t)
	c, tmp := newTestServer(t, func(s *Server) { s.OpenSubtitlesBase = os.srv.URL })
	var login struct{ Token string }
	c.do("POST", "/api/setup", credentials{"admin", "correct horse"}, &login)
	c.token = login.Token

	movie := filepath.Join(tmp, "movies", "Sub Test (2020)", "Sub Test (2020).mkv")
	// Noise, so the file is big enough to hash (128 KB at least).
	makeMedia(t, movie, "-f", "lavfi", "-i", "testsrc=duration=2:size=320x240:rate=24", "-c:v", "libx264", "-preset", "ultrafast", "-qp", "0")
	var lib store.Library
	c.do("POST", "/api/libraries", map[string]any{"name": "Movies", "kind": "movies", "paths": []string{filepath.Join(tmp, "movies")}}, &lib)
	waitForScan(t, c)
	var items []store.Item
	c.do("GET", "/api/libraries/"+itoa(lib.ID)+"/items", nil, &items)
	base := "/api/items/" + itoa(items[0].ID)

	// No key yet: nothing to search with.
	var item itemView
	c.do("GET", base, nil, &item)
	if item.SubtitleSearch || len(item.Subtitles) != 0 {
		t.Fatalf("before a key: %+v", item)
	}
	if code := c.do("GET", base+"/subtitles/search", nil, nil); code != http.StatusConflict {
		t.Fatalf("search without a key: %d", code)
	}

	// The admin enters a key; a bad one, or half an account, is turned down.
	if code := c.do("PUT", "/api/integrations/opensubtitles", map[string]string{"apiKey": "nope"}, nil); code != http.StatusBadRequest {
		t.Fatalf("bad key: %d", code)
	}
	if code := c.do("PUT", "/api/integrations/opensubtitles", map[string]string{"apiKey": "good-key-1234", "username": "mike"}, nil); code != http.StatusBadRequest {
		t.Fatalf("username without password: %d", code)
	}
	if code := c.do("PUT", "/api/integrations/opensubtitles", map[string]string{"apiKey": "good-key-1234", "username": "mike", "password": "bad"}, nil); code != http.StatusBadRequest {
		t.Fatalf("bad password: %d", code)
	}
	var view openSubtitlesView
	if code := c.do("PUT", "/api/integrations/opensubtitles", map[string]string{"apiKey": "good-key-1234", "username": "mike", "password": "pw"}, &view); code != 200 ||
		!view.Connected || view.Hint != "1234" || view.Username != "mike" {
		t.Fatalf("good key: %d %+v", code, view)
	}
	// Saving again without retyping the key or password keeps them.
	if code := c.do("PUT", "/api/integrations/opensubtitles", map[string]string{"username": "mike"}, &view); code != 200 || view.Hint != "1234" {
		t.Fatalf("resave: %d %+v", code, view)
	}

	c.do("GET", base, nil, &item)
	if !item.SubtitleSearch {
		t.Fatal("search not offered once there's a key")
	}

	// The search marks the file made for this video and puts it first,
	// then the most downloaded, machine translations last; split files are left out.
	var res subtitleSearchResult
	if code := c.do("GET", base+"/subtitles/search?languages=en,pt-BR", nil, &res); code != 200 {
		t.Fatalf("search: %d", code)
	}
	if len(res.Results) != 3 || res.Results[0].ID != 42 || !res.Results[0].MatchesFile || res.Results[1].ID != 43 || !res.Results[1].SDH ||
		res.Results[2].ID != 44 || !res.Results[2].Machine || res.Results[2].Label != "Portuguese (Brazil)" || res.Results[2].Language != "pt" ||
		res.Results[2].Release != "Sub.Test.PT" || len(res.Have) != 0 {
		t.Fatalf("results: %+v", res)
	}
	if strings.Join(res.Languages, ",") != "en,pt-br" {
		t.Fatalf("languages: %v", res.Languages)
	}
	if q := os.queries[len(os.queries)-1]; q != "languages=en%2Cpt-br&query=sub+test&year=2020" {
		t.Fatalf("query sent: %s", q)
	}
	// Asking again uses what the server found a moment ago.
	searches := os.searches.Load()
	c.do("GET", base+"/subtitles/search?languages=en,pt-BR", nil, &res)
	if os.searches.Load() != searches {
		t.Fatal("searched again instead of using the cache")
	}

	// Picking one fetches it once and adds it to the video's tracks, as WebVTT.
	var track subtitle.Track
	if code := c.do("POST", base+"/subtitles", res.Results[0], &track); code != http.StatusCreated ||
		!track.Found || !track.External || track.Language != "en" || track.Label != "English" || !track.Removable {
		t.Fatalf("add: %d %+v", code, track)
	}
	if code := c.do("POST", base+"/subtitles", res.Results[0], nil); code != http.StatusCreated || os.downloads.Load() != 1 {
		t.Fatalf("adding it again downloaded it again: %d, %d downloads", code, os.downloads.Load())
	}
	c.do("GET", base, nil, &item)
	if len(item.Subtitles) != 1 || item.Subtitles[0].ID != track.ID {
		t.Fatalf("tracks after adding: %+v", item.Subtitles)
	}
	if vtt := get(t, c, base+"/subtitles/"+track.ID+".vtt"); !strings.HasPrefix(vtt, "WEBVTT") || !strings.Contains(vtt, "Hello from online") {
		t.Fatalf("found track:\n%s", vtt)
	}
	c.do("GET", base+"/subtitles/search?languages=en,pt-BR", nil, &res)
	if len(res.Have) != 1 || res.Have[0] != 42 {
		t.Fatalf("have: %v", res.Have)
	}

	// Out of downloads for the day.
	os.quota.Store(0)
	if code := c.do("POST", base+"/subtitles", res.Results[1], nil); code != http.StatusTooManyRequests {
		t.Fatalf("over quota: %d", code)
	}

	// Removing: admins can, then the track is gone.
	if code := c.do("DELETE", base+"/subtitles/e0", nil, nil); code != http.StatusBadRequest {
		t.Fatalf("remove a file's own track: %d", code)
	}
	if code := c.do("DELETE", base+"/subtitles/"+track.ID, nil, nil); code != http.StatusNoContent {
		t.Fatalf("remove: %d", code)
	}
	item = itemView{}
	c.do("GET", base, nil, &item)
	if len(item.Subtitles) != 0 {
		t.Fatalf("tracks after removing: %+v", item.Subtitles)
	}

	// The admin takes the key away: search is gone.
	if code := c.do("DELETE", "/api/integrations/opensubtitles", nil, nil); code != http.StatusNoContent {
		t.Fatalf("remove the key: %d", code)
	}
	item = itemView{}
	c.do("GET", base, nil, &item)
	if item.SubtitleSearch {
		t.Fatal("search still offered after the key was removed")
	}
}

func TestOpenSubtitlesLanguages(t *testing.T) {
	for in, want := range map[string]string{"en": "en", "pt-BR": "pt-br", "pt": "pt-pt", "zh-Hant": "zh-tw", "zh-Hans": "zh-cn", "nb": "no", "de-AT": "de", "xx": "", "": ""} {
		if got := subtitle.APILanguage(in); got != want {
			t.Errorf("APILanguage(%q) = %q, want %q", in, got, want)
		}
	}
}
