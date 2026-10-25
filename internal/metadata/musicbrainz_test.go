package metadata

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yeixio/matinee/internal/store/storetest"
)

func fakeMusicBrainz(t *testing.T) *MusicBrainz {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/2/artist", func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.UserAgent(), "Matinee/") {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		res := []map[string]any{}
		if r.URL.Query().Get("query") == `artist:"Radiohead"` {
			res = append(res,
				map[string]any{"id": "tribute", "name": "Radiohead Tribute Band", "score": 100},
				map[string]any{"id": "a74b1b7f", "name": "Radiohead", "score": 98})
		}
		json.NewEncoder(w).Encode(map[string]any{"artists": res})
	})
	mux.HandleFunc("GET /ws/2/artist/a74b1b7f", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"relations": []map[string]any{
			{"type": "official homepage", "url": map[string]any{"resource": "https://radiohead.com"}},
			{"type": "wikidata", "url": map[string]any{"resource": "https://www.wikidata.org/wiki/Q44190"}},
		}})
	})
	mux.HandleFunc("GET /ws/2/release-group", func(w http.ResponseWriter, r *http.Request) {
		res := []map[string]any{}
		if r.URL.Query().Get("query") == `releasegroup:"OK Computer" AND artist:"Radiohead"` {
			res = append(res, map[string]any{"id": "b1392450", "score": 100, "first-release-date": "1997-05-21"})
		}
		json.NewEncoder(w).Encode(map[string]any{"release-groups": res})
	})
	mux.HandleFunc("GET /wiki/Special:EntityData/Q44190.json", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"entities":{"Q44190":{"claims":{"P18":[{"mainsnak":{"datavalue":{"value":"Radiohead 2016.jpg"}}}]},
			"sitelinks":{"enwiki":{"title":"Radiohead"}}}}}`))
	})
	mux.HandleFunc("GET /api/rest_v1/page/summary/Radiohead", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"extract": "Radiohead are an English rock band."})
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	mb := NewMusicBrainz("test")
	mb.BaseURL, mb.CoverURL, mb.WikidataURL, mb.WikipediaURL = ts.URL+"/ws/2", "https://covers", ts.URL, ts.URL
	mb.Interval = 0
	return mb
}

func TestMusicBrainzFillsArtistsAndAlbums(t *testing.T) {
	st := storetest.Open(t)
	ctx := context.Background()
	lib, _ := st.CreateLibrary(ctx, "Music", "music", []string{"/music"})
	artist, _ := st.EnsureContainer(ctx, lib.ID, nil, "artist", "Radiohead", nil, nil)
	album, _ := st.EnsureContainer(ctx, lib.ID, &artist, "album", "OK Computer", nil, nil)
	unknown, _ := st.EnsureContainer(ctx, lib.ID, nil, "artist", "Unknown Artist", nil, nil)

	f := &Fetcher{Store: st, Providers: []Provider{fakeMusicBrainz(t)}, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	f.FetchLibrary(ctx, lib)

	a, _ := st.Item(ctx, 0, artist)
	if !a.HasPoster || a.Poster != "https://commons.wikimedia.org/wiki/Special:FilePath/Radiohead%202016.jpg?width=1000" {
		t.Fatalf("artist photo should come from the exact-name match: %+v", a)
	}
	if a.Overview != "Radiohead are an English rock band." {
		t.Fatalf("artist bio: %q", a.Overview)
	}
	al, _ := st.Item(ctx, 0, album)
	if al.Poster != "https://covers/release-group/b1392450/front-500" || al.Year == nil || *al.Year != 1997 {
		t.Fatalf("album: %+v", al)
	}
	if u, _ := st.Item(ctx, 0, unknown); u.MetaAt == 0 || u.HasPoster {
		t.Fatalf("Unknown Artist should be marked looked up without a match: %+v", u)
	}
	if left, _ := st.NeedsMetadata(ctx, lib.ID, f.Kinds()); len(left) != 0 {
		t.Fatalf("everything should be looked up: %+v", left)
	}
}
