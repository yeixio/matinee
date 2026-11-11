package api

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/yeixio/matinee/internal/probe"
	"github.com/yeixio/matinee/internal/slowfs"
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

// While a title's network share isn't answering, its page still loads
// straight away (without the subtitle files beside it), and playing it
// says at once that its files can't be read, rather than waiting on the
// share.
func TestItemWithShareDown(t *testing.T) {
	c, _, db := newTestServerStore(t)
	ctx := context.Background()
	var login struct{ Token string }
	c.do("POST", "/api/v1/setup", credentials{"admin", "correct horse"}, &login)
	c.token = login.Token
	lib, _ := db.CreateLibrary(ctx, "Movies", "movies", []string{"/stalled-share/movies"})
	id, err := db.UpsertFile(ctx, store.Item{LibraryID: lib.ID, Kind: "movie", Title: "Dune", VideoCodec: "h264",
		Path: "/stalled-share/movies/Dune (2021)/Dune.mkv"})
	if err != nil {
		t.Fatal(err)
	}
	old := slowfs.Wait
	slowfs.Wait = 50 * time.Millisecond
	t.Cleanup(func() { slowfs.Wait = old })
	release := make(chan struct{})
	defer close(release)
	slowfs.Do("/stalled-share/movies/x", func() (int, error) { <-release; return 0, nil })

	start := time.Now()
	var v struct{ Title string }
	if code := c.do("GET", "/api/v1/items/"+itoa(id), nil, &v); code != http.StatusOK || v.Title != "Dune" || time.Since(start) > 2*time.Second {
		t.Fatalf("item: %d %+v after %v", code, v, time.Since(start))
	}
	if code := c.do("GET", "/api/v1/items/"+itoa(id)+"/file", nil, nil); code != http.StatusServiceUnavailable {
		t.Fatalf("file: %d", code)
	}
	if code := c.do("POST", "/api/v1/items/"+itoa(id)+"/hls", nil, nil); code != http.StatusServiceUnavailable {
		t.Fatalf("hls: %d", code)
	}
}
