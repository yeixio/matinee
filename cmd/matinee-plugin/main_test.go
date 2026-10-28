package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The template's copy of the player library is the SDK's.
func TestTemplatePlayerFiles(t *testing.T) {
	for _, name := range []string{"matinee-player.js", "matinee-player.d.ts"} {
		sdk, err := os.ReadFile(filepath.Join("../../pluginsdk/player", name))
		if err != nil {
			t.Fatal(err)
		}
		tmpl, err := templates.ReadFile("template/player/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(sdk, tmpl) {
			t.Errorf("template/player/%s differs from pluginsdk/player/%s; copy it over", name, name)
		}
	}
}

func output(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	stdout = &buf
	t.Cleanup(func() { stdout = os.Stdout })
	return &buf
}

// newPluginDir makes a plugin with the template, built against this
// repository's SDK, and a folder of files for it.
func newPluginDir(t *testing.T) (dir, library string) {
	t.Helper()
	sdk, err := filepath.Abs("../../pluginsdk")
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	dir = filepath.Join(tmp, "notes")
	output(t)
	if err := runNew([]string{"-dir", dir, "-sdk", sdk, "-ext", ".txt,.md", "-item", "note", "notes"}); err != nil {
		t.Fatal(err)
	}
	library = filepath.Join(tmp, "library")
	for name, body := range map[string]string{"One.txt": "one", "sub/Two.md": "two", "photo.jpg": "x", ".hidden/Three.txt": "3"} {
		p := filepath.Join(library, filepath.FromSlash(name))
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(body), 0o644)
	}
	return dir, library
}

func TestNewBuildScan(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a plugin")
	}
	dir, library := newPluginDir(t)
	man, err := readManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if man.ID != "notes" || man.Name != "Notes" || !slices.Equal(man.LibraryKinds[0].Extensions, []string{".txt", ".md"}) || man.Players[0].ItemKinds[0] != "note" {
		t.Fatalf("manifest %+v", man)
	}

	out := output(t)
	if err := runBuild([]string{dir}); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	zr, err := zip.OpenReader(filepath.Join(dir, "notes.matinee-plugin"))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	zr.Close()
	want := []string{"README.md", "icon.png", "player/index.html", "player/matinee-player.d.ts", "player/matinee-player.js", "player/player.css", "player/player.js", "plugin.json", "server.wasm"}
	if !slices.Equal(names, want) {
		t.Errorf("package has %v, want %v", names, want)
	}

	out = output(t)
	if err := runScan([]string{dir, library}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, s := range []string{"One.txt\n  note \"One\"", "sub/Two.md\n  note \"Two\"", "2 files: 2 items"} {
		if !strings.Contains(got, s) {
			t.Errorf("scan output lacks %q:\n%s", s, got)
		}
	}
	if strings.Contains(got, "photo") || strings.Contains(got, "Three") {
		t.Errorf("scan asked about files it shouldn't:\n%s", got)
	}

	out = output(t)
	if err := runScan([]string{"-json", dir, library}); err != nil {
		t.Fatal(err)
	}
	var files []scanned
	if err := json.Unmarshal(out.Bytes(), &files); err != nil || len(files) != 2 || files[0].Path != "One.txt" {
		t.Errorf("json: %v %s", err, out)
	}

	t.Run("try", func(t *testing.T) { testTry(t, dir, library) })
}

func TestCheckProblems(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "plugin.json"), []byte(`{"id":"bad","name":"Bad","version":"1","api":1,"icon":"icon.png",
		"players":[{"itemKinds":["game"],"entry":"player/index.html"}]}`), 0o644)
	out := output(t)
	if _, err := check(t.Context(), dir); err == nil {
		t.Fatal("a plugin without its files passed")
	}
	for _, s := range []string{"problem: the icon icon.png isn't there", "problem: the player player/index.html isn't there", `warning: version "1"`} {
		if !strings.Contains(out.String(), s) {
			t.Errorf("output lacks %q:\n%s", s, out)
		}
	}

	os.WriteFile(filepath.Join(dir, "plugin.json"), []byte(`{"id":"bad","name":"Bad","version":"1.0.0","api":1,"libraryKinds":[{"name":"movies"}]}`), 0o644)
	if _, err := check(t.Context(), dir); err == nil || !strings.Contains(err.Error(), "server module") {
		t.Errorf("got %v", err)
	}
}

func testTry(t *testing.T, dir, library string) {
	tr, err := newTry(dir, library, "", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(tr)
	defer srv.Close()
	do := func(method, path string, body string) (int, http.Header, string) {
		t.Helper()
		req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return res.StatusCode, res.Header, string(b)
	}

	code, _, body := do("GET", "/api/items", "")
	var list struct {
		Items []tryItem `json:"items"`
	}
	json.Unmarshal([]byte(body), &list)
	if code != 200 || len(list.Items) != 2 || list.Items[0].Title != "One" || list.Items[0].Player != "/plugin/player/index.html" {
		t.Fatalf("items: %d %s", code, body)
	}
	if code, _, body := do("GET", "/api/file/1", ""); code != 200 || body != "one" {
		t.Errorf("file: %d %q", code, body)
	}
	if code, _, body := do("GET", "/api/library-file?path=sub/Two.md", ""); code != 200 || body != "two" {
		t.Errorf("library file: %d %q", code, body)
	}
	for _, p := range []string{"photo.jpg", "../notes/plugin.json", "/etc/passwd"} {
		if code, _, _ := do("GET", "/api/library-file?path="+p, ""); code == 200 {
			t.Errorf("library file %s was handed over", p)
		}
	}

	code, h, _ := do("GET", "/plugin/player/index.html", "")
	if code != 200 || !strings.HasPrefix(h.Get("Content-Security-Policy"), "sandbox") {
		t.Errorf("player: %d %v", code, h)
	}
	for _, p := range []string{"/plugin/plugin.json", "/plugin/server.wasm", "/plugin/player/../plugin.json"} {
		if code, _, _ := do("GET", p, ""); code != 404 {
			t.Errorf("%s: %d, want 404", p, code)
		}
	}

	if code, _, _ := do("GET", "/api/storage/saves/1", ""); code != 404 {
		t.Errorf("an empty key: %d", code)
	}
	if code, _, _ := do("PUT", "/api/storage/saves/1", "state"); code != 204 {
		t.Errorf("put: %d", code)
	}
	if code, _, body := do("GET", "/api/storage/saves/1", ""); code != 200 || body != "state" {
		t.Errorf("get: %d %q", code, body)
	}
	if _, _, body := do("GET", "/api/storage?prefix=saves/", ""); !strings.Contains(body, `"key":"saves/1","size":5`) {
		t.Errorf("list: %s", body)
	}
	if code, _, _ := do("PUT", "/api/storage/..%2Fescape", "x"); code != 400 {
		t.Errorf("a bad key: %d", code)
	}
	do("DELETE", "/api/storage/saves/1", "")
	if code, _, _ := do("GET", "/api/storage/saves/1", ""); code != 404 {
		t.Errorf("after delete: %d", code)
	}
}
