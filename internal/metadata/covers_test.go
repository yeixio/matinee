package metadata

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Covers come from every edition on the Cover Art Archive, then Apple and
// Deezer, for albums; from Open Library and Apple for books. A source
// that's down leaves the others.
func TestCoversFind(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/2/release/{id}", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"release-group": map[string]any{"id": "fluoro"}})
	})
	mux.HandleFunc("GET /ws/2/release", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("release-group") != "fluoro" {
			t.Errorf("release group %q", r.URL.Query().Get("release-group"))
		}
		json.NewEncoder(w).Encode(map[string]any{"releases": []map[string]any{
			{"id": "uk", "title": "Perfecto Fluoro", "date": "1996-09-02", "country": "GB",
				"cover-art-archive": map[string]any{"front": true}, "media": []map[string]any{{"format": "CD"}}},
			{"id": "promo", "title": "Perfecto Fluoro", "cover-art-archive": map[string]any{"front": false}},
		}})
	})
	mux.HandleFunc("GET /itunes/search", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("term") != "Paul Oakenfold Fluro" && q.Get("term") != "Jane Doe The Hobbit" {
			t.Errorf("apple term %q", q.Get("term"))
		}
		json.NewEncoder(w).Encode(map[string]any{"results": []map[string]any{
			{"collectionName": "Perfecto Fluoro", "artistName": "Paul Oakenfold", "releaseDate": "1996-09-02T07:00:00Z",
				"artworkUrl100": "https://is1.example/image/100x100bb.jpg"},
		}})
	})
	mux.HandleFunc("GET /deezer/search/album", func(w http.ResponseWriter, r *http.Request) {
		if q := r.URL.Query().Get("q"); q != `artist:"Paul Oakenfold" album:"Fluro"` {
			t.Errorf("deezer q %q", q)
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	mux.HandleFunc("GET /ol/search.json", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"docs": []map[string]any{
			{"title": "The Hobbit", "author_name": []string{"J. R. R. Tolkien"}, "cover_i": 42, "first_publish_year": 1937},
			{"title": "The Hobbit", "cover_i": 0},
		}})
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	mb := NewMusicBrainz("test")
	mb.BaseURL, mb.CoverURL, mb.Interval = ts.URL+"/ws/2", "https://caa", 0
	ol := NewOpenLibrary("test")
	ol.BaseURL, ol.CoverURL, ol.Interval = ts.URL+"/ol", "https://olc", 0
	c := NewCovers("test", mb, ol)
	c.ITunesURL, c.DeezerURL = ts.URL+"/itunes", ts.URL+"/deezer"
	ctx := context.Background()

	got := c.Find(ctx, Query{Kind: "album", Title: "Fluro - Disk 1", Artist: "Paul Oakenfold", ReleaseID: "6a1e1a70-8c62-4f4f-9e8c-3b9c1b4b2f10"})
	want := []CoverOption{
		{URL: "https://caa/release/uk/front-1200", Thumb: "https://caa/release/uk/front-250", Source: "Cover Art Archive", Title: "Perfecto Fluoro", Detail: "1996-09-02 · GB · CD"},
		{URL: "https://is1.example/image/1200x1200bb.jpg", Thumb: "https://is1.example/image/300x300bb.jpg", Source: "Apple", Title: "Perfecto Fluoro · Paul Oakenfold", Detail: "1996"},
	}
	if len(got) != len(want) {
		t.Fatalf("album covers: %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("cover %d: %+v, want %+v", i, got[i], want[i])
		}
	}

	got = c.Find(ctx, Query{Kind: "book", Title: "The Hobbit (Unabridged)", Artist: "Jane Doe"})
	if len(got) != 2 || got[0].Source != "Open Library" || got[0].URL != "https://olc/b/id/42-L.jpg" || got[0].Detail != "1937" || got[1].Source != "Apple" {
		t.Fatalf("book covers: %+v", got)
	}
}
