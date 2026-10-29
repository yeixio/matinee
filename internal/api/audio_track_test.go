package api

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/yeixio/matinee/internal/store"
)

// A transcode plays the audio stream the app picked: a movie whose first
// audio stream is silent and second is a tone.
func TestHLSAudioTrack(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	c, tmp := newTestServer(t)
	var login struct{ Token string }
	c.do("POST", "/api/setup", credentials{"admin", "correct horse"}, &login)
	c.token = login.Token

	dir := filepath.Join(tmp, "movies", "Two Tracks (2020)")
	makeMedia(t, filepath.Join(dir, "Two Tracks (2020).mkv"),
		"-f", "lavfi", "-i", "color=c=black:duration=8:size=320x240:rate=24",
		"-f", "lavfi", "-i", "anullsrc=r=48000:cl=stereo", "-f", "lavfi", "-i", "sine=frequency=440:duration=8",
		"-map", "0:v", "-map", "1:a", "-map", "2:a", "-t", "8", "-c:v", "libx264", "-preset", "ultrafast", "-c:a", "aac",
		"-metadata:s:a:0", "language=eng", "-metadata:s:a:1", "language=spa")

	var lib store.Library
	c.do("POST", "/api/libraries", map[string]any{"name": "Movies", "kind": "movies", "paths": []string{filepath.Join(tmp, "movies")}}, &lib)
	waitForScan(t, c)
	var items []store.Item
	c.do("GET", "/api/libraries/"+itoa(lib.ID)+"/items", nil, &items)
	if len(items) != 1 {
		t.Fatalf("items: %+v", items)
	}
	base := "/api/items/" + itoa(items[0].ID)

	loudness := func(query string) float64 {
		t.Helper()
		var hls struct {
			URL   string
			Audio *int
		}
		if code := c.do("POST", base+"/hls"+query, nil, &hls); code != 200 {
			t.Fatalf("start hls%s: %d", query, code)
		}
		if hls.Audio == nil {
			t.Fatalf("start hls%s: no audio in the answer", query)
		}
		seg := filepath.Join(t.TempDir(), "seg.ts")
		os.WriteFile(seg, []byte(get(t, c, strings.Replace(hls.URL, "index.m3u8", "seg00000.ts", 1))), 0o644)
		out, _ := exec.Command("ffmpeg", "-hide_banner", "-i", seg, "-af", "volumedetect", "-vn", "-f", "null", "-").CombinedOutput()
		m := regexp.MustCompile(`max_volume: (-?[\d.]+|-inf) dB`).FindSubmatch(out)
		if m == nil {
			t.Fatalf("no volume for hls%s:\n%s", query, out)
		}
		if string(m[1]) == "-inf" {
			return -1000
		}
		v, _ := strconv.ParseFloat(string(m[1]), 64)
		return v
	}
	if first := loudness(""); first > -60 {
		t.Fatalf("first audio stream should be the silent one: max %.1f dB", first)
	}
	if second := loudness("?audio=1"); second < -30 {
		t.Fatalf("audio=1 should play the tone: max %.1f dB", second)
	}
	for _, bad := range []string{"-1", "x"} {
		if code := c.do("POST", base+"/hls?audio="+bad, nil, nil); code != http.StatusBadRequest {
			t.Fatalf("audio=%s: %d", bad, code)
		}
	}
}
