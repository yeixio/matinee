package store_test

import (
	"context"
	"testing"

	"github.com/yeixio/matinee/internal/store"
	"github.com/yeixio/matinee/internal/store/storetest"
)

// Albums keep the record label their files name, one spelling per label;
// the library lists its labels, and a label filter picks its albums and
// the artists with albums on it.
func TestLabels(t *testing.T) {
	db := storetest.Open(t)
	ctx := context.Background()
	lib, _ := db.CreateLibrary(ctx, "Music", "music", []string{"/music"})
	album := func(artist, title string) int64 {
		ar, _ := db.EnsureContainer(ctx, lib.ID, nil, "artist", artist, nil, nil)
		id, err := db.EnsureContainer(ctx, lib.ID, &ar, "album", title, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.UpsertFile(ctx, store.Item{LibraryID: lib.ID, ParentID: &id, Kind: "track", Title: "1", Path: "/music/" + artist + "/" + title + "/1.flac"}); err != nil {
			t.Fatal(err)
		}
		return id
	}
	deep := album("Lane 8", "Little by Little")
	deep2 := album("Yotto", "Hyperfall")
	beats := album("Above & Beyond", "Common Ground")
	none := album("Someone", "Demo")

	toRead, err := db.AlbumsToReadLabel(ctx, lib.ID)
	if err != nil || len(toRead) != 4 {
		t.Fatalf("albums to read: %+v %v", toRead, err)
	}
	for id, label := range map[int64]string{deep: "Anjunadeep", deep2: " ANJUNADEEP ", beats: "Anjunabeats", none: ""} {
		if err := db.SetAlbumLabel(ctx, id, label); err != nil {
			t.Fatal(err)
		}
	}
	if toRead, _ := db.AlbumsToReadLabel(ctx, lib.ID); len(toRead) != 0 {
		t.Fatalf("read albums are listed again: %+v", toRead)
	}
	// A file without a label leaves the album's label alone.
	db.SetAlbumLabel(ctx, beats, "")

	it, _ := db.Item(ctx, 0, deep2)
	if it.RecordLabel != "Anjunadeep" {
		t.Fatalf("second spelling: %q", it.RecordLabel)
	}
	tracks, _ := db.Tracks(ctx, 0, beats)
	if len(tracks) != 1 || tracks[0].RecordLabel != "Anjunabeats" {
		t.Fatalf("a song's label: %+v", tracks)
	}

	labels, err := db.Labels(ctx, 0, lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(labels) != 2 || labels[0].Name != "Anjunabeats" || labels[1].Name != "Anjunadeep" ||
		labels[1].Albums != 2 || labels[1].Artists != 2 || labels[0].Albums != 1 {
		t.Fatalf("labels: %+v", labels)
	}

	titles := func(kind string) []string {
		items, err := db.FilterItems(ctx, 0, store.ItemFilter{Library: []int64{lib.ID}, Kind: []string{kind}, Label: []string{"Anjunadeep"}})
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, it := range items {
			out = append(out, it.Title)
		}
		return out
	}
	if got := titles("album"); len(got) != 2 || got[0] != "Hyperfall" || got[1] != "Little by Little" {
		t.Fatalf("label's albums: %v", got)
	}
	if got := titles("track"); len(got) != 2 {
		t.Fatalf("label's songs: %v", got)
	}
	if got := titles("artist"); len(got) != 2 || got[0] != "Lane 8" || got[1] != "Yotto" {
		t.Fatalf("label's artists: %v", got)
	}
}
