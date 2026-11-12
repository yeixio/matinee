package metadata

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/yeixio/matinee/internal/store"
	"github.com/yeixio/matinee/internal/store/storetest"
)

func TestPlace(t *testing.T) {
	aired := &EpisodeList{Aired: map[int]int{1: 12, 2: 10}}
	for s := 1; s <= 2; s++ {
		for e := 1; e <= aired.Aired[s]; e++ {
			aired.Episodes = append(aired.Episodes, store.Place{Season: s, Episode: e, TMDBSeason: s, TMDBEpisode: e})
		}
	}
	cases := []struct {
		n       int
		compact bool
		order   string
		jp      bool
		want    store.Place
		ok      bool
	}{
		{3, false, "aired", false, store.Place{Season: 1, Episode: 3, TMDBSeason: 1, TMDBEpisode: 3}, true},
		{14, false, "aired", false, store.Place{Season: 2, Episode: 2, TMDBSeason: 2, TMDBEpisode: 2}, true},
		{23, false, "aired", false, store.Place{}, false},
		// 104 is season 1, episode 4 when there's no 104th episode…
		{104, true, "aired", true, store.Place{Season: 1, Episode: 4, TMDBSeason: 1, TMDBEpisode: 4}, true},
		// …and when the show has more, for a show from outside Japan.
		{205, true, "aired", false, store.Place{Season: 2, Episode: 5, TMDBSeason: 2, TMDBEpisode: 5}, true},
		{113, true, "aired", false, store.Place{}, false}, // season 1 has 12
	}
	for _, c := range cases {
		aired.Japanese = c.jp
		got, ok := place(c.n, c.compact, c.order, aired)
		if ok != c.ok || got != c.want {
			t.Errorf("place(%d, %v, %s, jp=%v) = %+v %v, want %+v %v", c.n, c.compact, c.order, c.jp, got, ok, c.want, c.ok)
		}
	}
	// A long Japanese show's 104 is its 104th episode.
	long := &EpisodeList{Aired: map[int]int{1: 200}, Japanese: true}
	for e := 1; e <= 200; e++ {
		long.Episodes = append(long.Episodes, store.Place{Season: 1, Episode: e, TMDBSeason: 1, TMDBEpisode: e})
	}
	if got, _ := place(104, true, "aired", long); got.Episode != 104 {
		t.Errorf("long anime: %+v", got)
	}
	long.Japanese = false
	if got, _ := place(104, true, "aired", long); got.Episode != 4 {
		t.Errorf("long western show: %+v", got)
	}
}

// fakeAnime is a show from Japan with 3 episodes in season 1 and 2 in
// season 2 as they aired, and a DVD order of 2 sets: 4 episodes, the
// first two swapped, then 1.
func fakeAnime(t *testing.T) *TMDB {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /search/tv", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"results": []map[string]any{{"id": 700, "name": "Frieren", "first_air_date": "2023-09-29"}}})
	})
	mux.HandleFunc("GET /tv/700", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"id": 700, "original_language": "ja", "origin_country": []string{"JP"},
			"seasons": []map[string]any{{"season_number": 0, "episode_count": 1}, {"season_number": 1, "episode_count": 3}, {"season_number": 2, "episode_count": 2}}})
	})
	mux.HandleFunc("GET /tv/700/season/{s}", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"id": 7000, "air_date": "2023-09-29"})
	})
	mux.HandleFunc("GET /tv/700/season/{s}/episode/{e}", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.Atoi(r.PathValue("s") + r.PathValue("e"))
		json.NewEncoder(w).Encode(map[string]any{"id": id, "name": fmt.Sprintf("Aired %s-%s", r.PathValue("s"), r.PathValue("e"))})
	})
	mux.HandleFunc("GET /tv/700/episode_groups", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"results": []map[string]any{{"id": "dvd1", "type": 3, "episode_count": 5}}})
	})
	mux.HandleFunc("GET /tv/episode_group/dvd1", func(w http.ResponseWriter, r *http.Request) {
		ep := func(s, e, o int) map[string]any {
			return map[string]any{"season_number": s, "episode_number": e, "order": o}
		}
		json.NewEncoder(w).Encode(map[string]any{"groups": []map[string]any{
			{"order": 0, "episodes": []any{ep(0, 1, 0)}},
			{"order": 1, "episodes": []any{ep(1, 1, 1), ep(1, 2, 0), ep(1, 3, 2), ep(2, 1, 3)}},
			{"order": 2, "episodes": []any{ep(2, 2, 0)}},
		}})
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	tm := NewTMDB("k")
	tm.BaseURL, tm.ImageURL = ts.URL, "https://img/"
	return tm
}

func TestFetcherPlacesAbsoluteEpisodes(t *testing.T) {
	st := storetest.Open(t)
	ctx := context.Background()
	lib, _ := st.CreateLibrary(ctx, "Anime", "tv", []string{"/tv"})
	show, _ := st.EnsureContainer(ctx, lib.ID, nil, "show", "Frieren", nil, nil)
	one := 1
	season, _ := st.EnsureContainer(ctx, lib.ID, &show, "season", "Season 1", &one, nil)
	ids := map[int]int64{}
	upsert := func(n int) int64 {
		id, err := st.UpsertFile(ctx, store.Item{LibraryID: lib.ID, ParentID: &season, Kind: "episode", Title: fmt.Sprintf("Episode %d", n),
			IndexNumber: &n, Absolute: &n, Path: fmt.Sprintf("/tv/Frieren/Frieren - %02d.mkv", n)})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	for n := 1; n <= 5; n++ {
		ids[n] = upsert(n)
	}
	f := &Fetcher{Store: st, Providers: []Provider{fakeAnime(t)}, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	st.SetMetadataKinds(f.Kinds()...)
	where := func(n int) (int, int, string) {
		ep, err := st.Item(ctx, 0, ids[n])
		if err != nil {
			t.Fatal(err)
		}
		return *ep.SeasonIndex, *ep.IndexNumber, ep.Title
	}
	check := func(label string, n, season, episode int, title string) {
		t.Helper()
		if s, e, ti := where(n); s != season || e != episode || ti != title {
			t.Errorf("%s: episode %d is S%dE%d %q, want S%dE%d %q", label, n, s, e, ti, season, episode, title)
		}
	}

	f.FetchLibrary(ctx, lib)
	check("aired", 3, 1, 3, "Aired 1-3")
	check("aired", 4, 2, 1, "Aired 2-1")
	check("aired", 5, 2, 2, "Aired 2-2")

	// A rescan of a changed file leaves it where it was put.
	upsert(4)
	check("rescan", 4, 2, 1, "Episode 4")

	// The DVD sets' order, looked up where each aired.
	if err := st.SetEpisodeOrder(ctx, show, store.OrderDVD); err != nil {
		t.Fatal(err)
	}
	f.FetchLibrary(ctx, lib)
	check("dvd", 1, 1, 1, "Aired 1-2")
	check("dvd", 4, 1, 4, "Aired 2-1")
	check("dvd", 5, 2, 1, "Aired 2-2")

	// One long season.
	st.SetEpisodeOrder(ctx, show, store.OrderAbsolute)
	f.FetchLibrary(ctx, lib)
	check("absolute", 5, 1, 5, "Aired 2-2")
	seasons, _ := st.Children(ctx, 0, show)
	if len(seasons) != 1 {
		t.Errorf("empty seasons left: %+v", seasons)
	}
	if order, abs, _ := st.EpisodeOrder(ctx, show); order != store.OrderAbsolute || !abs {
		t.Errorf("order: %s %v", order, abs)
	}
}
