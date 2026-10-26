package api

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/yeixio/matinee/internal/theme"
)

func TestThemes(t *testing.T) {
	dir := t.TempDir()
	c, _ := newTestServer(t, func(s *Server) { s.ThemeDir = dir })
	var login struct{ Token string }
	c.do("POST", "/api/setup", credentials{"admin", "correct horse"}, &login)
	signedOut := &client{t: t, base: c.base}
	c.token = login.Token

	// Anyone can see the themes, so the sign-in page can wear the server's.
	var list themeList
	if code := signedOut.do("GET", "/api/v1/themes", nil, &list); code != 200 || list.Default != theme.Default || len(list.Themes) != len(theme.Builtin()) {
		t.Fatalf("themes: %d %+v", code, list)
	}

	// An admin's own theme file shows up without a restart; a broken one is
	// reported in the settings and skipped.
	os.WriteFile(filepath.Join(dir, "mint.json"), []byte(`{"id": "mint", "name": "Mint", "scheme": "dark",
		"colors": {"ink": "#0b1210", "surface": "#101a17", "raised": "#18241f", "line": "#25352f",
		"text": "#eafff6", "muted": "#9cc4b4", "faint": "#6c8e81", "accent": "#3ddc97"}}`), 0o644)
	os.WriteFile(filepath.Join(dir, "broken.json"), []byte(`{`), 0o644)
	signedOut.do("GET", "/api/v1/themes", nil, &list)
	if last := list.Themes[len(list.Themes)-1]; last.ID != "mint" || last.Colors.Glow == "" {
		t.Fatalf("custom theme: %+v", last)
	}
	var set serverSettings
	c.do("GET", "/api/v1/settings", nil, &set)
	if set.Theme != theme.Default || len(set.ThemeProblems) != 1 {
		t.Fatalf("settings: %q %v", set.Theme, set.ThemeProblems)
	}

	// The admin picks the server's theme.
	if code := c.do("PUT", "/api/v1/settings", map[string]any{"theme": "nope"}, nil); code != http.StatusBadRequest {
		t.Errorf("unknown default theme: %d", code)
	}
	c.do("PUT", "/api/v1/settings", map[string]any{"theme": "mint"}, &set)
	signedOut.do("GET", "/api/v1/themes", nil, &list)
	if set.Theme != "mint" || list.Default != "mint" {
		t.Fatalf("default theme: %q %q", set.Theme, list.Default)
	}

	// Each person picks their own, which follows them to every device.
	var mine myTheme
	if c.do("GET", "/api/v1/me/theme", nil, &mine); mine.Theme != "" {
		t.Fatalf("no pick yet: %+v", mine)
	}
	if code := c.do("PUT", "/api/v1/me/theme", myTheme{Theme: "nope"}, nil); code != http.StatusBadRequest {
		t.Errorf("unknown theme: %d", code)
	}
	c.do("PUT", "/api/v1/me/theme", myTheme{Theme: "sci-fi"}, nil)
	if c.do("GET", "/api/v1/me/theme", nil, &mine); mine.Theme != "sci-fi" {
		t.Fatalf("picked: %+v", mine)
	}
	// At home without signing in, it's the shared Home account's.
	signedOut.do("PUT", "/api/v1/me/theme", myTheme{Theme: "light"}, nil)
	var home myTheme
	signedOut.do("GET", "/api/v1/me/theme", nil, &home)
	c.do("GET", "/api/v1/me/theme", nil, &mine)
	if home.Theme != "light" || mine.Theme != "sci-fi" {
		t.Fatalf("home %+v, admin %+v", home, mine)
	}

	// A removed theme file sends people back to the default.
	os.Remove(filepath.Join(dir, "mint.json"))
	signedOut.do("GET", "/api/v1/themes", nil, &list)
	if list.Default != theme.Default {
		t.Errorf("default after its file went: %q", list.Default)
	}
	c.do("PUT", "/api/v1/me/theme", myTheme{Theme: ""}, nil)
	if c.do("GET", "/api/v1/me/theme", nil, &mine); mine.Theme != "" {
		t.Fatalf("back to the default: %+v", mine)
	}
}
