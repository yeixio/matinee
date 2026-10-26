package metadata

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
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
				"poster_path": "/p.jpg", "backdrop_path": "/b.jpg", "release_date": "1995-12-15", "genre_ids": []int{28, 80, 18}})
		}
		json.NewEncoder(w).Encode(map[string]any{"results": res})
	})
	mux.HandleFunc("GET /search/tv", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"results": []map[string]any{
			{"id": 1396, "overview": "A teacher.", "first_air_date": "2008-01-20", "poster_path": "/bb.jpg", "genre_ids": []int{18, 10765}}}})
	})
	// Details, for genres of titles matched before Matinee kept them.
	mux.HandleFunc("GET /movie/949", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"id": 949, "genres": []map[string]any{{"id": 28, "name": "Action"}, {"id": 53, "name": "Thriller"}}})
	})
	mux.HandleFunc("GET /tv/1396", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"id": 1396, "genres": []map[string]any{{"id": 10759, "name": "Action & Adventure"}}})
	})
	mux.HandleFunc("GET /tv/1396/season/1", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"id": 3572, "overview": "Walt starts.", "air_date": "2008-01-20", "poster_path": "/s1.jpg"})
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
	if !slices.Equal(m.Genres, []string{"Action", "Crime", "Drama"}) {
		t.Fatalf("movie genres: %v", m.Genres)
	}
	// A show's combined TV genres count as each of their parts.
	if m, _ := tm.Lookup(context.Background(), Query{Kind: "show", Title: "Breaking Bad"}); m == nil || !slices.Equal(m.Genres, []string{"Drama", "Science Fiction", "Fantasy"}) {
		t.Fatalf("show genres: %+v", m)
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

func TestFetcherFillsShowsSeasonsThenEpisodes(t *testing.T) {
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

	f := &Fetcher{Store: st, Providers: []Provider{fakeTMDB(t)}, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	st.SetMetadataKinds(f.Kinds()...)
	if got, _ := st.Item(ctx, 0, show); !got.Pending {
		t.Fatalf("show should be pending before its lookup: %+v", got)
	}
	f.FetchLibrary(ctx, lib)

	got, _ := st.Item(ctx, 0, show)
	if got.TMDBID == nil || *got.TMDBID != 1396 || !got.HasPoster || got.Overview != "A teacher." {
		t.Fatalf("show: %+v", got)
	}
	if got.Pending {
		t.Fatal("show still pending after its lookup")
	}
	se, _ := st.Item(ctx, 0, season)
	if se.Overview != "Walt starts." || !se.HasPoster || se.Year == nil || *se.Year != 2008 {
		t.Fatalf("season: %+v", se)
	}
	ep, _ := st.Item(ctx, 0, epID)
	if ep.Title != "Pilot" || ep.Overview != "It begins." || !ep.HasPoster {
		t.Fatalf("episode: %+v", ep)
	}
	if left, _ := st.NeedsMetadata(ctx, lib.ID, f.Kinds()); len(left) != 0 {
		t.Fatalf("everything should be looked up: %+v", left)
	}
}

func TestTMDBTrailers(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /movie/949/videos", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"results": []map[string]any{
			{"name": "Behind the scenes", "site": "YouTube", "key": "bts", "type": "Featurette", "official": true},
			{"name": "Teaser", "site": "YouTube", "key": "teaser", "type": "Teaser", "official": true},
			{"name": "Fan trailer", "site": "YouTube", "key": "fan", "type": "Trailer", "official": false},
			{"name": "Old trailer", "site": "YouTube", "key": "old", "type": "Trailer", "official": true, "published_at": "1995-01-01"},
			{"name": "Official Trailer", "site": "YouTube", "key": "new", "type": "Trailer", "official": true, "published_at": "2020-01-01"},
			{"name": "Vimeo trailer", "site": "Vimeo", "key": "v", "type": "Trailer", "official": true},
		}})
	})
	mux.HandleFunc("GET /tv/1396/season/2/videos", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"results": []map[string]any{{"name": "Season 2", "site": "YouTube", "key": "s2", "type": "Trailer"}}})
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	tm := NewTMDB("k")
	tm.BaseURL = ts.URL
	ctx := context.Background()

	vids, err := tm.Trailers(ctx, "movie", 949, 0)
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for _, v := range vids {
		keys = append(keys, v.Key)
	}
	if got := strings.Join(keys, ","); got != "new,old,fan,teaser" {
		t.Errorf("order: %s", got)
	}
	if vids, _ := tm.Trailers(ctx, "show", 1396, 2); len(vids) != 1 || vids[0].Key != "s2" {
		t.Errorf("season: %+v", vids)
	}
	if vids, err := tm.Trailers(ctx, "show", 1, 0); err != nil || len(vids) != 0 {
		t.Errorf("unknown show: %+v %v", vids, err)
	}
}

// Movies and shows matched before Matinee kept genres get them on the next scan.
func TestFetcherBackfillsGenres(t *testing.T) {
	st := storetest.Open(t)
	ctx := context.Background()
	movies, _ := st.CreateLibrary(ctx, "Movies", "movies", []string{"/m"})
	tv, _ := st.CreateLibrary(ctx, "TV", "tv", []string{"/tv"})
	movie, _ := st.UpsertFile(ctx, store.Item{LibraryID: movies.ID, Kind: "movie", Title: "Heat", Path: "/m/heat.mkv"})
	show, _ := st.EnsureContainer(ctx, tv.ID, nil, "show", "Breaking Bad", nil, nil)
	// As an earlier Matinee left them: matched, with no genres.
	st.ApplyMetadata(ctx, movie, &store.Metadata{TMDBID: 949})
	st.ApplyMetadata(ctx, show, &store.Metadata{TMDBID: 1396})

	f := &Fetcher{Store: st, Providers: []Provider{fakeTMDB(t)}, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	f.FetchLibrary(ctx, movies)
	f.FetchLibrary(ctx, tv)
	if it, _ := st.Item(ctx, 0, movie); !slices.Equal(it.Genres, []string{"Action", "Thriller"}) {
		t.Fatalf("movie genres: %v", it.Genres)
	}
	if it, _ := st.Item(ctx, 0, show); !slices.Equal(it.Genres, []string{"Action", "Adventure"}) {
		t.Fatalf("show genres: %v", it.Genres)
	}
	if left, _ := st.NeedsGenres(ctx, movies.ID); len(left) != 0 {
		t.Fatalf("still needing genres: %+v", left)
	}
	counts, _ := st.GenreCounts(ctx, 0)
	if counts["Action"] != 2 || counts["Thriller"] != 1 {
		t.Fatalf("counts: %v", counts)
	}
	if in, _ := st.InGenre(ctx, 0, "Action", 10); len(in) != 2 {
		t.Fatalf("in Action: %+v", in)
	}
	// "Action" mustn't match a genre that merely contains it.
	if in, _ := st.InGenre(ctx, 0, "Act", 10); len(in) != 0 {
		t.Fatalf("in Act: %+v", in)
	}
}
