package api

import (
	"context"
	"testing"

	"github.com/yeixio/matinee/internal/arr"
	"github.com/yeixio/matinee/internal/arr/arrtest"
	"github.com/yeixio/matinee/internal/store"
)

// Matinee tells a Box Office that takes it what has been watched, as counts
// of people: movies to it as Radarr, shows as Sonarr. It says so again only
// when something has changed.
func TestWatchedReportedToBoxOffice(t *testing.T) {
	e := pairingSetup(t)
	ctx := context.Background()
	admin, err := e.db.UserByName(ctx, "admin")
	if err != nil {
		t.Fatal(err)
	}
	tv, _ := e.db.CreateLibrary(ctx, "TV", "shows", []string{"/s"})
	show, _ := e.db.EnsureContainer(ctx, tv.ID, nil, "show", "Severance", nil, nil)
	e.db.ApplyMetadata(ctx, show, &store.Metadata{TMDBID: 95396})
	zero, one, two := 0, 1, 2
	season, _ := e.db.EnsureContainer(ctx, tv.ID, &show, "season", "Season 1", &one, nil)
	specials, _ := e.db.EnsureContainer(ctx, tv.ID, &show, "season", "Specials", &zero, nil)
	e1, _ := e.db.UpsertFile(ctx, store.Item{LibraryID: tv.ID, ParentID: &season, Kind: "episode", Title: "One", IndexNumber: &one, Path: "/s/e1.mkv"})
	e2, _ := e.db.UpsertFile(ctx, store.Item{LibraryID: tv.ID, ParentID: &season, Kind: "episode", Title: "Two", IndexNumber: &two, Path: "/s/e2.mkv"})
	if _, err := e.db.UpsertFile(ctx, store.Item{LibraryID: tv.ID, ParentID: &specials, Kind: "episode", Title: "Extra", IndexNumber: &one, Path: "/s/s0e1.mkv"}); err != nil {
		t.Fatal(err)
	}
	// Ellen finished the movie and the show; the admin started both.
	for _, p := range []struct {
		user, item, pos int64
		played          bool
	}{
		{e.ellenID, e.movie, 0, true}, {admin.ID, e.movie, 60_000, false},
		{e.ellenID, e1, 0, true}, {e.ellenID, e2, 0, true}, {admin.ID, e1, 0, true},
	} {
		if err := e.db.SetProgress(ctx, p.user, p.item, p.pos, p.played); err != nil {
			t.Fatal(err)
		}
	}

	bo := arrtest.NewBoxOffice(t)
	bo.Radarr.TakesWatched, bo.Sonarr.TakesWatched = true, true
	if code := e.admin.do("PUT", "/api/v1/integrations/boxoffice", boxOfficeConfig{URL: bo.URL, APIKey: bo.Key}, nil); code != 200 {
		t.Fatalf("connect: %d", code)
	}
	e.srv.ReadBoxOffice(ctx)
	check := func(f *arrtest.Fake, id, viewers, finished float64) {
		t.Helper()
		if len(f.Watched) != 1 {
			t.Fatalf("%s got %d reports", f.App, len(f.Watched))
		}
		r := f.Watched[0]
		titles, _ := r["titles"].([]any)
		if r["people"] != 2.0 || len(titles) != 1 {
			t.Fatalf("%s: %+v", f.App, r)
		}
		got := titles[0].(map[string]any)
		if got["tmdbId"] != id || got["viewers"] != viewers || got["finishedBy"] != finished || got["lastPlayed"] == nil || got["finishedAt"] == nil {
			t.Fatalf("%s: %+v", f.App, got)
		}
	}
	check(bo.Radarr, 78, 2, 1)
	check(bo.Sonarr, 95396, 2, 1)

	// Nothing new: nothing said. Something new: said again.
	e.srv.reportWatched(ctx, true)
	if len(bo.Radarr.Watched) != 1 || len(bo.Sonarr.Watched) != 1 {
		t.Fatalf("reported again with nothing new: %d %d", len(bo.Radarr.Watched), len(bo.Sonarr.Watched))
	}
	e.db.SetProgress(ctx, admin.ID, e2, 0, true)
	e.srv.reportWatched(ctx, true)
	if len(bo.Radarr.Watched) != 1 || len(bo.Sonarr.Watched) != 2 {
		t.Fatalf("after a play: %d %d", len(bo.Radarr.Watched), len(bo.Sonarr.Watched))
	}
	if got := bo.Sonarr.Watched[1]["titles"].([]any)[0].(map[string]any); got["finishedBy"] != 2.0 {
		t.Fatalf("after a play: %+v", got)
	}
}

// Real Radarr and Sonarr, and a Box Office that doesn't take the report,
// are told nothing.
func TestWatchedNotReportedElsewhere(t *testing.T) {
	e := pairingSetup(t)
	ctx := context.Background()
	e.db.SetProgress(ctx, e.ellenID, e.movie, 0, true)
	radarr := arrtest.New(t, arr.Radarr)
	radarr.TakesWatched = true // it would take one, were it asked
	if code := e.admin.do("PUT", "/api/v1/integrations/radarr", map[string]any{"url": radarr.URL, "apiKey": radarr.Key, "qualityProfileId": 1, "rootFolder": "/media/movie"}, nil); code != 200 {
		t.Fatalf("connect radarr: %d", code)
	}
	e.srv.ReadBoxOffice(ctx)
	e.srv.reportWatched(ctx, true)
	if len(radarr.Watched) != 0 {
		t.Fatalf("Radarr was told: %+v", radarr.Watched)
	}
	bo := connectPairedBoxOffice(t, e)
	e.srv.reportWatched(ctx, true)
	if len(bo.Radarr.Watched) != 0 || len(bo.Sonarr.Watched) != 0 {
		t.Fatal("a Box Office that doesn't take the report was told")
	}
}
