package api

import (
	"context"
	"testing"

	"github.com/yeixio/matinee/internal/probe"
	"github.com/yeixio/matinee/internal/store"
)

func TestItemChapters(t *testing.T) {
	c, _, db := newTestServerStore(t)
	ctx := context.Background()
	var login struct{ Token string }
	c.do("POST", "/api/v1/setup", credentials{"admin", "correct horse"}, &login)
	c.token = login.Token

	lib, _ := db.CreateLibrary(ctx, "Movies", "movies", []string{"/m"})
	movie := func(title, info string) string {
		dur := int64(600_000)
		id, err := db.UpsertFile(ctx, store.Item{LibraryID: lib.ID, Kind: "movie", Title: title, VideoCodec: "h264",
			DurationMs: &dur, Path: "/m/" + title + ".mkv"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.SetMediaInfo(ctx, id, 0, 0, info, nil, store.MediaKey(probe.MediaInfoVersion, 0, 0)); err != nil {
			t.Fatal(err)
		}
		return itoa(id)
	}
	type view struct {
		Chapters  []probe.Chapter
		MediaInfo *probe.MediaInfo
	}
	var v view
	id := movie("Chaptered", `{"durationMs":600000,"chapters":3,"chapterList":[
		{"title":"Opening","startMs":0,"endMs":120000},{"title":"Heist","startMs":120000,"endMs":480000},{"startMs":480000,"endMs":600000}]}`)
	c.do("GET", "/api/v1/items/"+id, nil, &v)
	if len(v.Chapters) != 3 || v.Chapters[1].Title != "Heist" || v.Chapters[1].StartMs != 120000 || v.Chapters[2].Title != "" {
		t.Fatalf("chapters: %+v", v.Chapters)
	}
	if v.MediaInfo == nil || v.MediaInfo.ChapterList != nil || v.MediaInfo.Chapters != 3 {
		t.Fatalf("media info keeps the count, not the list: %+v", v.MediaInfo)
	}

	// One chapter covering the whole file marks nothing.
	v = view{}
	id = movie("Single", `{"durationMs":600000,"chapters":1,"chapterList":[{"startMs":0,"endMs":600000}]}`)
	c.do("GET", "/api/v1/items/"+id, nil, &v)
	if v.Chapters != nil {
		t.Fatalf("one chapter: %+v", v.Chapters)
	}
}
