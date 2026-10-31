package stream

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/yeixio/matinee/internal/store"
)

// A Chromecast plays from a page on Google's servers, so media must allow any origin.
func TestServeDirectAllowsCastDevices(t *testing.T) {
	path := filepath.Join(t.TempDir(), "film.mp4")
	if err := os.WriteFile(path, []byte("not really a film"), 0o644); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	ServeDirect(rec, httptest.NewRequest("GET", "/api/v1/items/1/file?token=t", nil), store.Item{Path: path})
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want *", got)
	}
	if got := rec.Header().Get("Content-Type"); got != "video/mp4" {
		t.Fatalf("Content-Type = %q", got)
	}
}

func TestPublicOnly(t *testing.T) {
	for addr, ok := range map[string]bool{
		"93.184.216.34:443": true, "[2606:2800:220:1::]:443": true,
		"127.0.0.1:80": false, "192.168.1.1:80": false, "10.0.0.5:443": false, "169.254.169.254:80": false,
		"[::1]:80": false, "[fd00::1]:80": false, "100.100.1.1:80": false, "[::ffff:192.168.1.1]:80": false,
	} {
		if err := publicOnly("tcp", addr, nil); (err == nil) != ok {
			t.Errorf("%s: %v", addr, err)
		}
	}
	AllowHomeNetwork.Store(true)
	defer AllowHomeNetwork.Store(false)
	if err := publicOnly("tcp", "192.168.1.1:80", nil); err != nil {
		t.Errorf("allowed: %v", err)
	}
}
