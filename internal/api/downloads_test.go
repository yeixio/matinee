package api

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/yeixio/matinee/internal/store"
)

// fetch makes a raw request as a client, for checking file transfers.
func (c *client) fetch(path string, header map[string]string) (*http.Response, []byte) {
	c.t.Helper()
	req, _ := http.NewRequest("GET", c.base+path, nil)
	req.Header.Set("Authorization", "Bearer "+c.token)
	for k, v := range header {
		req.Header.Set(k, v)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res, b
}

func waitForDownload(t *testing.T, c *client, id string) downloadView {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	var d downloadView
	for time.Now().Before(deadline) {
		if code := c.do("GET", "/api/v1/downloads/"+id, nil, &d); code != 200 {
			t.Fatalf("get download: %d", code)
		}
		switch d.State {
		case store.DownloadReady:
			return d
		case store.DownloadFailed:
			t.Fatalf("download failed: %s", d.Error)
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("download never got ready: %+v", d)
	return d
}

func signIn(t *testing.T, c *client, user, password, device string) *client {
	t.Helper()
	var login struct{ Token string }
	body := map[string]string{"username": user, "password": password, "deviceName": device, "platform": "ios"}
	if code := c.do("POST", "/api/v1/auth/login", body, &login); code != 200 {
		t.Fatalf("sign in %s: %d", device, code)
	}
	return &client{t: t, base: c.base, token: login.Token}
}

func sha(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func TestDownloads(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	admin, tmp := newTestServer(t)
	var login struct{ Token string }
	if code := admin.do("POST", "/api/v1/setup", credentials{"admin", "correct horse"}, &login); code != 200 {
		t.Fatalf("setup: %d", code)
	}
	admin.token = login.Token

	moviePath := filepath.Join(tmp, "movies", "Downloadable (2020)", "Downloadable (2020).mkv")
	makeMedia(t, moviePath, "-f", "lavfi", "-i", "testsrc=duration=4:size=640x480:rate=24", "-f", "lavfi", "-i", "sine=duration=4",
		"-c:v", "mpeg4", "-c:a", "mp3", "-shortest")
	if code := admin.do("POST", "/api/v1/libraries", map[string]any{"name": "Movies", "kind": "movies", "paths": []string{filepath.Join(tmp, "movies")}}, nil); code != http.StatusCreated {
		t.Fatalf("library: %d", code)
	}
	waitForScan(t, admin)
	var found []store.Item
	admin.do("GET", "/api/v1/search?q=downloadable", nil, &found)
	if len(found) != 1 {
		t.Fatalf("search: %+v", found)
	}
	movie := found[0]

	phone := signIn(t, admin, "admin", "correct horse", "Phone")
	tablet := signIn(t, admin, "admin", "correct horse", "Tablet")

	if code := phone.do("POST", "/api/v1/downloads", map[string]any{"itemId": movie.ID, "quality": "4k"}, nil); code != http.StatusBadRequest {
		t.Fatalf("unknown quality: %d", code)
	}

	// The original, as is.
	var orig downloadView
	if code := phone.do("POST", "/api/v1/downloads", map[string]any{"itemId": movie.ID, "quality": "original"}, &orig); code != 200 {
		t.Fatalf("create: %d", code)
	}
	orig = waitForDownload(t, phone, orig.ID)
	want, _ := os.ReadFile(moviePath)
	if orig.SHA256 != sha(want) || orig.Size != int64(len(want)) || orig.MimeType != "video/x-matroska" {
		t.Fatalf("original: %+v", orig)
	}

	// Asking again is safe: same download, nothing redone.
	var again downloadView
	phone.do("POST", "/api/v1/downloads", map[string]any{"itemId": movie.ID, "quality": "original"}, &again)
	if again.ID != orig.ID || again.State != store.DownloadReady {
		t.Fatalf("asking again: %+v", again)
	}

	// Whole file, with the checksum headers.
	res, body := phone.fetch(orig.URL, nil)
	if res.StatusCode != 200 || sha(body) != orig.SHA256 || res.Header.Get("ETag") != `"`+orig.SHA256+`"` || res.Header.Get("Repr-Digest") == "" {
		t.Fatalf("file: %d %v", res.StatusCode, res.Header)
	}
	// Resuming: the rest of the file, if it's still the same file.
	res, part := phone.fetch(orig.URL, map[string]string{"Range": "bytes=100-", "If-Range": `"` + orig.SHA256 + `"`})
	if res.StatusCode != http.StatusPartialContent || string(part) != string(want[100:]) {
		t.Fatalf("resume: %d, %d bytes", res.StatusCode, len(part))
	}
	// A resume against a different file gets the whole new file instead of a mix.
	res, _ = phone.fetch(orig.URL, map[string]string{"Range": "bytes=100-", "If-Range": `"stale"`})
	if res.StatusCode != 200 {
		t.Fatalf("stale resume: %d", res.StatusCode)
	}
	// Downloads belong to the device that asked.
	if res, _ := tablet.fetch(orig.URL, nil); res.StatusCode != http.StatusForbidden {
		t.Fatalf("other device: %d", res.StatusCode)
	}

	// A conversion, on the tablet: 480p MP4.
	var conv downloadView
	if code := tablet.do("POST", "/api/v1/downloads", map[string]any{"itemId": movie.ID, "quality": "480p"}, &conv); code != 200 {
		t.Fatalf("create 480p: %d", code)
	}
	conv = waitForDownload(t, tablet, conv.ID)
	res, body = tablet.fetch(conv.URL, nil)
	if res.StatusCode != 200 || sha(body) != conv.SHA256 || conv.MimeType != "video/mp4" || res.Header.Get("Content-Type") != "video/mp4" {
		t.Fatalf("converted: %d %+v", res.StatusCode, conv)
	}
	out := filepath.Join(tmp, "got.mp4")
	os.WriteFile(out, body, 0o644)
	probe, err := exec.Command("ffprobe", "-v", "error", "-select_streams", "v:0", "-show_entries", "stream=codec_name,height", "-of", "csv=p=0", out).Output()
	if err != nil || string(probe) != "h264,480\n" {
		t.Fatalf("converted file: %q %v", probe, err)
	}

	// The device lists only its own; ?device=all lists every device.
	var mine, all []downloadView
	tablet.do("GET", "/api/v1/downloads", nil, &mine)
	tablet.do("GET", "/api/v1/downloads?device=all", nil, &all)
	if len(mine) != 1 || mine[0].ID != conv.ID || len(all) != 2 {
		t.Fatalf("lists: mine %d, all %d", len(mine), len(all))
	}

	// The device has it; then it's removed and its converted file goes.
	if code := tablet.do("POST", "/api/v1/downloads/"+conv.ID+"/finished", nil, nil); code != http.StatusNoContent {
		t.Fatalf("finished: %d", code)
	}
	var files []string
	entries, _ := os.ReadDir(filepath.Join(tmp, "downloads"))
	for _, e := range entries {
		files = append(files, e.Name())
	}
	if len(files) != 1 {
		t.Fatalf("download cache: %v", files)
	}
	if code := tablet.do("DELETE", "/api/v1/downloads/"+conv.ID, nil, nil); code != http.StatusNoContent {
		t.Fatalf("delete: %d", code)
	}
	if entries, _ := os.ReadDir(filepath.Join(tmp, "downloads")); len(entries) != 0 {
		t.Fatalf("converted file left behind: %v", entries)
	}

	// A changed original is prepared again rather than served with a stale checksum.
	future := time.Now().Add(time.Hour)
	os.Chtimes(moviePath, future, future)
	if res, _ := phone.fetch(orig.URL, nil); res.StatusCode != http.StatusConflict {
		t.Fatalf("changed file: %d", res.StatusCode)
	}
	orig = waitForDownload(t, phone, orig.ID)
	if res, _ := phone.fetch(orig.URL, nil); res.StatusCode != 200 {
		t.Fatalf("after preparing again: %d", res.StatusCode)
	}

	// Signing a device out takes its downloads with it.
	var devices []store.Session
	phone.do("GET", "/api/v1/devices", nil, &devices)
	for _, d := range devices {
		if d.Current {
			admin.do("DELETE", "/api/v1/devices/"+d.ID, nil, nil)
		}
	}
	admin.do("GET", "/api/v1/downloads?device=all", nil, &all)
	if len(all) != 0 {
		t.Fatalf("downloads after sign-out: %+v", all)
	}
}

func TestSyncProgress(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	c, tmp := newTestServer(t)
	var login struct{ Token string }
	c.do("POST", "/api/v1/setup", credentials{"admin", "correct horse"}, &login)
	c.token = login.Token
	makeMedia(t, filepath.Join(tmp, "movies", "Offline (2020)", "Offline (2020).mp4"),
		"-f", "lavfi", "-i", "testsrc=duration=100:size=64x48:rate=1", "-c:v", "libx264", "-preset", "ultrafast")
	c.do("POST", "/api/v1/libraries", map[string]any{"name": "Movies", "kind": "movies", "paths": []string{filepath.Join(tmp, "movies")}}, nil)
	waitForScan(t, c)
	var found []store.Item
	c.do("GET", "/api/v1/search?q=offline", nil, &found)
	if len(found) != 1 {
		t.Fatalf("search: %+v", found)
	}
	id := found[0].ID
	position := func() int64 {
		var it store.Item
		c.do("GET", "/api/v1/items/"+itoa(id), nil, &it)
		return it.PositionMs
	}

	// Watched online just now.
	c.do("PUT", "/api/v1/items/"+itoa(id)+"/progress", map[string]any{"positionMs": 50_000}, nil)

	// The phone was offline an hour ago; its older events don't undo that.
	hourAgo := time.Now().Add(-time.Hour).Unix()
	var res struct{ Applied, Skipped int }
	events := []store.ProgressEvent{{ItemID: id, PositionMs: 20_000, At: hourAgo + 60}, {ItemID: id, PositionMs: 10_000, At: hourAgo}, {ItemID: 999999, PositionMs: 1, At: hourAgo}}
	if code := c.do("POST", "/api/v1/sync/progress", map[string]any{"events": events}, &res); code != 200 || res.Applied != 0 || res.Skipped != 3 {
		t.Fatalf("old events: %d %+v", code, res)
	}
	if p := position(); p != 50_000 {
		t.Fatalf("older offline progress overwrote newer: %d", p)
	}

	// A newer event wins. A clock running ahead counts as now.
	time.Sleep(1100 * time.Millisecond)
	events = []store.ProgressEvent{{ItemID: id, PositionMs: 70_000, At: time.Now().Add(24 * time.Hour).Unix()}}
	if code := c.do("POST", "/api/v1/sync/progress", map[string]any{"events": events}, &res); code != 200 || res.Applied != 1 {
		t.Fatalf("new event: %d %+v", code, res)
	}
	if p := position(); p != 70_000 {
		t.Fatalf("position %d, want 70000", p)
	}
	c.do("PUT", "/api/v1/items/"+itoa(id)+"/progress", map[string]any{"positionMs": 80_000}, nil)
	if p := position(); p != 80_000 {
		t.Fatalf("watching online after a future-dated sync: %d", p)
	}
}
