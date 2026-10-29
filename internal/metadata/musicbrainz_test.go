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

// An edition note comes off the title when the full title finds nothing,
// and an album without a cover of its own takes one of its releases'.
func TestMusicBrainzAlbumCovers(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/2/release-group", func(w http.ResponseWriter, r *http.Request) {
		res := []map[string]any{}
		switch r.URL.Query().Get("query") {
		case `releasegroup:"Kid A" AND artist:"Radiohead"`:
			res = append(res, map[string]any{"id": "kida", "title": "Kid A", "score": 100})
		case `releasegroup:"Amnesiac" AND artist:"Radiohead"`:
			// Short titles score low but match exactly.
			res = append(res, map[string]any{"id": "other", "title": "Amnesiac Live", "score": 70},
				map[string]any{"id": "amnesiac", "title": "Amnesiac", "score": 65})
		}
		json.NewEncoder(w).Encode(map[string]any{"release-groups": res})
	})
	mux.HandleFunc("GET /ws/2/release", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("release-group") != "amnesiac" {
			json.NewEncoder(w).Encode(map[string]any{"releases": []any{}})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"releases": []map[string]any{
			{"id": "promo", "cover-art-archive": map[string]any{"front": false}},
			{"id": "cd", "cover-art-archive": map[string]any{"front": true}},
		}})
	})
	mux.HandleFunc("HEAD /caa/release-group/kida/front-500", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://archive.example/kida.jpg", http.StatusTemporaryRedirect)
	})
	mux.HandleFunc("HEAD /caa/release-group/amnesiac/front-500", http.NotFound)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	mb := NewMusicBrainz("test")
	mb.BaseURL, mb.CoverURL, mb.Interval = ts.URL+"/ws/2", ts.URL+"/caa", 0

	ctx := context.Background()
	md, err := mb.Lookup(ctx, Query{Kind: "album", Title: "Kid A (Deluxe Edition) [Remastered]", Artist: "Radiohead"})
	if err != nil || md == nil || md.MBID != "kida" || md.Poster != ts.URL+"/caa/release-group/kida/front-500" {
		t.Fatalf("edition note: %+v %v", md, err)
	}
	md, err = mb.Lookup(ctx, Query{Kind: "album", Title: "Amnesiac", Artist: "Radiohead"})
	if err != nil || md == nil || md.MBID != "amnesiac" || md.Poster != ts.URL+"/caa/release/cd/front-500" {
		t.Fatalf("release cover: %+v %v", md, err)
	}
	if md, _ := mb.Lookup(ctx, Query{Kind: "album", Title: "Nothing", Artist: "Radiohead"}); md != nil {
		t.Fatalf("no match: %+v", md)
	}
}

func TestPlainAlbumTitle(t *testing.T) {
	for in, want := range map[string]string{
		"OK Computer (Deluxe Edition)":        "OK Computer",
		"Abbey Road [2019 Remaster] (Disc 1)": "Abbey Road",
		"The Wall CD2":                        "The Wall",
		"(What's the Story) Morning Glory?":   "(What's the Story) Morning Glory?",
		"Help!":                               "Help!",
	} {
		if got := plainAlbumTitle(in); got != want {
			t.Errorf("plainAlbumTitle(%q) = %q, want %q", in, got, want)
		}
	}
}
