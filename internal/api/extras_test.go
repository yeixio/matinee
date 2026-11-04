package api

import (
	"net/http"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/yeixio/matinee/internal/store"
)

func TestExtras(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	c, tmp := newTestServer(t)
	var login struct{ Token string }
	c.do("POST", "/api/setup", credentials{"admin", "correct horse"}, &login)
	c.token = login.Token

	h264 := []string{"-f", "lavfi", "-i", "testsrc=duration=2:size=160x120:rate=24", "-c:v", "libx264", "-preset", "ultrafast"}
	movie := filepath.Join(tmp, "movies", "Heat (1995)")
	makeMedia(t, filepath.Join(movie, "Heat (1995).mp4"), h264...)
	makeMedia(t, filepath.Join(movie, "Heat (1995)-featurette.mp4"), h264...)
	makeMedia(t, filepath.Join(movie, "Behind The Scenes", "Shooting the bank.mkv"), h264...)
	makeMedia(t, filepath.Join(movie, "Deleted Scenes", "Diner.mkv"), "-f", "lavfi", "-i", "testsrc=duration=2:size=160x120:rate=24", "-c:v", "mpeg4")
	makeMedia(t, filepath.Join(tmp, "tv", "Severance", "Season 1", "Severance S01E01.mkv"), h264...)
	makeMedia(t, filepath.Join(tmp, "tv", "Severance", "Interviews", "Adam Scott.mp4"), h264...)

	var movies, shows store.Library
	c.do("POST", "/api/libraries", map[string]any{"name": "Movies", "kind": "movies", "paths": []string{filepath.Join(tmp, "movies")}}, &movies)
	c.do("POST", "/api/libraries", map[string]any{"name": "TV", "kind": "tv", "paths": []string{filepath.Join(tmp, "tv")}}, &shows)
	waitForScan(t, c)

	var items []store.Item
	c.do("GET", "/api/libraries/"+itoa(movies.ID)+"/items", nil, &items)
	if len(items) != 1 {
		t.Fatalf("extras were listed as movies or versions: %+v", items)
	}
	id := itoa(items[0].ID)

	var extras []extraView
	c.do("GET", "/api/items/"+id+"/extras", nil, &extras)
	if len(extras) != 3 {
		t.Fatalf("extras: %+v", extras)
	}
	bts, deleted, feat := extras[0], extras[1], extras[2]
	if bts.Kind != "behindTheScenes" || bts.Name != "Shooting the bank" || bts.VideoCodec != "h264" || bts.DurationMs == 0 {
		t.Errorf("behind the scenes: %+v", bts)
	}
	if deleted.Kind != "deleted" || deleted.Name != "Diner" || deleted.VideoCodec != "mpeg4" {
		t.Errorf("deleted scene: %+v", deleted)
	}
	// Named after the movie, so it's called by its kind.
	if feat.Kind != "featurette" || feat.Name != "" || feat.MimeType != "video/mp4" {
		t.Errorf("featurette: %+v", feat)
	}

	req, _ := http.NewRequest("GET", c.base+"/api/items/"+id+"/extras/"+feat.ID+"/file", nil)
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Range", "bytes=0-3")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusPartialContent {
		t.Errorf("extra file: %d", res.StatusCode)
	}
	var hls struct{ URL string }
	if code := c.do("POST", "/api/items/"+id+"/extras/"+deleted.ID+"/hls", nil, &hls); code != 200 || hls.URL == "" {
		t.Errorf("extra transcode: %d %+v", code, hls)
	}
	req, _ = http.NewRequest("GET", c.base+"/api/items/"+id+"/extras/"+bts.ID+"/image", nil)
	req.Header.Set("Authorization", "Bearer "+c.token)
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 200 || res.Header.Get("Content-Type") != "image/jpeg" {
		t.Errorf("extra still: %d %s", res.StatusCode, res.Header.Get("Content-Type"))
	}
	if code := c.do("GET", "/api/items/"+id+"/extras/nope/file", nil, nil); code != http.StatusNotFound {
		t.Errorf("missing extra: %d", code)
	}

	// A show's extras sit in its folder.
	c.do("GET", "/api/libraries/"+itoa(shows.ID)+"/items", nil, &items)
	c.do("GET", "/api/items/"+itoa(items[0].ID)+"/extras", nil, &extras)
	if len(extras) != 1 || extras[0].Kind != "interview" || extras[0].Name != "Adam Scott" {
		t.Errorf("show extras: %+v", extras)
	}
}
