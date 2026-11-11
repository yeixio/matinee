package api

import (
	"net/http"
	"testing"
)

// Admins restart the server from Settings; asking twice restarts it once.
func TestRestart(t *testing.T) {
	restarts := make(chan struct{}, 4)
	admin, _, _ := newTestServerStore(t, func(s *Server) {
		s.Restart = func() { restarts <- struct{}{} }
	})
	admin.do("POST", "/api/v1/setup", credentials{"admin", "correct horse"}, nil)
	admin, _ = tryLogin(t, admin, "admin", "correct horse")
	admin.do("POST", "/api/v1/users", map[string]any{"username": "bob", "password": "bob password"}, nil)
	user, _ := tryLogin(t, admin, "bob", "bob password")
	if code := user.do("POST", "/api/v1/admin/restart", nil, nil); code != http.StatusForbidden {
		t.Fatalf("not an admin: %d", code)
	}

	var h healthReport
	if code := admin.do("GET", "/api/v1/admin/health", nil, &h); code != http.StatusOK || !h.Server.CanRestart {
		t.Fatalf("health: %d canRestart=%v", code, h.Server.CanRestart)
	}
	for range 2 {
		var out restartAnswer
		if code := admin.do("POST", "/api/v1/admin/restart", nil, &out); code != http.StatusAccepted || out.BootID != bootID {
			t.Fatalf("restart: %d %+v", code, out)
		}
	}
	<-restarts
	select {
	case <-restarts:
		t.Fatal("restarted twice")
	default:
	}
}

// A server that can't restart itself says so, and Settings offers no button.
func TestRestartUnavailable(t *testing.T) {
	admin, _, _ := newTestServerStore(t)
	admin.do("POST", "/api/v1/setup", credentials{"admin", "correct horse"}, nil)
	admin, _ = tryLogin(t, admin, "admin", "correct horse")
	var h healthReport
	if code := admin.do("GET", "/api/v1/admin/health", nil, &h); code != http.StatusOK || h.Server.CanRestart {
		t.Fatalf("health: %d canRestart=%v", code, h.Server.CanRestart)
	}
	if code := admin.do("POST", "/api/v1/admin/restart", nil, nil); code != http.StatusConflict {
		t.Fatalf("restart: %d", code)
	}
}
