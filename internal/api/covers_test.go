package api

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yeixio/matinee/internal/metadata"
	"github.com/yeixio/matinee/internal/store"
)

type fakeCovers struct{ got metadata.Query }

func (f *fakeCovers) Find(ctx context.Context, q metadata.Query) []metadata.CoverOption {
	f.got = q
	return []metadata.CoverOption{{URL: "https://covers.example/1.jpg", Thumb: "https://covers.example/1-250.jpg", Source: "Apple", Title: "Fluoro"}}
}

func testPNG(c color.Color) []byte {
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for x := range 8 {
		for y := range 8 {
			img.Set(x, y, c)
		}
	}
	var b bytes.Buffer
	png.Encode(&b, img)
	return b.Bytes()
}

// An admin picks an album's cover from the options found online, from a
// web address, or by uploading one; it wins over the folder's until reset.
func TestPickCover(t *testing.T) {
	finder := &fakeCovers{}
	c, _, db := newTestServerStore(t, func(s *Server) { s.Covers = finder })
	var login struct{ Token string }
	c.do("POST", "/api/setup", credentials{"admin", "correct horse"}, &login)
	c.token = login.Token
	ctx := context.Background()
	lib, _ := db.CreateLibrary(ctx, "Music", "music", []string{"/nowhere"})
	artist, _ := db.EnsureContainer(ctx, lib.ID, nil, "artist", "Paul Oakenfold", nil, nil)
	album, _ := db.EnsureContainer(ctx, lib.ID, &artist, "album", "Fluro - Disk 1", nil, nil)
	db.SetAlbumTags(ctx, album, "6a1e1a70-8c62-4f4f-9e8c-3b9c1b4b2f10", "")

	var opts struct {
		Options []metadata.CoverOption
		Chosen  bool
	}
	if code := c.do("GET", "/api/v1/items/"+itoa(album)+"/covers", nil, &opts); code != 200 || len(opts.Options) != 1 || opts.Chosen {
		t.Fatalf("options: %d %+v", code, opts)
	}
	if q := finder.got; q.Kind != "album" || q.Title != "Fluro - Disk 1" || q.Artist != "Paul Oakenfold" || q.ReleaseID != "6a1e1a70-8c62-4f4f-9e8c-3b9c1b4b2f10" {
		t.Fatalf("query: %+v", q)
	}
	if code := c.do("GET", "/api/v1/items/"+itoa(artist)+"/covers", nil, nil); code != http.StatusBadRequest {
		t.Fatalf("an artist has no cover to pick: %d", code)
	}

	put := func(body []byte, ct string) (int, store.Item) {
		req, _ := http.NewRequest("PUT", c.base+"/api/v1/items/"+itoa(album)+"/cover", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+c.token)
		req.Header.Set("Content-Type", ct)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var it store.Item
		if res.StatusCode == 200 {
			json.NewDecoder(res.Body).Decode(&it)
		}
		return res.StatusCode, it
	}
	image := func(path string) []byte {
		req, _ := http.NewRequest("GET", c.base+path, nil)
		req.Header.Set("Authorization", "Bearer "+c.token)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return b
	}

	red := testPNG(color.RGBA{255, 0, 0, 255})
	code, it := put(red, "image/png")
	if code != 200 || it.PosterTag == "" || !it.HasPoster {
		t.Fatalf("upload: %d %+v", code, it)
	}
	if got := image("/api/v1/items/" + itoa(album) + "/image/poster?v=" + it.PosterTag); !bytes.Equal(got, red) {
		t.Fatalf("served %d bytes, not the upload", len(got))
	}
	if code, _ := put([]byte("not an image"), "image/png"); code != http.StatusBadRequest {
		t.Fatalf("not an image: %d", code)
	}

	// A cover from a web address; the folder's cover found later doesn't replace it.
	blue := testPNG(color.RGBA{0, 0, 255, 255})
	img := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(blue) }))
	defer img.Close()
	code, it2 := put([]byte(`{"url":"`+img.URL+`/cover.png"}`), "application/json")
	if code != 200 || it2.PosterTag == it.PosterTag {
		t.Fatalf("from a URL: %d %+v", code, it2)
	}
	if code, _ := put([]byte(`{"url":"file:///etc/passwd"}`), "application/json"); code != http.StatusBadRequest {
		t.Fatalf("a file URL: %d", code)
	}
	db.SetArt(ctx, album, "/nowhere/cover.jpg", "")
	db.ApplyMetadata(ctx, album, &store.Metadata{Poster: "https://covers.example/online.jpg"})
	if got := image("/api/v1/items/" + itoa(album) + "/image/poster?v=" + it2.PosterTag); !bytes.Equal(got, blue) {
		t.Fatalf("served %d bytes, not the picked cover", len(got))
	}
	c.do("GET", "/api/v1/items/"+itoa(album)+"/covers", nil, &opts)
	if !opts.Chosen {
		t.Fatalf("chosen: %+v", opts)
	}

	if code := c.do("DELETE", "/api/v1/items/"+itoa(album)+"/cover", nil, nil); code != http.StatusNoContent {
		t.Fatalf("reset: %d", code)
	}
	waitForScan(t, c)
	if a, _ := db.Item(ctx, 0, album); a.PosterTag != "" || strings.HasPrefix(a.Poster, "chosen:") || a.MetaAt != 0 {
		t.Fatalf("after reset: %+v", a)
	}
	if _, err := db.ChosenImage(ctx, album); err != store.ErrNotFound {
		t.Fatalf("the picked image should be gone: %v", err)
	}
}
