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
