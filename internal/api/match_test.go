package api

import (
	"context"
	"strconv"
	"sync"
	"testing"

	"github.com/yeixio/matinee/internal/metadata"
	"github.com/yeixio/matinee/internal/store"
)

type fakeMatcher struct {
	mu       sync.Mutex
	searched string
}

func (f *fakeMatcher) Ready() bool { return true }

func (f *fakeMatcher) Search(ctx context.Context, kind, title string, year int) ([]metadata.Candidate, error) {
	f.mu.Lock()
	f.searched = kind + ":" + title + ":" + strconv.Itoa(year)
	f.mu.Unlock()
	return []metadata.Candidate{{ID: 1, Title: "Heat", Year: 1986}, {ID: 949, Title: "Heat", Year: 1995}}, nil
}

func (f *fakeMatcher) Find(ctx context.Context, kind, ref string) (*store.Metadata, *metadata.Candidate, error) {
	switch ref {
	case "949", "tt0113277":
		return &store.Metadata{TMDBID: 949, Overview: "A group of high-end professional thieves…", Year: 1995, Genres: []string{"Crime"}},
			&metadata.Candidate{ID: 949, Title: "Heat", Year: 1995}, nil
	case "1":
		return &store.Metadata{TMDBID: 1, Overview: "The 1986 one, refreshed."}, &metadata.Candidate{ID: 1, Title: "Heat", Year: 1986}, nil
	}
	return nil, nil, nil
}

// An admin fixes a wrong match: searches, picks the right title, which is
// locked, and can clear the match or refresh it.
func TestFixMatch(t *testing.T) {
	matcher := &fakeMatcher{}
	refetched := make(chan string, 8)
	c, _, db := newTestServerStore(t, func(s *Server) {
		s.Matcher = matcher
		s.Refetch = func(ctx context.Context, lib store.Library) { refetched <- lib.Name }
	})
	var login struct{ Token string }
	c.do("POST", "/api/setup", credentials{"admin", "correct horse"}, &login)
	c.token = login.Token
	ctx := context.Background()
	lib, _ := db.CreateLibrary(ctx, "Movies", "movies", []string{"/nowhere"})
	year := 1995
	id, _ := db.UpsertFile(ctx, store.Item{LibraryID: lib.ID, Kind: "movie", Title: "Heat", Year: &year, Path: "/nowhere/Heat (1995).mkv", VideoCodec: "h264"})
	// Matched with the wrong Heat.
	db.ApplyMetadata(ctx, id, &store.Metadata{TMDBID: 1, Overview: "The 1986 one.", Genres: []string{"Thriller"}, Poster: "https://img.example/wrong.jpg"})
	path := "/api/v1/items/" + itoa(id)

	var found struct {
		TMDBID  int64 `json:"tmdbId"`
		Locked  bool
		Ready   bool
		Results []metadata.Candidate
	}
	if code := c.do("GET", path+"/match", nil, &found); code != 200 || found.TMDBID != 1 || found.Locked || !found.Ready || len(found.Results) != 2 {
		t.Fatalf("match: %d %+v", code, found)
	}
	if matcher.searched != "movie:Heat:1995" {
		t.Fatalf("searched %q", matcher.searched)
	}
	if code := c.do("GET", path+"/match?q=tt0113277", nil, &found); code != 200 || len(found.Results) != 1 || found.Results[0].ID != 949 {
		t.Fatalf("by IMDb id: %d %+v", code, found)
	}

	var it store.Item
	if code := c.do("PUT", path+"/match", map[string]string{"ref": "949"}, &it); code != 200 {
		t.Fatalf("pick: %d", code)
	}
	if it.Overview != "A group of high-end professional thieves…" || len(it.Genres) != 1 || it.Genres[0] != "Crime" {
		t.Fatalf("after pick: %+v", it)
	}
	if got := <-refetched; got != "Movies" {
		t.Fatalf("refetched %s", got)
	}
	if m, _ := db.ItemMatch(ctx, id); m.TMDBID != 949 || !m.Locked {
		t.Fatalf("match: %+v", m)
	}
	if code := c.do("PUT", path+"/match", map[string]string{"ref": "12345678"}, nil); code != 404 {
		t.Fatalf("unknown title: %d", code)
	}

	// Refresh keeps the match and its lock.
	if code := c.do("POST", path+"/refresh", nil, &it); code != 200 || it.Overview == "" {
		t.Fatalf("refresh: %d %+v", code, it)
	}
	<-refetched
	if m, _ := db.ItemMatch(ctx, id); m.TMDBID != 949 || !m.Locked {
		t.Fatalf("after refresh: %+v", m)
	}

	// Unmatch: it goes by its file name, and a refresh doesn't match it again.
	var cleared store.Item
	if code := c.do("DELETE", path+"/match", nil, &cleared); code != 200 || cleared.Overview != "" || len(cleared.Genres) != 0 || cleared.Title != "Heat" {
		t.Fatalf("unmatch: %d %+v", code, cleared)
	}
	<-refetched
	c.do("POST", path+"/refresh", nil, &it)
	<-refetched
	if full, _ := db.Item(ctx, 0, id); full.TMDBID != nil || full.MetaAt == 0 || full.Poster != "" {
		t.Fatalf("after unmatch and refresh: %+v", full)
	}
}

// Fixing a show's match looks its seasons and episodes up again.
func TestFixShowMatchLooksUpEpisodesAgain(t *testing.T) {
	c, _, db := newTestServerStore(t, func(s *Server) { s.Matcher = &fakeMatcher{} })
	var login struct{ Token string }
	c.do("POST", "/api/setup", credentials{"admin", "correct horse"}, &login)
	c.token = login.Token
	ctx := context.Background()
	lib, _ := db.CreateLibrary(ctx, "TV", "tv", []string{"/nowhere"})
	show, _ := db.EnsureContainer(ctx, lib.ID, nil, "show", "Heat", nil, nil)
	one := 1
	season, _ := db.EnsureContainer(ctx, lib.ID, &show, "season", "Season 1", nil, &one)
	ep, _ := db.UpsertFile(ctx, store.Item{LibraryID: lib.ID, ParentID: &season, Kind: "episode", Title: "Pilot", IndexNumber: &one, Path: "/nowhere/Heat/S01E01.mkv", VideoCodec: "h264"})
	db.ApplyMetadata(ctx, show, &store.Metadata{TMDBID: 1})
	db.ApplyMetadata(ctx, season, &store.Metadata{TMDBID: 11, Overview: "Wrong season."})
	db.ApplyMetadata(ctx, ep, &store.Metadata{TMDBID: 111, Overview: "Wrong episode."})

	if code := c.do("PUT", "/api/v1/items/"+itoa(show)+"/match", map[string]string{"ref": "949"}, nil); code != 200 {
		t.Fatalf("pick: %d", code)
	}
	for _, id := range []int64{season, ep} {
		it, _ := db.Item(ctx, 0, id)
		if it.MetaAt != 0 || it.TMDBID != nil || it.Overview != "" {
			t.Fatalf("%s not waiting for a lookup: %+v", it.Kind, it)
		}
	}
	if code := c.do("GET", "/api/v1/items/"+itoa(ep)+"/match", nil, nil); code != 400 {
		t.Fatalf("an episode's match is its show's: %d", code)
	}
}
