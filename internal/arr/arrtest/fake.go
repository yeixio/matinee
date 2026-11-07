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
	// BoxOfficeID makes it Box Office answering as the app, with its
	// feed of Notifications.
	BoxOfficeID   string
	Notifications []arr.BoxOfficeNotification
	// Discover, when set, makes it a Box Office with Discover: answers by
	// what follows /boxoffice/discover ("" for the home, "/rows/trending",
	// "/search").
	Discover map[string]any
	// Pairing makes it a Box Office with progress and upgrades (and, as
	// Radarr, editions): Activity is what /boxoffice/activity answers,
	// Editions /boxoffice/editions, and Asked records what was posted to
	// /boxoffice/editions and /boxoffice/upgrade, with its "path".
	Pairing bool
	// Problems makes a Box Office with Pairing fix reported problems
	// ("problems").
	Problems bool
	Activity map[string]any
	Editions map[string]any
	Asked    []map[string]any
	// TakesRequests makes it a Box Office that takes requests on its
	// Requests page: Requests are those filed, by id. RequestRule, when
	// set, turns new ones down with those words, as a weekly limit does.
	TakesRequests bool
	Requests      map[int64]map[string]any
	RequestRule   string
	nextID        int
}

// Notify adds to Box Office's feed, numbering it.
func (f *Fake) Notify(n arr.BoxOfficeNotification) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n.ID = int64(len(f.Notifications) + 1)
	f.Notifications = append(f.Notifications, n)
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
	case path == "/system/status" && f.BoxOfficeID != "":
		features := []string{"notifications"}
		if f.Discover != nil {
			features = append(features, "discover")
		}
		if f.TakesRequests {
			features = append(features, "requests")
		}
		if f.Pairing {
			features = append(features, "progress", "upgrades")
			if f.Problems {
				features = append(features, "problems")
			}
			if f.App == arr.Radarr {
				features = append(features, "editions")
			}
		}
		write(w, map[string]any{"appName": f.App.Name(), "version": "5.0.0", "boxOfficeId": f.BoxOfficeID, "boxOfficeFeatures": features})
	case strings.HasPrefix(path, "/boxoffice/discover") && f.Discover != nil:
		v, ok := f.Discover[strings.TrimPrefix(path, "/boxoffice/discover")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		write(w, v)
	case strings.HasPrefix(path, "/boxoffice/requests") && f.TakesRequests:
		f.serveRequests(w, r, path, body)
	case path == "/boxoffice/activity" && f.Pairing:
		write(w, f.Activity)
	case path == "/boxoffice/editions" && f.Pairing && r.Method == http.MethodGet:
		write(w, f.Editions)
	case (path == "/boxoffice/editions" || path == "/boxoffice/upgrade") && f.Pairing && r.Method == http.MethodPost:
		body["path"] = path
		f.Asked = append(f.Asked, body)
		w.WriteHeader(http.StatusAccepted)
		write(w, map[string]any{"queued": true})
	case path == "/system/status":
		write(w, map[string]any{"appName": f.App.Name(), "version": "5.0.0"})
	case path == "/boxoffice/notifications" && f.BoxOfficeID != "":
		after, _ := strconv.ParseInt(q.Get("after"), 10, 64)
		last := int64(len(f.Notifications))
		out := []arr.BoxOfficeNotification{}
		for _, n := range f.Notifications {
			if after >= 0 && n.ID > after {
				out = append(out, n)
			}
		}
		write(w, map[string]any{"boxOfficeId": f.BoxOfficeID, "last": last, "notifications": out})
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
				strings.HasPrefix(term, "tmdb:") && strconv.Itoa(num(m["tmdbId"])) == strings.TrimPrefix(term, "tmdb:") ||
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

// BoxOffice is a pretend Box Office: Radarr at /radarr and Sonarr at
// /sonarr with one API key, and its sign-in-free status at /api/v1/status.
type BoxOffice struct {
	URL            string
	Key            string
	Radarr, Sonarr *Fake
	// Close stops it answering, like a Box Office that's down.
	Close func()
}

func NewBoxOffice(t *testing.T) *BoxOffice {
	b := &BoxOffice{Key: "secret-boxoffice"}
	mk := func(app arr.App) *Fake {
		return &Fake{App: app, Key: b.Key, Base: "/" + string(app), BoxOfficeID: "bo-1", Library: map[int]map[string]any{}, nextID: 1}
	}
	b.Radarr, b.Sonarr = mk(arr.Radarr), mk(arr.Sonarr)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/status":
			write(w, map[string]any{"product": "boxoffice", "id": "bo-1", "setup": true, "version": "1.2.3"})
		case strings.HasPrefix(r.URL.Path, "/radarr/"):
			b.Radarr.serve(w, r)
		case strings.HasPrefix(r.URL.Path, "/sonarr/"):
			b.Sonarr.serve(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(ts.Close)
	b.URL, b.Close = ts.URL, ts.Close
	b.Radarr.URL, b.Sonarr.URL = ts.URL+"/radarr", ts.URL+"/sonarr"
	return b
}

func (f *Fake) serveRequests(w http.ResponseWriter, r *http.Request, path string, body map[string]any) {
	if f.Requests == nil {
		f.Requests = map[int64]map[string]any{}
	}
	rest := strings.Trim(strings.TrimPrefix(path, "/boxoffice/requests"), "/")
	idPart, action, _ := strings.Cut(rest, "/")
	id, _ := strconv.ParseInt(idPart, 10, 64)
	rule := func(msg string) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		json.NewEncoder(w).Encode(map[string]string{"message": msg})
	}
	req := f.Requests[id]
	switch {
	case rest == "" && r.Method == http.MethodPost:
		if f.RequestRule != "" {
			rule(f.RequestRule)
			return
		}
		status := "pending"
		if body["approved"] == true {
			status = "approved"
		}
		id := int64(len(f.Requests) + 100)
		body["id"], body["kind"], body["status"], body["state"] = id, f.kind(), status, status
		f.Requests[id] = body
		w.WriteHeader(http.StatusCreated)
		write(w, body)
	case rest == "" && r.Method == http.MethodGet:
		out := []map[string]any{}
		for _, s := range strings.Split(r.URL.Query().Get("ids"), ",") {
			id, _ := strconv.ParseInt(s, 10, 64)
			if m, ok := f.Requests[id]; ok {
				out = append(out, m)
			}
		}
		write(w, out)
	case req == nil:
		http.NotFound(w, r)
	case r.Method == http.MethodDelete:
		if req["status"] == "pending" {
			delete(f.Requests, id)
		}
		w.WriteHeader(http.StatusNoContent)
	case req["status"] != "pending":
		rule("that request was " + req["status"].(string) + " already")
	case action == "approve":
		req["status"], req["state"], req["approvedWith"] = "approved", "searching", body["qualityProfileId"]
		write(w, req)
	case action == "decline":
		req["status"], req["state"], req["reply"] = "declined", "declined", body["reply"]
		write(w, req)
	default:
		http.NotFound(w, r)
	}
}
