package api

import (
	"context"
	"net/http"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/yeixio/matinee/internal/analyze"
	"github.com/yeixio/matinee/internal/store"
	"github.com/yeixio/matinee/internal/thumbs"
)

// A player asking for a video's pictures has them made, then gets their
// layout and the sheets; turned off, there are none.
func TestThumbnailsOnPlay(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	c, tmp := newTestServer(t)
	var login struct{ Token string }
	if code := c.do("POST", "/api/v1/setup", credentials{"admin", "correct horse"}, &login); code != 200 {
		t.Fatalf("setup: %d", code)
	}
	c.token = login.Token
	g := &thumbs.Generator{Store: c.srv.Store, FFmpeg: "ffmpeg", Dir: filepath.Join(tmp, "thumbnails"), Log: c.srv.Log}
	c.srv.Thumbs, c.srv.Analyzer.Thumbs = g, g
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() { g.Serve(ctx); close(stopped) }()
	t.Cleanup(func() { cancel(); <-stopped })

	makeMedia(t, filepath.Join(tmp, "movies", "Film (2020).mkv"), "-f", "lavfi", "-i", "testsrc2=s=320x180:r=5:d=30",
		"-c:v", "libx264", "-preset", "ultrafast", "-g", "5")
	if code := c.do("POST", "/api/v1/libraries", map[string]any{"name": "Movies", "kind": "movies", "paths": []string{filepath.Join(tmp, "movies")}}, nil); code != http.StatusCreated {
		t.Fatalf("create library: %d", code)
	}
	waitForScan(t, c)
	var st analyze.Status
	if code := c.do("PUT", "/api/v1/analysis", map[string]string{"thumbnails": "onPlay"}, &st); code != 200 || st.Thumbnails == nil || st.Thumbnails.Mode != thumbs.OnPlay || st.Thumbnails.Total != 1 {
		t.Fatalf("status: %d %+v", code, st.Thumbnails)
	}
	if code := c.do("PUT", "/api/v1/analysis", map[string]string{"thumbnails": "sometimes"}, nil); code != http.StatusBadRequest {
		t.Fatalf("bad mode: %d", code)
	}
	var libs []libraryView
	c.do("GET", "/api/v1/libraries", nil, &libs)
	var top []store.Item
	c.do("GET", "/api/v1/libraries/"+itoa(libs[0].ID)+"/items", nil, &top)
	if len(top) != 1 {
		t.Fatalf("items: %+v", top)
	}
	id := itoa(top[0].ID)

	var v thumbnailsView
	if c.do("GET", "/api/v1/items/"+id+"/thumbnails", nil, &v); v.State != "making" {
		t.Fatalf("first ask: %+v", v)
	}
	deadline := time.Now().Add(30 * time.Second)
	for v.State != "ready" {
		if time.Now().After(deadline) {
			t.Fatalf("not made: %+v", v)
		}
		time.Sleep(100 * time.Millisecond)
		c.do("GET", "/api/v1/items/"+id+"/thumbnails", nil, &v)
	}
	if v.IntervalMs != 2000 || v.Count != 15 || v.Width != 240 || v.Height != 134 || v.Sheets != 1 || v.Tag == "" {
		t.Fatalf("layout: %+v", v)
	}
	res, err := http.Get(c.base + "/api/v1/items/" + id + "/thumbnails/0?v=" + v.Tag + "&token=" + c.token)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 200 || res.Header.Get("Content-Type") != "image/jpeg" {
		t.Fatalf("sheet: %d %s", res.StatusCode, res.Header.Get("Content-Type"))
	}
	if code := c.do("GET", "/api/v1/items/"+id+"/thumbnails/1", nil, nil); code != http.StatusNotFound {
		t.Fatalf("sheet past the end: %d", code)
	}

	c.do("PUT", "/api/v1/analysis", map[string]string{"thumbnails": "off"}, nil)
	v = thumbnailsView{}
	if c.do("GET", "/api/v1/items/"+id+"/thumbnails", nil, &v); v.State != "off" || v.Count != 0 {
		t.Fatalf("off: %+v", v)
	}
}
