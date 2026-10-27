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

// Movies and shows get every language's text at once; seasons and episodes
// only the languages people use.
func TestFetchTranslations(t *testing.T) {
	mux := http.NewServeMux()
	seasonLookups := 0
	mux.HandleFunc("GET /tv/1396/translations", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"translations": []map[string]any{
			// Austria's description, Germany's name: each field from the best fit that has it.
			{"iso_639_1": "de", "iso_3166_1": "AT", "data": map[string]any{"name": "Breaking Bad (AT)", "overview": "Ein Lehrer."}},
			{"iso_639_1": "de", "iso_3166_1": "DE", "data": map[string]any{"name": "Breaking Bad", "overview": ""}},
			{"iso_639_1": "zh", "iso_3166_1": "TW", "data": map[string]any{"name": "絕命毒師"}},
			{"iso_639_1": "zh", "iso_3166_1": "CN", "data": map[string]any{"name": "绝命毒师"}},
			{"iso_639_1": "en", "iso_3166_1": "US", "data": map[string]any{"name": "Breaking Bad"}},
			{"iso_639_1": "sw", "iso_3166_1": "KE", "data": map[string]any{"name": "Not listed"}},
			{"iso_639_1": "fr", "iso_3166_1": "FR", "data": map[string]any{}},
		}})
	})
	mux.HandleFunc("GET /tv/1396/season/1", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("language") != "de" {
			t.Errorf("season language %q", r.URL.Query().Get("language"))
		}
		seasonLookups++
		json.NewEncoder(w).Encode(map[string]any{"name": "Staffel 1", "overview": "", "episodes": []map[string]any{
			{"episode_number": 1, "name": "Der Anfang", "overview": "Walt erfährt es."},
			{"episode_number": 2, "name": "Folge 2", "overview": ""},
		}})
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	tm := NewTMDB("k")
	tm.BaseURL = ts.URL

	st := storetest.Open(t)
	ctx := context.Background()
	lib, _ := st.CreateLibrary(ctx, "TV", "tv", []string{"/tv"})
	show, _ := st.EnsureContainer(ctx, lib.ID, nil, "show", "Breaking Bad", nil, nil)
	st.ApplyMetadata(ctx, show, &store.Metadata{TMDBID: 1396})
	season, _ := st.EnsureContainer(ctx, lib.ID, &show, "season", "Season 1", new(1), nil)
	ep := map[int]int64{}
	for _, n := range []int{1, 2} {
		ep[n], _ = st.UpsertFile(ctx, store.Item{LibraryID: lib.ID, ParentID: &season, Kind: "episode", Title: "Pilot",
			IndexNumber: new(n), Path: "/tv/bb/" + string(rune('0'+n)) + ".mkv"})
	}

	f := &Fetcher{Store: st, Providers: []Provider{tm}, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	f.FetchTranslations(ctx, lib)
	item := func(lang string, id int64) store.Item {
		it, _ := st.Item(store.WithLanguage(ctx, lang), 0, id)
		return it
	}
	if got := item("de", show); got.Title != "Breaking Bad" || got.Overview != "Ein Lehrer." {
		t.Errorf("German show: %+v", got)
	}
	if hant, hans := item("zh-Hant", show), item("zh-Hans", show); hant.Title != "絕命毒師" || hans.Title != "绝命毒师" {
		t.Errorf("Chinese: %q %q", hant.Title, hans.Title)
	}
	if seasonLookups != 0 {
		t.Error("seasons looked up with nobody using another language")
	}

	// Someone starts using German: its seasons and episodes come next.
	st.SetUserLanguage(ctx, 1, "de")
	f.TranslateAll(ctx)
	if got := item("de", season); got.Title != "Staffel 1" {
		t.Errorf("German season: %+v", got)
	}
	if got := item("de", ep[1]); got.Title != "Der Anfang" || got.Overview != "Walt erfährt es." {
		t.Errorf("German episode 1: %+v", got)
	}
	// "Folge 2" is a made-up name; the original stays.
	if got := item("de", ep[2]); got.Title != "Pilot" {
		t.Errorf("German episode 2: %+v", got)
	}
	f.TranslateAll(ctx)
	if seasonLookups != 1 {
		t.Errorf("season looked up %d times", seasonLookups)
	}
	// A new episode brings its season back.
	st.UpsertFile(ctx, store.Item{LibraryID: lib.ID, ParentID: &season, Kind: "episode", Title: "Three", IndexNumber: new(3), Path: "/tv/bb/3.mkv"})
	f.TranslateAll(ctx)
	if seasonLookups != 2 {
		t.Errorf("season with a new episode looked up %d times", seasonLookups)
	}
}

func TestNumbered(t *testing.T) {
	for name, want := range map[string]bool{"Episode 3": true, "Folge 3": true, "第 3 集": true, "3. epizód": true,
		"Pilot": false, "Episode 3: The Return": false, "Chapter 13": false} {
		if got := numbered(name, 3); got != want {
			t.Errorf("numbered(%q) = %v", name, got)
		}
	}
}
