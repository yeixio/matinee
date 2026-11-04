package metadata

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	"github.com/yeixio/matinee/internal/arr"
	"github.com/yeixio/matinee/internal/arr/arrtest"
	"github.com/yeixio/matinee/internal/store/storetest"
)

func TestArr(t *testing.T) {
	ctx := context.Background()
	st := storetest.Open(t)
	radarr := arrtest.New(t, arr.Radarr)
	radarr.Add(map[string]any{
		"title": "Heat", "year": 1995, "tmdbId": 949, "overview": "A heist.", "genres": []string{"Action", "Crime"},
		"ratings": map[string]any{"imdb": map[string]any{"value": 8.3}, "tmdb": map[string]any{"value": 7.9}},
		"images": []map[string]any{
			{"coverType": "poster", "url": "/MediaCover/1/poster.jpg", "remoteUrl": "http://image.tmdb.org/t/p/original/heat.jpg"},
			{"coverType": "fanart", "remoteUrl": "https://image.tmdb.org/t/p/original/heat-bg.jpg"},
		},
	})
	radarr.Catalog = []map[string]any{{"title": "Ronin", "year": 1998, "tmdbId": 8195, "images": []map[string]any{{"coverType": "poster", "remoteUrl": "https://image.tmdb.org/t/p/original/ronin.jpg"}}}}

	tmdb := NewTMDB("")
	a := &Arr{Store: st, TMDB: tmdb}
	if k := a.Kinds(); len(k) != 0 {
		t.Fatalf("kinds with nothing connected = %v", k)
	}
	b, _ := json.Marshal(arr.Config{URL: radarr.URL, APIKey: radarr.Key})
	if err := st.SetSetting(ctx, arr.SettingKey(arr.Radarr), string(b)); err != nil {
		t.Fatal(err)
	}
	a.Forget()
	if k := a.Kinds(); !slices.Equal(k, []string{"movie"}) {
		t.Fatalf("kinds with Radarr connected = %v", k)
	}

	// In the library: everything Radarr knows, the poster over https.
	m, err := a.Lookup(ctx, Query{Kind: "movie", Title: "heat", Year: 1996})
	if err != nil || m == nil {
		t.Fatalf("lookup Heat = %v, %v", m, err)
	}
	if m.TMDBID != 949 || m.Poster != "https://image.tmdb.org/t/p/original/heat.jpg" || m.Backdrop != "https://image.tmdb.org/t/p/original/heat-bg.jpg" ||
		m.Rating != 7.9 || m.Overview != "A heist." || !slices.Equal(m.Genres, []string{"Action", "Crime"}) {
		t.Errorf("Heat = %+v", m)
	}
	// Not in the library: found the way Radarr's Add New page finds it.
	m, err = a.Lookup(ctx, Query{Kind: "movie", Title: "Ronin", Year: 1998})
	if err != nil || m == nil || m.TMDBID != 8195 || m.Poster == "" {
		t.Errorf("lookup Ronin = %+v, %v", m, err)
	}
	m, err = a.Lookup(ctx, Query{Kind: "movie", Title: "Ronin", Year: 2010})
	if err != nil || m != nil {
		t.Errorf("lookup Ronin (2010) = %+v, %v; want no match", m, err)
	}
	// Sonarr isn't connected, so shows wait.
	if m, err := a.Lookup(ctx, Query{Kind: "show", Title: "Heat"}); m != nil || err != nil {
		t.Errorf("show lookup = %+v, %v", m, err)
	}

	// With a TMDB key, TMDB looks everything up.
	tmdb.SetKey("key")
	if k := a.Kinds(); len(k) != 0 {
		t.Errorf("kinds with a TMDB key = %v", k)
	}
}
