package api

import (
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yeixio/matinee/internal/store"
	"github.com/yeixio/matinee/internal/stream"
)

func TestHLSSteps(t *testing.T) {
	c, _ := newTestServer(t)
	s := c.srv
	home := httptest.NewRequest("POST", "/", nil)
	home.RemoteAddr = "192.168.1.20:5000"
	away := httptest.NewRequest("POST", "/", nil)
	away.RemoteAddr = "203.0.113.5:5000"
	video := store.Item{VideoCodec: "hevc"}

	check := func(name string, r *http.Request, it store.Item, ask, wantSteps, wantQuality string) {
		t.Helper()
		steps, q, err := s.hlsSteps(r, it, ask)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got := strings.Join(stepIDs(steps), ","); got != wantSteps || q != wantQuality {
			t.Errorf("%s: got %q %s, want %q %s", name, got, q, wantSteps, wantQuality)
		}
	}
	set := func(key, v string) {
		if err := s.Store.SetSetting(t.Context(), key, v); err != nil {
			t.Fatal(err)
		}
	}

	// Off by default: everyone gets the original, whatever they ask for.
	check("off", home, video, "auto", "", "original")
	check("off, a step", home, video, "480p", "", "original")

	set(settingAdaptiveQuality, "true")
	check("auto", home, video, "auto", "1080p,720p,480p,360p", "auto")
	check("a step", home, video, "480p", "480p", "480p")
	check("original", home, video, "original", "", "original")
	check("nothing asked", home, video, "", "", "original")
	check("music", home, store.Item{}, "auto", "", "original")
	set(settingQualitySteps, "720p,360p")
	check("admin's steps", home, video, "auto", "720p,360p", "auto")
	if _, _, err := s.hlsSteps(home, video, "4k"); err != errBadQuality {
		t.Errorf("unknown step: %v", err)
	}

	// The cap only applies away from home.
	set(settingRemoteQualityCap, "480p")
	check("capped, at home", home, video, "auto", "720p,360p", "auto")
	check("capped auto", away, video, "auto", "360p", "auto")
	check("capped step", away, video, "720p", "480p", "480p")
	check("capped original", away, video, "original", "480p", "original")
	set(settingQualitySteps, "")
	check("capped auto, every step", away, video, "auto", "480p,360p", "auto")

	// And with adaptive quality off.
	set(settingAdaptiveQuality, "false")
	check("capped while off", away, video, "auto", "480p", "original")
	check("uncapped while off", home, video, "auto", "", "original")
}

func TestAdaptiveStreaming(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	c, tmp := newTestServer(t)
	var login struct{ Token string }
	c.do("POST", "/api/setup", credentials{"admin", "correct horse"}, &login)
	c.token = login.Token
	makeMedia(t, filepath.Join(tmp, "movies", "Wide (2020)", "Wide (2020).mkv"),
		"-f", "lavfi", "-i", "testsrc=duration=8:size=1280x720:rate=24", "-f", "lavfi", "-i", "sine=duration=8",
		"-c:v", "mpeg4", "-c:a", "mp2", "-shortest")
	body := map[string]any{"name": "Movies", "kind": "movies", "paths": []string{filepath.Join(tmp, "movies")}}
	if code := c.do("POST", "/api/libraries", body, nil); code != http.StatusCreated {
		t.Fatalf("create library: %d", code)
	}
	waitForScan(t, c)
	var items []struct{ ID int64 }
	c.do("GET", "/api/libraries/1/items", nil, &items)
	if len(items) != 1 {
		t.Fatalf("items: %v", items)
	}
	id := itoa(items[0].ID)

	var info streamingView
	c.do("GET", "/api/streaming", nil, &info)
	if info.Adaptive || len(info.Steps) != 0 || info.Cap != nil {
		t.Fatalf("off by default: %+v", info)
	}

	var set serverSettings
	if code := c.do("PUT", "/api/settings", map[string]any{"adaptiveQuality": true, "qualitySteps": []string{"720p", "480p"}, "remoteQualityCap": "720p"}, &set); code != 200 {
		t.Fatalf("settings: %d", code)
	}
	if !set.AdaptiveQuality || strings.Join(set.QualitySteps, ",") != "720p,480p" || set.RemoteQualityCap != "720p" {
		t.Fatalf("settings: %+v", set)
	}
	if code := c.do("PUT", "/api/settings", map[string]any{"qualitySteps": []string{"8k"}}, nil); code != http.StatusBadRequest {
		t.Fatalf("unknown step: %d", code)
	}
	if code := c.do("PUT", "/api/settings", map[string]any{"qualitySteps": []string{}}, nil); code != http.StatusBadRequest {
		t.Fatalf("no steps: %d", code)
	}
	c.do("GET", "/api/streaming", nil, &info)
	// The test server is on this machine, which is home: no cap.
	if !info.Adaptive || len(info.Steps) != 2 || info.Steps[0] != stream.Steps[1] || info.Cap != nil {
		t.Fatalf("streaming: %+v", info)
	}

	var hls struct{ URL, Quality string }
	if code := c.do("POST", "/api/items/"+id+"/hls?quality=auto", nil, &hls); code != 200 {
		t.Fatalf("start auto: %d", code)
	}
	if !strings.HasSuffix(hls.URL, "/master.m3u8") || hls.Quality != "auto" {
		t.Fatalf("auto: %+v", hls)
	}
	master := get(t, c, hls.URL)
	if !strings.Contains(master, "RESOLUTION=1280x720") || !strings.Contains(master, "RESOLUTION=854x480") {
		t.Fatalf("master:\n%s", master)
	}
	if pl := get(t, c, strings.Replace(hls.URL, "master.m3u8", "index_1.m3u8", 1)); !strings.Contains(pl, "seg_1_00000.ts") {
		t.Fatalf("variant:\n%s", pl)
	}

	if code := c.do("POST", "/api/items/"+id+"/hls?quality=480p", nil, &hls); code != 200 || !strings.HasSuffix(hls.URL, "/index.m3u8") || hls.Quality != "480p" {
		t.Fatalf("480p: %d %+v", code, hls)
	}
	if code := c.do("POST", "/api/items/"+id+"/hls?quality=nope", nil, nil); code != http.StatusBadRequest {
		t.Fatalf("bad quality: %d", code)
	}
}
