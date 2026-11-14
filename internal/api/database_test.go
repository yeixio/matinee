package api

import (
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

// Admins check a database to move to and ask for the move, which waits for
// the next start; a server that can restart itself does so at once.
func TestDatabaseMove(t *testing.T) {
	restarted := make(chan struct{}, 1)
	admin, _, _ := newTestServerStore(t, func(s *Server) {
		s.Database = DatabaseInfo{DSN: "/data/matinee.db", Configured: "/data/matinee.db", DataDir: s.MediaRoot}
		s.Restart = func() { restarted <- struct{}{} }
	})
	admin.do("POST", "/api/v1/setup", credentials{"admin", "correct horse"}, nil)
	admin, _ = tryLogin(t, admin, "admin", "correct horse")
	admin.do("POST", "/api/v1/users", map[string]any{"username": "bob", "password": "bob password"}, nil)
	user, _ := tryLogin(t, admin, "bob", "bob password")
	if code := user.do("GET", "/api/v1/admin/database", nil, nil); code != http.StatusForbidden {
		t.Fatalf("not an admin: %d", code)
	}
	var v databaseView
	if code := admin.do("GET", "/api/v1/admin/database", nil, &v); code != http.StatusOK || v.Where != "/data/matinee.db" || v.SuggestedFile == "" || !v.SQLiteOK {
		t.Fatalf("view: %d %+v", code, v)
	}
	var e struct{ Error string }
	if code := admin.do("POST", "/api/v1/admin/database/check", moveRequest{To: "relative.db"}, &e); code != http.StatusBadRequest || !strings.Contains(e.Error, "full path") {
		t.Fatalf("relative path: %d %q", code, e.Error)
	}
	to := filepath.Join(t.TempDir(), "moved.db")
	if code := admin.do("POST", "/api/v1/admin/database/check", moveRequest{To: to}, nil); code != http.StatusOK {
		t.Fatalf("check: %d", code)
	}
	var m moveAnswer
	if code := admin.do("POST", "/api/v1/admin/database/move", moveRequest{To: to}, &m); code != http.StatusOK || m.Pending != to || !m.Restarting {
		t.Fatalf("move: %d %+v", code, m)
	}
	<-restarted
	if code := admin.do("DELETE", "/api/v1/admin/database/move", nil, &v); code != http.StatusOK || v.Pending != "" {
		t.Fatalf("cancel: %d %+v", code, v)
	}
}

// On Kubernetes a SQLite file isn't offered: the data folder doesn't last.
func TestDatabaseMoveKubernetes(t *testing.T) {
	admin, _, _ := newTestServerStore(t, func(s *Server) {
		s.Database = DatabaseInfo{DSN: "postgres://matinee:x@db/matinee", DataDir: s.MediaRoot, Kubernetes: true}
	})
	admin.do("POST", "/api/v1/setup", credentials{"admin", "correct horse"}, nil)
	admin, _ = tryLogin(t, admin, "admin", "correct horse")
	var v databaseView
	if code := admin.do("GET", "/api/v1/admin/database", nil, &v); code != http.StatusOK || v.SQLiteOK || v.SuggestedFile != "" || strings.Contains(v.Where, ":x@") {
		t.Fatalf("view: %d %+v", code, v)
	}
	if code := admin.do("POST", "/api/v1/admin/database/check", moveRequest{To: filepath.Join(t.TempDir(), "m.db")}, nil); code != http.StatusBadRequest {
		t.Fatalf("sqlite on kubernetes: %d", code)
	}
}
