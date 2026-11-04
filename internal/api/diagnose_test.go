package api

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/yeixio/matinee/internal/diagnose"
	"github.com/yeixio/matinee/internal/store"
)

// A player says it stalled and hears why; admins also hear what to change.
func TestPlaybackReport(t *testing.T) {
	admin, tmp, db := newTestServerStore(t)
	admin.do("POST", "/api/v1/setup", credentials{"admin", "correct horse"}, nil)
	admin, _ = tryLogin(t, admin, "admin", "correct horse")
	var bob store.User
	admin.do("POST", "/api/v1/users", map[string]any{"username": "bob", "password": "bob password"}, &bob)
	user, _ := tryLogin(t, admin, "bob", "bob password")

	ctx := context.Background()
	path := filepath.Join(tmp, "m", "heat.mkv")
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, make([]byte, 1<<20), 0o644)
	lib, _ := db.CreateLibrary(ctx, "Movies", "movies", []string{filepath.Dir(path)})
	dur := int64(60_000)
	id, _ := db.UpsertFile(ctx, store.Item{LibraryID: lib.ID, Kind: "movie", Title: "Heat", Path: path, Size: 1 << 20, DurationMs: &dur})

	if code := user.do("POST", "/api/v1/items/"+itoa(id)+"/playback-report", map[string]any{"kind": "nap"}, nil); code != http.StatusBadRequest {
		t.Fatalf("bad kind: %d", code)
	}
	report := map[string]any{"kind": "stall", "positionMs": 30_000, "downloadBps": 1_000_000, "neededBps": 8_000_000}
	var out struct{ Cause diagnose.Cause }
	if code := admin.do("POST", "/api/v1/items/"+itoa(id)+"/playback-report", report, &out); code != http.StatusOK {
		t.Fatalf("report: %d", code)
	}
	if out.Cause.Code != diagnose.CauseSlowConnection || out.Cause.Values["have"] != "1.0" || out.Cause.Values["need"] != "8.0" || out.Cause.Tip != "adaptive" {
		t.Fatalf("admin's cause: %+v", out.Cause)
	}
	out.Cause = diagnose.Cause{}
	if code := user.do("POST", "/api/v1/items/"+itoa(id)+"/playback-report", report, &out); code != http.StatusOK {
		t.Fatalf("bob's report: %d", code)
	}
	if out.Cause.Code != diagnose.CauseSlowConnection || out.Cause.Tip != "" {
		t.Fatalf("bob's cause has no tip: %+v", out.Cause)
	}

	// Why, later: each person sees their own.
	var why struct{ Cause *diagnose.Cause }
	user.do("GET", "/api/v1/items/"+itoa(id)+"/playback-why", nil, &why)
	if why.Cause == nil || why.Cause.Code != diagnose.CauseSlowConnection || why.Cause.Tip != "" {
		t.Fatalf("why: %+v", why.Cause)
	}

	// The connection test sends what's asked for.
	req, _ := http.NewRequest("GET", user.base+"/api/v1/speedtest?bytes=12345", nil)
	req.Header.Set("Authorization", "Bearer "+user.token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	n, _ := io.Copy(io.Discard, res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusOK || n != 12345 {
		t.Fatalf("speedtest: %d, %d bytes", res.StatusCode, n)
	}
	if code := user.do("GET", "/api/v1/speedtest?bytes=-1", nil, nil); code != http.StatusBadRequest {
		t.Fatalf("negative bytes: %d", code)
	}
}
