package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/yeixio/matinee/internal/store/storetest"
)

// Albums still without a cover are looked up again when their files turn
// out to be tagged with an album's MusicBrainz id, and a month after their
// last lookup otherwise. Albums with a cover are left be.
func TestAlbumArtRetries(t *testing.T) {
	st := storetest.Open(t)
	ctx := context.Background()
	lib, _ := st.CreateLibrary(ctx, "Music", "music", []string{"/music"})
	artist, _ := st.EnsureContainer(ctx, lib.ID, nil, "artist", "Paul Oakenfold", nil, nil)
	bare, _ := st.EnsureContainer(ctx, lib.ID, &artist, "album", "Fluro - Disk 1", nil, nil)
	covered, _ := st.EnsureContainer(ctx, lib.ID, &artist, "album", "Tranceport", nil, nil)
	st.SetArt(ctx, covered, "/music/Tranceport/cover.jpg", "")
	kinds := []string{"artist", "album"}
	pending := func() map[int64]bool {
		items, err := st.NeedsMetadata(ctx, lib.ID, kinds)
		if err != nil {
			t.Fatal(err)
		}
		out := map[int64]bool{}
		for _, it := range items {
			out[it.ID] = true
		}
		return out
	}
	for _, id := range []int64{artist, bare, covered} {
		st.ApplyMetadata(ctx, id, nil)
	}
	if p := pending(); len(p) != 0 {
		t.Fatalf("just looked up: %v", p)
	}

	const id = "6a1e1a70-8c62-4f4f-9e8c-3b9c1b4b2f10"
	for _, a := range []int64{bare, covered} {
		if err := st.SetAlbumTags(ctx, a, id, ""); err != nil {
			t.Fatal(err)
		}
	}
	if p := pending(); !p[bare] || p[covered] || len(p) != 1 {
		t.Fatalf("after new tags: %v", p)
	}
	if r, g, _ := st.AlbumTags(ctx, bare); r != id || g != "" {
		t.Fatalf("tags: %q %q", r, g)
	}
	st.ApplyMetadata(ctx, bare, nil)
	st.SetAlbumTags(ctx, bare, id, "") // the same tags again
	if p := pending(); len(p) != 0 {
		t.Fatalf("same tags: %v", p)
	}

	old := time.Now().Add(-31 * 24 * time.Hour).Unix()
	for _, id := range []int64{artist, bare, covered} {
		st.SetMetaAt(ctx, id, old)
	}
	if p := pending(); !p[bare] || len(p) != 1 {
		t.Fatalf("a month on: %v", p)
	}
}
