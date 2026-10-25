package metadata

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/yeixio/matinee/internal/store"
	"github.com/yeixio/matinee/internal/store/storetest"
)

func fakeTMDB(t *testing.T) *TMDB {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /search/movie", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("api_key") != "k" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		res := []map[string]any{}
		if r.URL.Query().Get("query") == "Heat" && r.URL.Query().Get("year") != "1996" {
			res = append(res, map[string]any{"id": 949, "overview": "A heist.", "vote_average": 7.9,
				"poster_path": "/p.jpg", "backdrop_path": "/b.jpg", "release_date": "1995-12-15"})
		}
		json.NewEncoder(w).Encode(map[string]any{"results": res})
	})
	mux.HandleFunc("GET /search/tv", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"results": []map[string]any{
			{"id": 1396, "overview": "A teacher.", "first_air_date": "2008-01-20", "poster_path": "/bb.jpg"}}})
	})
	mux.HandleFunc("GET /tv/1396/season/1/episode/1", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"id": 62085, "name": "Pilot", "overview": "It begins.", "still_path": "/s.jpg"})
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	tm := NewTMDB("k")
	tm.BaseURL, tm.ImageURL = ts.URL, "https://img/"
	return tm
}

func TestTMDBLookup(t *testing.T) {
	tm := fakeTMDB(t)
	m, err := tm.Lookup(context.Background(), Query{Kind: "movie", Title: "Heat", Year: 1995})
	if err != nil || m == nil {
		t.Fatalf("movie: %v %v", m, err)
	}
	if m.TMDBID != 949 || m.Year != 1995 || m.Poster != "https://img/w500/p.jpg" || m.Backdrop != "https://img/w1280/b.jpg" {
		t.Fatalf("movie: %+v", m)
	}
	// A wrong year in the file name falls back to a search without one.
	if m, _ := tm.Lookup(context.Background(), Query{Kind: "movie", Title: "Heat", Year: 1996}); m == nil || m.TMDBID != 949 {
		t.Fatalf("year fallback: %+v", m)
	}
	if m, _ := tm.Lookup(context.Background(), Query{Kind: "movie", Title: "Nope"}); m != nil {
		t.Fatalf("no match should be nil: %+v", m)
	}
	ep, err := tm.Lookup(context.Background(), Query{Kind: "episode", ShowID: 1396, Season: 1, Episode: 1})
	if err != nil || ep == nil || ep.Title != "Pilot" {
		t.Fatalf("episode: %+v %v", ep, err)
	}
	if ep, err := tm.Lookup(context.Background(), Query{Kind: "episode", ShowID: 1396, Season: 9, Episode: 9}); ep != nil || err != nil {
		t.Fatalf("missing episode: %+v %v", ep, err)
	}
}

func TestFetcherFillsShowsThenEpisodes(t *testing.T) {
	st := storetest.Open(t)
	ctx := context.Background()
	lib, _ := st.CreateLibrary(ctx, "TV", "tv", []string{"/tv"})
	show, _ := st.EnsureContainer(ctx, lib.ID, nil, "show", "Breaking Bad", nil, nil)
	one := 1
	season, _ := st.EnsureContainer(ctx, lib.ID, &show, "season", "Season 1", &one, nil)
	epID, err := st.UpsertFile(ctx, store.Item{LibraryID: lib.ID, ParentID: &season, Kind: "episode", Title: "Episode 1", IndexNumber: &one, Path: "/tv/bb/s01e01.mkv"})
	if err != nil {
		t.Fatal(err)
	}

	f := &Fetcher{Store: st, Provider: fakeTMDB(t), Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	f.FetchLibrary(ctx, lib)

	got, _ := st.Item(ctx, 0, show)
	if got.TMDBID == nil || *got.TMDBID != 1396 || !got.HasPoster || got.Overview != "A teacher." {
		t.Fatalf("show: %+v", got)
	}
	ep, _ := st.Item(ctx, 0, epID)
	if ep.Title != "Pilot" || ep.Overview != "It begins." || !ep.HasPoster {
		t.Fatalf("episode: %+v", ep)
	}
	if left, _ := st.NeedsMetadata(ctx, lib.ID); len(left) != 0 {
		t.Fatalf("everything should be looked up: %+v", left)
	}
}
