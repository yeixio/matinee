package api

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/yeixio/matinee/internal/plugin"
)

// A player's page is served at the path the manifest names, walled off
// from the site by its headers.
func TestPluginPlayerFiles(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range map[string]string{
		"plugin.json":       `{"id": "pages", "name": "Pages", "version": "1.0.0", "api": 1, "players": [{"itemKinds": ["page"], "entry": "player/index.html"}]}`,
		"player/index.html": "<!doctype html><title>player</title>",
		"player/run.wasm":   "\x00asm",
		"secret.txt":        "not for players",
	} {
		w, _ := zw.Create(name)
		io.WriteString(w, body)
	}
	zw.Close()

	var mgr *plugin.Manager
	c, _ := newTestServer(t, func(s *Server) {
		var err error
		mgr, err = plugin.Open(context.Background(), filepath.Join(t.TempDir(), "plugins"), s.Store, slog.New(slog.NewTextHandler(io.Discard, nil)))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { mgr.Close(context.Background()) })
		s.Plugins = mgr
	})
	ctx := context.Background()
	if _, err := mgr.Install(ctx, buf.Bytes()); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.SetEnabled(ctx, "pages", true); err != nil {
		t.Fatal(err)
	}

	get := func(p string) *http.Response {
		t.Helper()
		res, err := http.Get(c.base + "/api/v1/plugins/pages/files/" + p)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res
	}
	res := get("player/index.html")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("entry page: %d", res.StatusCode)
	}
	if csp := res.Header.Get("Content-Security-Policy"); csp != "sandbox allow-scripts allow-pointer-lock allow-downloads" {
		t.Errorf("CSP %q", csp)
	}
	if ct := res.Header.Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Errorf("entry page type %q", ct)
	}
	if ct := get("player/run.wasm").Header.Get("Content-Type"); ct != "application/wasm" {
		t.Errorf("wasm type %q", ct)
	}
	for _, p := range []string{"secret.txt", "plugin.json", "player/", "player/../secret.txt"} {
		if code := get(p).StatusCode; code != http.StatusNotFound {
			t.Errorf("%s: %d, want 404", p, code)
		}
	}
}
