package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/yeixio/matinee/internal/metadata"
)

func TestTMDBKeySetting(t *testing.T) {
	tmdbAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("api_key") != "good-key-1234" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Write([]byte(`{}`))
	}))
	defer tmdbAPI.Close()
	tmdb := metadata.NewTMDB("env-key-abcd")
	tmdb.BaseURL = tmdbAPI.URL
	changed := make(chan struct{}, 4)
	c, _ := newTestServer(t, func(s *Server) {
		s.TMDB, s.TMDBFallback, s.TMDBFallbackSource = tmdb, "env-key-abcd", TMDBKeyFromEnvironment
		s.OnTMDBKey = func() { changed <- struct{}{} }
	})
	var login struct{ Token string }
	c.do("POST", "/api/v1/setup", credentials{"admin", "correct horse"}, &login)
	c.token = login.Token

	var set serverSettings
	c.do("GET", "/api/v1/settings", nil, &set)
	if set.TMDB != (tmdbStatus{Source: TMDBKeyFromEnvironment, Hint: "abcd"}) {
		t.Fatalf("from the environment: %+v", set.TMDB)
	}
	if code := c.do("PUT", "/api/v1/settings", map[string]any{"tmdbKey": "wrong"}, nil); code != http.StatusBadRequest || tmdb.Key() != "env-key-abcd" {
		t.Fatalf("a key TMDB turns down: %d, key %q", code, tmdb.Key())
	}
	if code := c.do("PUT", "/api/v1/settings", map[string]any{"tmdbKey": " good-key-1234 "}, &set); code != 200 ||
		set.TMDB != (tmdbStatus{Source: TMDBKeyFromSettings, Hint: "1234"}) || tmdb.Key() != "good-key-1234" {
		t.Fatalf("a good key: %d %+v, key %q", code, set.TMDB, tmdb.Key())
	}
	<-changed
	// What a client got back can be sent as is, without changing the key.
	if code := c.do("PUT", "/api/v1/settings", set, &set); code != 200 || set.TMDB.Source != TMDBKeyFromSettings {
		t.Fatalf("round trip: %d %+v", code, set.TMDB)
	}
	if code := c.do("PUT", "/api/v1/settings", map[string]any{"tmdbKey": ""}, &set); code != 200 ||
		set.TMDB.Source != TMDBKeyFromEnvironment || tmdb.Key() != "env-key-abcd" {
		t.Fatalf("cleared: %d %+v, key %q", code, set.TMDB, tmdb.Key())
	}
	<-changed
}
