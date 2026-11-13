package metadata

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

func TestWikipedia(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch {
		case r.URL.Path == "/wiki" && q.Get("list") == "search":
			json.NewEncoder(w).Encode(map[string]any{"query": map[string]any{"search": []map[string]string{
				{"title": "Galaxy Quest"}, {"title": "Galaxian"}}}})
		case r.URL.Path == "/wiki":
			titles := strings.Split(q.Get("titles"), "|")
			var pages []map[string]any
			var redirects []map[string]string
			for _, title := range titles {
				switch title {
				case "Pac-Man":
					// The redirect's page, below.
				case "Pac-Man (video game)":
					redirects = append(redirects, map[string]string{"from": title, "to": "Pac-Man"})
					pages = append(pages, map[string]any{"title": "Pac-Man", "extract": "Pac-Man is a 1980 maze video game.\nIt was a hit.",
						"pageprops": map[string]string{"wikibase_item": "Q173544", "wikibase-shortdesc": "1980 video game"}})
				case "Galaxian":
					pages = append(pages, map[string]any{"title": "Galaxian", "extract": "Galaxian is a shooter.",
						"pageprops": map[string]string{"wikibase-shortdesc": "1979 video game"}})
				case "Galaxy Quest":
					pages = append(pages, map[string]any{"title": "Galaxy Quest", "extract": "A film.",
						"pageprops": map[string]string{"wikibase-shortdesc": "1999 film"}})
				default:
					pages = append(pages, map[string]any{"title": title, "missing": true})
				}
			}
			json.NewEncoder(w).Encode(map[string]any{"query": map[string]any{"redirects": redirects, "pages": pages}})
		case r.URL.Path == "/wikidata" && q.Get("props") == "claims":
			claim := func(id string) map[string]any {
				return map[string]any{"mainsnak": map[string]any{"datavalue": map[string]any{"value": map[string]string{"id": id}}}}
			}
			json.NewEncoder(w).Encode(map[string]any{"entities": map[string]any{"Q173544": map[string]any{
				"claims": map[string]any{"P136": []any{claim("Q1"), claim("Q2")}}}}})
		case r.URL.Path == "/wikidata":
			label := func(v string) map[string]any {
				return map[string]any{"labels": map[string]any{"en": map[string]string{"value": v}}}
			}
			json.NewEncoder(w).Encode(map[string]any{"entities": map[string]any{"Q1": label("maze video game"), "Q2": label("action game")}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	wp := &Wikipedia{API: srv.URL + "/wiki", Wikidata: srv.URL + "/wikidata", Client: srv.Client()}
	ctx := context.Background()

	m, err := wp.Lookup(ctx, Query{Kind: "game", Title: "Pac-Man", Year: 1980})
	if err != nil || m == nil {
		t.Fatalf("pac-man: %v, %v", m, err)
	}
	if m.Overview != "Pac-Man is a 1980 maze video game.\n\nIt was a hit." || !slices.Equal(m.Genres, []string{"Maze", "Action"}) {
		t.Fatalf("pac-man: %+v", m)
	}
	// Found by a search, skipping the film.
	if m, err := wp.Lookup(ctx, Query{Kind: "game", Title: "Galaxian", Year: 1979}); err != nil || m == nil || m.Overview != "Galaxian is a shooter." {
		t.Fatalf("galaxian: %+v, %v", m, err)
	}
	// Another year's game of the name isn't this one.
	if m, err := wp.Lookup(ctx, Query{Kind: "game", Title: "Galaxian", Year: 2011}); err != nil || m != nil {
		t.Fatalf("galaxian 2011: %+v, %v", m, err)
	}
	// Nothing fits.
	if m, err := wp.Lookup(ctx, Query{Kind: "game", Title: "Galaxy Quest"}); err != nil || m != nil {
		t.Fatalf("film: %+v, %v", m, err)
	}
}
