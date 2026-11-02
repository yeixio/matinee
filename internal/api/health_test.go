package api

import (
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yeixio/matinee/internal/health"
	"github.com/yeixio/matinee/internal/store"
)

// The admin dashboard: how the server is doing, its disks, and its log.
func TestHealthDashboard(t *testing.T) {
	logs := health.NewLogs(100)
	admin, tmp := newTestServer(t, func(s *Server) {
		s.Log = slog.New(logs.Handler(slog.NewTextHandler(io.Discard, nil)))
		s.Health = health.NewMonitor(logs)
		s.Storage = []StoragePath{{Use: "data", Path: s.MediaRoot}, {Use: "thumbnails", Path: filepath.Join(s.MediaRoot, "not", "made", "yet")}}
	})
	admin.do("POST", "/api/v1/setup", credentials{"admin", "correct horse"}, nil)
	admin, _ = tryLogin(t, admin, "admin", "correct horse")
	var bob store.User
	admin.do("POST", "/api/v1/users", map[string]any{"username": "bob", "password": "bob password"}, &bob)
	user, _ := tryLogin(t, admin, "bob", "bob password")
	for _, path := range []string{"/api/v1/admin/health", "/api/v1/admin/logs", "/api/v1/admin/logs/download"} {
		if code := user.do("GET", path, nil, nil); code != http.StatusForbidden {
			t.Fatalf("%s for someone who isn't an admin: %d", path, code)
		}
	}

	movies := filepath.Join(tmp, "movies")
	gone := filepath.Join(tmp, "share")
	os.MkdirAll(movies, 0o755)
	os.MkdirAll(gone, 0o755)
	if code := admin.do("POST", "/api/v1/libraries", map[string]any{"name": "Movies", "kind": "movies", "paths": []string{movies, gone}}, nil); code != http.StatusCreated {
		t.Fatalf("create library: %d", code)
	}
	waitForScan(t, admin)
	os.RemoveAll(gone)
	admin.srv.Log.Error("request failed", "err", "disk full")

	var h healthReport
	if code := admin.do("GET", "/api/v1/admin/health", nil, &h); code != http.StatusOK {
		t.Fatalf("health: %d", code)
	}
	if h.Database == nil || (h.Database.Kind != "sqlite" && h.Database.Kind != "postgres") || h.Database.Users != 2 || h.Database.Libraries != 1 || h.Database.Schema == 0 {
		t.Fatalf("database: %+v %s", h.Database, h.DatabaseError)
	}
	if h.System.Requests == 0 || h.System.BytesOut == 0 {
		t.Fatalf("traffic: %+v", h.System)
	}
	if len(h.Jobs.Scans) != 1 || h.Jobs.Scans[0].Name != "Movies" || h.Jobs.Scans[0].Last == nil || h.Jobs.Analysis == nil {
		t.Fatalf("jobs: %+v", h.Jobs)
	}
	if len(h.Problems) != 1 || h.Problems[0].Detail != "disk full" {
		t.Fatalf("problems: %+v", h.Problems)
	}
	// The data folder, the folder not made yet and the movies folder share
	// a disk; the share that went away can't be read.
	var reachable, unreachable []healthUse
	for _, d := range h.Disks {
		if d.Reachable {
			reachable = append(reachable, d.Uses...)
		} else {
			unreachable = append(unreachable, d.Uses...)
		}
	}
	if len(reachable) != 3 || len(unreachable) != 1 || unreachable[0].Path != gone || unreachable[0].Library != "Movies" {
		t.Fatalf("disks: %+v", h.Disks)
	}

	var page struct {
		Lines []health.Entry
		More  bool
	}
	admin.do("GET", "/api/v1/admin/logs?level=error", nil, &page)
	if len(page.Lines) != 1 || page.Lines[0].Message != "request failed" {
		t.Fatalf("error lines: %+v", page)
	}
	admin.do("GET", "/api/v1/admin/logs?q=nothing+says+this", nil, &page)
	if len(page.Lines) != 0 {
		t.Fatalf("search: %+v", page)
	}

	req, _ := http.NewRequest("GET", admin.base+"/api/v1/admin/logs/download", nil)
	req.Header.Set("Authorization", "Bearer "+admin.token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if !strings.Contains(res.Header.Get("Content-Disposition"), "attachment") || !strings.Contains(string(body), "ERROR request failed err=\"disk full\"") {
		t.Fatalf("download: %s\n%s", res.Header, body)
	}

	if code := admin.do("DELETE", "/api/v1/admin/transcodes/nope", nil, nil); code != http.StatusNotFound {
		t.Fatalf("stop a transcode that isn't running: %d", code)
	}
}
