package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/yeixio/matinee/internal/store"
)

func TestDetailsRelatedAndPeople(t *testing.T) {
	c, _, db := newTestServerStore(t)
	ctx := context.Background()
	var login struct{ Token string }
	c.do("POST", "/api/v1/setup", credentials{"admin", "correct horse"}, &login)
	c.token = login.Token

	photos := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("jpeg"))
	}))
	defer photos.Close()

	lib, _ := db.CreateLibrary(ctx, "Movies", "movies", []string{"/m"})
	add := func(title string, tmdb int64, u store.DetailsUpdate) int64 {
		id, err := db.UpsertFile(ctx, store.Item{LibraryID: lib.ID, Kind: "movie", Title: title, Path: "/m/" + title + ".mkv"})
		if err != nil {
			t.Fatal(err)
		}
		db.ApplyMetadata(ctx, id, &store.Metadata{TMDBID: tmdb})
		if err := db.ApplyDetails(ctx, id, u); err != nil {
			t.Fatal(err)
		}
		return id
	}
	weaver := store.Credit{PersonID: 10205, Name: "Sigourney Weaver", Role: "Ripley", Photo: photos.URL + "/w.jpg"}
	scott := store.Credit{PersonID: 578, Name: "Ridley Scott", Role: "Director", Department: "Directing"}
	coll := &store.Collection{ID: 8091, Name: "Alien Collection"}
	alien := add("Alien", 348, store.DetailsUpdate{
		Details: store.Details{Tagline: "In space no one can hear you scream.", Collection: coll, AwardSummary: "Won 1 Academy Award"},
		Cast:    []store.Credit{weaver}, Crew: []store.Credit{scott},
		Terms: []store.Term{{Kind: "collection", Term: "8091"}, {Kind: "genre", Term: "movie:27"}},
	})
	add("Aliens", 679, store.DetailsUpdate{
		Details: store.Details{Collection: coll},
		Cast:    []store.Credit{{PersonID: 10205, Name: "Sigourney Weaver", Role: "Ellen Ripley"}},
		Terms:   []store.Term{{Kind: "collection", Term: "8091"}},
	})
	add("Blade Runner", 78, store.DetailsUpdate{Crew: []store.Credit{scott}, Terms: []store.Term{{Kind: "genre", Term: "movie:27"}}})

	var item struct {
		Details *store.Details
		Credits *store.Credits
	}
	if code := c.do("GET", "/api/v1/items/"+itoa(alien), nil, &item); code != 200 {
		t.Fatalf("item: %d", code)
	}
	if item.Details == nil || item.Details.Tagline == "" || item.Details.AwardSummary == "" || item.Credits == nil ||
		len(item.Credits.Cast) != 1 || !item.Credits.Cast[0].HasPhoto || item.Credits.Crew[0].Role != "Director" {
		t.Fatalf("item details: %+v %+v", item.Details, item.Credits)
	}

	var rel relatedView
	if code := c.do("GET", "/api/v1/items/"+itoa(alien)+"/related", nil, &rel); code != 200 {
		t.Fatalf("related: %d", code)
	}
	if rel.Collection == nil || rel.Collection.Name != "Alien Collection" || len(rel.Collection.Items) != 2 {
		t.Fatalf("collection: %+v", rel.Collection)
	}
	if len(rel.Items) != 1 || rel.Items[0].Title != "Blade Runner" {
		t.Fatalf("related: %+v", rel.Items)
	}

	var p personView
	if code := c.do("GET", "/api/v1/people/10205", nil, &p); code != 200 {
		t.Fatalf("person: %d", code)
	}
	if p.Name != "Sigourney Weaver" || !p.HasPhoto || len(p.Titles) != 2 {
		t.Fatalf("person: %+v", p)
	}
	if code := c.do("GET", "/api/v1/people/999", nil, nil); code != http.StatusNotFound {
		t.Fatalf("unknown person: %d", code)
	}

	req, _ := http.NewRequest("GET", c.base+"/api/v1/people/10205/image", nil)
	req.Header.Set("Authorization", "Bearer "+c.token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 || string(body) != "jpeg" {
		t.Fatalf("photo: %d %q", res.StatusCode, body)
	}
	if code := c.do("GET", "/api/v1/people/578/image", nil, nil); code != http.StatusNotFound {
		t.Fatalf("no photo: %d", code)
	}
}
