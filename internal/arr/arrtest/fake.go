// Package arrtest is a pretend Radarr or Sonarr for tests: just enough of
// their v3 API for what Matinee calls.
package arrtest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/yeixio/matinee/internal/arr"
)

type Fake struct {
	App arr.App
	Key string
	URL string
	// Base is the app's URL base (Settings → General), such as "/radarr".
	// Requests without it are redirected to it, as the real apps do.
	Base string

	mu sync.Mutex
	// Catalog is what lookups find online: movies by tmdbId, shows by tvdbId.
	Catalog []map[string]any
	// Library is what the app has added, by its own id.
	Library  map[int]map[string]any
	Episodes []map[string]any // seriesId, seasonNumber, episodeNumber, episodeFileId
	Calendar []map[string]any
	Commands []map[string]any
	Deleted  []string // paths of deleted files, like "/moviefile/3"
	nextID   int
}

func New(t *testing.T, app arr.App) *Fake {
	f := &Fake{App: app, Key: "secret-" + string(app), Library: map[int]map[string]any{}, nextID: 1}
	ts := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(ts.Close)
	f.URL = ts.URL
	return f
}

func (f *Fake) kind() string {
	if f.App == arr.Radarr {
		return "movie"
	}
	return "series"
}

// Add puts a title straight into the app's library and returns its id.
func (f *Fake) Add(title map[string]any) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	id := f.nextID
	f.nextID++
	m := clone(title)
	m["id"] = id
	f.Library[id] = m
	return id
}

func clone(m map[string]any) map[string]any {
	b, _ := json.Marshal(m)
	var out map[string]any
	json.Unmarshal(b, &out)
	return out
}

func num(v any) int {
	n, _ := v.(float64)
	if i, ok := v.(int); ok {
		return i
	}
	return int(n)
}

func (f *Fake) existing(m map[string]any) map[string]any {
	key := "tmdbId"
	if f.App == arr.Sonarr {
		key = "tvdbId"
	}
	for _, l := range f.Library {
		if num(l[key]) == num(m[key]) {
			return l
		}
	}
	return nil
}

func write(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func (f *Fake) serve(w http.ResponseWriter, r *http.Request) {
	if f.Base != "" {
		rest, ok := strings.CutPrefix(r.URL.Path, f.Base)
		if !ok {
			http.Redirect(w, r, f.Base+r.URL.Path, http.StatusFound)
			return
		}
		r.URL.Path = rest
	}
	if r.Header.Get("X-Api-Key") != f.Key {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	path, ok := strings.CutPrefix(r.URL.Path, "/api/v3")
	if !ok {
		http.NotFound(w, r)
		return
	}
	q := r.URL.Query()
	var body map[string]any
	if r.Body != nil {
		json.NewDecoder(r.Body).Decode(&body)
	}
	k := "/" + f.kind()
	parts := strings.Split(strings.Trim(path, "/"), "/")
	switch {
	case path == "/system/status":
		write(w, map[string]any{"appName": f.App.Name(), "version": "5.0.0"})
	case path == "/qualityprofile":
		write(w, []map[string]any{{"id": 1, "name": "HD-1080p"}, {"id": 4, "name": "Ultra-HD"}})
	case path == "/rootfolder":
		write(w, []map[string]any{{"path": "/media/" + f.kind(), "freeSpace": 1 << 40}})
	case path == k+"/lookup/tmdb":
		for _, m := range f.Catalog {
			if strconv.Itoa(num(m["tmdbId"])) == q.Get("tmdbId") {
				write(w, f.withID(m))
				return
			}
		}
		http.NotFound(w, r)
	case path == k+"/lookup":
		term, out := strings.ToLower(q.Get("term")), []map[string]any{}
		for _, m := range f.Catalog {
			if id, ok := strings.CutPrefix(term, "tvdb:"); ok && strconv.Itoa(num(m["tvdbId"])) == id ||
				strings.Contains(strings.ToLower(m["title"].(string)), term) {
				out = append(out, f.withID(m))
			}
		}
		write(w, out)
	case path == k && r.Method == "GET":
		out := []map[string]any{}
		for _, m := range f.Library {
			if t := q.Get("tmdbId"); t == "" || strconv.Itoa(num(m["tmdbId"])) == t {
				out = append(out, m)
			}
		}
		write(w, out)
	case path == k && r.Method == "POST":
		if f.existing(body) != nil {
			w.WriteHeader(http.StatusBadRequest)
			write(w, []map[string]any{{"errorMessage": "This title has already been added"}})
			return
		}
		id := f.nextID
		f.nextID++
		body["id"] = id
		f.Library[id] = body
		w.WriteHeader(http.StatusCreated)
		write(w, body)
	case len(parts) == 2 && "/"+parts[0] == k:
		id, _ := strconv.Atoi(parts[1])
		m, ok := f.Library[id]
		if !ok {
			http.NotFound(w, r)
			return
		}
		if r.Method == "PUT" {
			f.Library[id] = body
			m = body
		}
		write(w, m)
	case path == "/episode":
		out := []map[string]any{}
		for _, e := range f.Episodes {
			if strconv.Itoa(num(e["seriesId"])) == q.Get("seriesId") {
				out = append(out, e)
			}
		}
		write(w, out)
	case len(parts) == 2 && (parts[0] == "moviefile" || parts[0] == "episodefile") && r.Method == "DELETE":
		f.Deleted = append(f.Deleted, path)
		write(w, map[string]any{})
	case path == "/command":
		f.Commands = append(f.Commands, body)
		write(w, body)
	case path == "/calendar":
		write(w, f.Calendar)
	default:
		http.NotFound(w, r)
	}
}

// withID is a lookup result, which carries the app's id once it has the title.
func (f *Fake) withID(m map[string]any) map[string]any {
	if e := f.existing(m); e != nil {
		return e
	}
	return clone(m)
}

// Commands run so far, by name.
func (f *Fake) CommandNames() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, c := range f.Commands {
		out = append(out, c["name"].(string))
	}
	return out
}
