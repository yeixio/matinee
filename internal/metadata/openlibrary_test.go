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

func fakeOpenLibrary(t *testing.T) *OpenLibrary {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /search.json", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		docs := []map[string]any{}
		if q.Get("title") == "The Hobbit" && q.Get("author") == "J.R.R. Tolkien" {
			docs = append(docs,
				map[string]any{"title": "The Hobbit Companion", "author_name": []string{"David Day"}, "cover_i": 1},
				map[string]any{"title": "The Hobbit", "author_name": []string{"J. R. R. Tolkien"}, "cover_i": 0},
				map[string]any{"title": "The Hobbit", "author_name": []string{"J. R. R. Tolkien"}, "cover_i": 14627509, "first_publish_year": 1937})
		}
		json.NewEncoder(w).Encode(map[string]any{"docs": docs})
	})
	mux.HandleFunc("GET /search/authors.json", func(w http.ResponseWriter, r *http.Request) {
		docs := []map[string]any{}
		if r.URL.Query().Get("q") == "J.R.R. Tolkien" {
			docs = append(docs,
				map[string]any{"key": "OL1A", "name": "J.R.R. Tolkien", "work_count": 3},
				map[string]any{"key": "OL26320A", "name": "J. R. R. Tolkien", "work_count": 600})
		}
		json.NewEncoder(w).Encode(map[string]any{"docs": docs})
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	o := NewOpenLibrary("test")
	o.BaseURL, o.CoverURL, o.Interval = ts.URL, "https://covers", 0
	return o
}

// Books without a cover of their own get Open Library's, and authors a
// photo; books with their own cover aren't looked up.
func TestOpenLibraryFillsBooksAndAuthors(t *testing.T) {
	st := storetest.Open(t)
	ctx := context.Background()
	lib, _ := st.CreateLibrary(ctx, "Books", "audiobooks", []string{"/books"})
	author, _ := st.EnsureContainer(ctx, lib.ID, nil, "author", "J.R.R. Tolkien", nil, nil)
	hobbit, _ := st.EnsureContainer(ctx, lib.ID, &author, "book", "The Hobbit (Unabridged)", nil, nil)
	st.SetBookInfo(ctx, hobbit, store.BookInfo{Overview: "There and back again."})
	own, _ := st.EnsureContainer(ctx, lib.ID, &author, "book", "Silmarillion", nil, nil)
	st.SetArt(ctx, own, "/books/J.R.R. Tolkien/Silmarillion/cover.jpg", "")

	f := &Fetcher{Store: st, Providers: []Provider{fakeOpenLibrary(t)}, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	f.FetchLibrary(ctx, lib)

	h, _ := st.Item(ctx, 0, hobbit)
	if h.Poster != "https://covers/b/id/14627509-L.jpg" || h.Year == nil || *h.Year != 1937 {
		t.Fatalf("book: %+v", h)
	}
	if h.Overview != "There and back again." {
		t.Fatalf("the book's own description should stay: %q", h.Overview)
	}
	a, _ := st.Item(ctx, 0, author)
	if a.Poster != "https://covers/a/olid/OL26320A-L.jpg?default=false" {
		t.Fatalf("author: %+v", a)
	}
	if o, _ := st.Item(ctx, 0, own); o.Poster != "/books/J.R.R. Tolkien/Silmarillion/cover.jpg" || o.MetaAt == 0 {
		t.Fatalf("own cover: %+v", o)
	}
}
