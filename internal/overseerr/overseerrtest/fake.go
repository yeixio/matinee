// Package overseerrtest is a pretend Overseerr for tests: just enough of
// its v1 API for what Matinee calls.
package overseerrtest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
)

type Fake struct {
	Key string
	URL string

	mu sync.Mutex
	// Users are Overseerr's accounts; the first owns the API key.
	Users []map[string]any
	// Catalog is every title Overseerr knows, as its API returns them
	// (id, mediaType, title or name, …), with "seasons" for shows.
	Catalog []map[string]any
	// Lists maps a list's path, like "/discover/trending" or
	// "/movie/27205/recommendations", to the ids of the titles on it.
	Lists map[string][]int
	// Requests are what was asked for: mediaType, mediaId, seasons, userId.
	Requests []map[string]any
	// Searches are the raw query strings searches came with.
	Searches []string
	// AutoApprove lists the users whose requests Overseerr approves.
	AutoApprove map[int]bool
	// status of each title, by id, once asked for.
	status map[int]int
}

func New(t *testing.T) *Fake {
	f := &Fake{
		Key:         "secret-overseerr",
		Users:       []map[string]any{{"id": 1, "email": "mike@example.com", "displayName": "Mike"}},
		Lists:       map[string][]int{},
		AutoApprove: map[int]bool{1: true},
		status:      map[int]int{},
	}
	ts := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(ts.Close)
	f.URL = ts.URL
	return f
}

func write(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func num(v any) int {
	n, _ := v.(float64)
	if i, ok := v.(int); ok {
		return i
	}
	return int(n)
}

func (f *Fake) title(id int) map[string]any {
	for _, m := range f.Catalog {
		if num(m["id"]) == id {
			out := map[string]any{}
			for k, v := range m {
				out[k] = v
			}
			if s := f.status[id]; s != 0 {
				out["mediaInfo"] = map[string]any{"status": s}
			}
			return out
		}
	}
	return nil
}

func (f *Fake) page(ids []int) map[string]any {
	results := []map[string]any{}
	for _, id := range ids {
		if m := f.title(id); m != nil {
			results = append(results, m)
		}
	}
	return map[string]any{"page": 1, "totalPages": 1, "results": results}
}

func (f *Fake) serve(w http.ResponseWriter, r *http.Request) {
	path, ok := strings.CutPrefix(r.URL.Path, "/api/v1")
	if !ok {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<html></html>"))
		return
	}
	if path == "/status" {
		write(w, map[string]any{"version": "1.33.2", "commitTag": "v1.33.2"})
		return
	}
	if r.Header.Get("X-Api-Key") != f.Key {
		w.WriteHeader(http.StatusForbidden)
		write(w, map[string]any{"message": "You do not have permission to access this endpoint"})
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	parts := strings.Split(strings.Trim(path, "/"), "/")
	switch {
	case path == "/auth/me":
		write(w, f.Users[0])
	case path == "/user":
		write(w, map[string]any{"pageInfo": map[string]any{"results": len(f.Users)}, "results": f.Users})
	case path == "/search":
		f.Searches = append(f.Searches, r.URL.RawQuery)
		q, ids := strings.ToLower(r.URL.Query().Get("query")), []int{}
		for _, m := range f.Catalog {
			name, _ := m["title"].(string)
			if name == "" {
				name, _ = m["name"].(string)
			}
			if strings.Contains(strings.ToLower(name), q) {
				ids = append(ids, num(m["id"]))
			}
		}
		write(w, f.page(ids))
	case path == "/request" && r.Method == "POST":
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		id, user := num(body["mediaId"]), num(body["userId"])
		if f.title(id) == nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if f.status[id] != 0 {
			w.WriteHeader(http.StatusConflict)
			write(w, map[string]any{"message": "Request for this media already exists."})
			return
		}
		f.Requests = append(f.Requests, body)
		state := 1
		f.status[id] = 2
		if f.AutoApprove[max(user, 1)] {
			state, f.status[id] = 2, 3
		}
		w.WriteHeader(http.StatusCreated)
		write(w, map[string]any{"id": len(f.Requests), "status": state})
	case len(parts) == 2 && (parts[0] == "movie" || parts[0] == "tv"):
		id, _ := strconv.Atoi(parts[1])
		m := f.title(id)
		if m == nil {
			http.NotFound(w, r)
			return
		}
		write(w, m)
	default:
		ids, ok := f.Lists[path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		write(w, f.page(ids))
	}
}

// SetStatus sets where Overseerr has a title (see overseerr.Status…).
func (f *Fake) SetStatus(id, status int) {
	f.mu.Lock()
	f.status[id] = status
	f.mu.Unlock()
}
