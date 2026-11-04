package api

import (
	"io"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/yeixio/matinee/internal/backup"
)

// Admins list, make, download and restore database backups.
func TestBackups(t *testing.T) {
	admin, _, db := newTestServerStore(t, func(s *Server) {
		s.Backups = &backup.Manager{Store: s.Store, Dir: filepath.Join(s.MediaRoot, "backups"), DBPath: filepath.Join(s.MediaRoot, "matinee.db")}
	})
	admin.do("POST", "/api/v1/setup", credentials{"admin", "correct horse"}, nil)
	admin, _ = tryLogin(t, admin, "admin", "correct horse")
	admin.do("POST", "/api/v1/users", map[string]any{"username": "bob", "password": "bob password"}, nil)
	user, _ := tryLogin(t, admin, "bob", "bob password")
	if code := user.do("GET", "/api/v1/admin/backups", nil, nil); code != http.StatusForbidden {
		t.Fatalf("not an admin: %d", code)
	}

	var v backupsView
	if code := admin.do("GET", "/api/v1/admin/backups", nil, &v); code != http.StatusOK {
		t.Fatalf("list: %d", code)
	}
	if !db.SQLite() {
		if v.Supported || v.Database != "postgres" {
			t.Fatalf("postgres: %+v", v)
		}
		if code := admin.do("POST", "/api/v1/admin/backups", nil, nil); code != http.StatusConflict {
			t.Fatalf("make on postgres: %d", code)
		}
		return
	}
	if !v.Supported || len(v.Backups) != 0 || v.Settings != backup.DefaultSettings {
		t.Fatalf("empty: %+v", v)
	}
	if code := admin.do("POST", "/api/v1/admin/backups", nil, &v); code != http.StatusOK || len(v.Backups) != 1 || v.Backups[0].Reason != backup.ReasonManual {
		t.Fatalf("make: %d %+v", code, v)
	}
	name := v.Backups[0].Name

	req, _ := http.NewRequest("GET", admin.base+"/api/v1/admin/backups/"+name, nil)
	req.Header.Set("Authorization", "Bearer "+admin.token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	head := make([]byte, 16)
	io.ReadFull(res.Body, head)
	res.Body.Close()
	if res.StatusCode != http.StatusOK || string(head) != "SQLite format 3\x00" {
		t.Fatalf("download: %d %q", res.StatusCode, head)
	}
	if code := admin.do("GET", "/api/v1/admin/backups/..%2fmatinee.db", nil, nil); code != http.StatusNotFound {
		t.Fatalf("a path out of the folder: %d", code)
	}

	if code := admin.do("PUT", "/api/v1/admin/backups/settings", backup.Settings{EveryHours: 6, Keep: 3}, &v); code != http.StatusOK || v.Settings.EveryHours != 6 || v.Settings.Keep != 3 {
		t.Fatalf("settings: %d %+v", code, v.Settings)
	}
	if code := admin.do("POST", "/api/v1/admin/backups/"+name+"/restore", nil, &v); code != http.StatusOK || v.Restore != name {
		t.Fatalf("restore: %d %+v", code, v)
	}
	v = backupsView{}
	if code := admin.do("DELETE", "/api/v1/admin/backups/restore", nil, &v); code != http.StatusOK || v.Restore != "" {
		t.Fatalf("cancel: %d %+v", code, v)
	}
	v = backupsView{}
	if code := admin.do("DELETE", "/api/v1/admin/backups/"+name, nil, &v); code != http.StatusOK || len(v.Backups) != 0 {
		t.Fatalf("delete: %d %+v", code, v)
	}
}
