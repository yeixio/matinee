package api

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/yeixio/matinee/internal/analyze"
	"github.com/yeixio/matinee/internal/store"
)

// The background analysis fills in media info and tags, shows collect their
// episodes' tags, lists filter on them, and a changed file is analyzed again.
func TestMediaAnalysis(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	c, tmp := newTestServer(t)
	var login struct{ Token string }
	if code := c.do("POST", "/api/v1/setup", credentials{"admin", "correct horse"}, &login); code != 200 {
		t.Fatalf("setup: %d", code)
	}
	c.token = login.Token
	c.srv.Scanner.AfterScan = func(context.Context, store.Library) { c.srv.Analyzer.Wake() }

	ep := func(n string) string {
		return filepath.Join(tmp, "tv", "Wide Show", "Season 1", "Wide Show - S01E0"+n+".mkv")
	}
	makeMedia(t, ep("1"), "-f", "lavfi", "-i", "testsrc2=s=3840x1600:d=0.2", "-f", "lavfi", "-i", "sine=d=0.2",
		"-c:v", "libx264", "-preset", "ultrafast", "-c:a", "ac3", "-ac", "6", "-metadata:s:a:0", "language=eng")
	makeMedia(t, ep("2"), "-f", "lavfi", "-i", "testsrc2=s=1280x720:d=0.2", "-f", "lavfi", "-i", "sine=d=0.2",
		"-c:v", "libx264", "-preset", "ultrafast", "-c:a", "aac")
	if code := c.do("POST", "/api/v1/libraries", map[string]any{"name": "TV", "kind": "tv", "paths": []string{filepath.Join(tmp, "tv")}}, nil); code != http.StatusCreated {
		t.Fatalf("create library: %d", code)
	}
	waitForScan(t, c)

	var st analyze.Status
	if code := c.do("PUT", "/api/v1/analysis", map[string]string{"mode": "fast"}, nil); code != http.StatusBadRequest {
		t.Fatalf("bad mode: %d", code)
	}
	if code := c.do("PUT", "/api/v1/analysis", map[string]string{"mode": "normal"}, &st); code != 200 || st.Mode != analyze.Normal || st.Total != 2 || st.Done != 0 {
		t.Fatalf("set mode: %d %+v", code, st)
	}
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() { c.srv.Analyzer.Run(ctx); close(stopped) }()
	t.Cleanup(func() { cancel(); <-stopped })
	waitAnalyzed := func(done int) {
		t.Helper()
		deadline := time.Now().Add(30 * time.Second)
		for time.Now().Before(deadline) {
			c.do("GET", "/api/v1/analysis", nil, &st)
			if st.Done == done && st.State == analyze.StateIdle {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Fatalf("analysis didn't finish: %+v", st)
	}
	waitAnalyzed(2)

	var libs []libraryView
	c.do("GET", "/api/v1/libraries", nil, &libs)
	var shows []store.Item
	c.do("GET", "/api/v1/libraries/"+itoa(libs[0].ID)+"/items?tag=4k&tag=5.1", nil, &shows)
	if len(shows) != 1 || !slices.Contains(shows[0].MediaTags, "720p") {
		t.Fatalf("4K shows: %+v", shows)
	}
	c.do("GET", "/api/v1/libraries/"+itoa(libs[0].ID)+"/items?tag=hdr", nil, &shows)
	if len(shows) != 0 {
		t.Fatalf("HDR shows: %+v", shows)
	}

	var seasons, eps []store.Item
	c.do("GET", "/api/v1/items/"+itoa(mustShow(t, c, libs[0].ID).ID)+"/children", nil, &seasons)
	c.do("GET", "/api/v1/items/"+itoa(seasons[0].ID)+"/children", nil, &eps)
	var detail itemView
	c.do("GET", "/api/v1/items/"+itoa(eps[0].ID), nil, &detail)
	mi := detail.MediaInfo
	if mi == nil || mi.Video[0].Resolution != "4K" || mi.Audio[0].Channels != 6 || mi.Audio[0].Language != "eng" ||
		detail.FilePath != ep("1") || detail.FileSize == 0 || detail.MediaInfoStale {
		t.Fatalf("detail: %+v %+v", detail, mi)
	}

	// Replace episode 1 with a 720p file: its tags go until it's analyzed again.
	makeMedia(t, ep("1"), "-f", "lavfi", "-i", "testsrc2=s=1280x720:d=0.3", "-c:v", "libx264", "-preset", "ultrafast")
	os.Chtimes(ep("1"), time.Now(), time.Now().Add(time.Minute))
	c.do("POST", "/api/v1/libraries/"+itoa(libs[0].ID)+"/scan", nil, nil)
	waitForScan(t, c)
	waitAnalyzed(2)
	detail = itemView{}
	c.do("GET", "/api/v1/items/"+itoa(eps[0].ID), nil, &detail)
	if detail.MediaInfo == nil || detail.MediaInfo.Video[0].Resolution != "720p" || len(detail.MediaInfo.Audio) != 0 {
		t.Fatalf("after replacing: %+v", detail.MediaInfo)
	}
	c.do("GET", "/api/v1/libraries/"+itoa(libs[0].ID)+"/items?tag=4k", nil, &shows)
	if len(shows) != 0 {
		t.Fatalf("still 4K: %+v", shows)
	}

	// Off pauses it; asking for one file again works for admins only.
	if code := c.do("PUT", "/api/v1/analysis", map[string]string{"mode": "off"}, &st); code != 200 || st.State != analyze.StateOff {
		t.Fatalf("off: %d %+v", code, st)
	}
	if code := c.do("POST", "/api/v1/items/"+itoa(eps[1].ID)+"/analyze", nil, nil); code != http.StatusAccepted {
		t.Fatalf("reanalyze: %d", code)
	}
	c.do("GET", "/api/v1/analysis", nil, &st)
	if st.Done != 1 {
		t.Fatalf("after reanalyze: %+v", st)
	}
	c.do("POST", "/api/v1/users", map[string]any{"username": "kid", "password": "password123"}, nil)
	kid := signIn(t, c, "kid", "password123", "Phone")
	if code := kid.do("GET", "/api/v1/analysis", nil, nil); code != http.StatusForbidden {
		t.Fatalf("status as non-admin: %d", code)
	}
	detail = itemView{}
	kid.do("GET", "/api/v1/items/"+itoa(eps[0].ID), nil, &detail)
	if detail.FilePath != "" || detail.MediaInfo == nil {
		t.Fatalf("non-admin detail: path %q, info %v", detail.FilePath, detail.MediaInfo)
	}
}

func mustShow(t *testing.T, c *client, lib int64) store.Item {
	t.Helper()
	var shows []store.Item
	c.do("GET", "/api/v1/libraries/"+itoa(lib)+"/items", nil, &shows)
	if len(shows) != 1 {
		t.Fatalf("shows: %+v", shows)
	}
	return shows[0]
}
