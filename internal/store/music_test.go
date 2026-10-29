package store_test

import (
	"context"
	"testing"

	"github.com/yeixio/matinee/internal/store"
	"github.com/yeixio/matinee/internal/store/storetest"
)

// An artist's tracks play album by album, oldest first, each in disc and
// track order, and every track names its album and artist.
func TestTracks(t *testing.T) {
	db := storetest.Open(t)
	ctx := context.Background()
	lib, _ := db.CreateLibrary(ctx, "Music", "music", []string{"/music"})
	artist, _ := db.EnsureContainer(ctx, lib.ID, nil, "artist", "The Band", nil, nil)
	y1, y2 := 1999, 1994
	later, _ := db.EnsureContainer(ctx, lib.ID, &artist, "album", "Later", nil, &y1)
	first, _ := db.EnsureContainer(ctx, lib.ID, &artist, "album", "First", nil, &y2)
	track := func(album int64, title string, disc, n int) int64 {
		id, err := db.UpsertFile(ctx, store.Item{LibraryID: lib.ID, ParentID: &album, Kind: "track", Title: title,
			DiscNumber: &disc, IndexNumber: &n, Path: "/music/" + title + ".flac"})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	track(later, "L2", 1, 2)
	track(later, "L1", 1, 1)
	track(first, "F2-1", 2, 1)
	f1 := track(first, "F1-9", 1, 9)

	titles := func(id int64) []string {
		items, err := db.Tracks(ctx, 0, id)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, it := range items {
			out = append(out, it.Title)
		}
		return out
	}
	if got := titles(artist); len(got) != 4 || got[0] != "F1-9" || got[1] != "F2-1" || got[2] != "L1" || got[3] != "L2" {
		t.Fatalf("artist's tracks: %v", got)
	}
	if got := titles(later); len(got) != 2 || got[0] != "L1" {
		t.Fatalf("album's tracks: %v", got)
	}
	if got := titles(f1); len(got) != 2 || got[0] != "F1-9" {
		t.Fatalf("a track's album: %v", got)
	}

	it, _ := db.Item(ctx, 0, f1)
	if it.Album != "First" || it.Artist != "The Band" || it.ArtistID == nil || *it.ArtistID != artist {
		t.Fatalf("track names: %+v", it)
	}
}

// Recently played albums: started lists what's been listened to, most
// recent first.
func TestStartedAlbums(t *testing.T) {
	db := storetest.Open(t)
	ctx := context.Background()
	user, _ := db.CreateUser(ctx, "mike", "hash", true)
	lib, _ := db.CreateLibrary(ctx, "Music", "music", []string{"/music"})
	artist, _ := db.EnsureContainer(ctx, lib.ID, nil, "artist", "The Band", nil, nil)
	var tracks []int64
	for _, title := range []string{"A", "B", "C"} {
		album, _ := db.EnsureContainer(ctx, lib.ID, &artist, "album", title, nil, nil)
		id, _ := db.UpsertFile(ctx, store.Item{LibraryID: lib.ID, ParentID: &album, Kind: "track", Title: title + "1", Path: "/music/" + title + ".flac"})
		tracks = append(tracks, id)
	}
	db.SetProgress(ctx, user.ID, tracks[0], 0, true)
	db.SetProgress(ctx, user.ID, tracks[2], 1000, false)
	f := store.ItemFilter{Library: []int64{lib.ID}, Kind: []string{"album"}, Watched: "started", Sort: "lastPlayed"}
	items, err := db.FilterItems(ctx, user.ID, f)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("started albums: %+v", items)
	}
}
