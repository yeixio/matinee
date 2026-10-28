package plugin

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/tls"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yeixio/matinee/internal/metadata"
	"github.com/yeixio/matinee/internal/probe"
	"github.com/yeixio/matinee/internal/scan"
	"github.com/yeixio/matinee/internal/store"
	"github.com/yeixio/matinee/internal/store/storetest"
)

var (
	testkitOnce sync.Once
	testkitWasm []byte
	testkitErr  error
)

// testkit builds the test plugin in testdata/testkit with the SDK.
func testkit(t *testing.T) []byte {
	t.Helper()
	testkitOnce.Do(func() {
		dir, err := os.MkdirTemp("", "testkit")
		if err != nil {
			testkitErr = err
			return
		}
		defer os.RemoveAll(dir)
		out := filepath.Join(dir, "server.wasm")
		cmd := exec.Command("go", "build", "-buildmode=c-shared", "-o", out, ".")
		cmd.Dir = "testdata/testkit"
		cmd.Env = append(os.Environ(), "GOOS=wasip1", "GOARCH=wasm", "GOFLAGS=-mod=mod")
		if b, err := cmd.CombinedOutput(); err != nil {
			testkitErr = err
			testkitWasm = b
			return
		}
		testkitWasm, testkitErr = os.ReadFile(out)
	})
	if testkitErr != nil {
		t.Fatalf("build test plugin: %v\n%s", testkitErr, testkitWasm)
	}
	return testkitWasm
}

// pkg zips files into a plugin package.
func pkg(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, b := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write(b)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func testkitPackage(t *testing.T, manifest string) []byte {
	if manifest == "" {
		b, err := os.ReadFile("testdata/testkit/plugin.json")
		if err != nil {
			t.Fatal(err)
		}
		manifest = string(b)
	}
	return pkg(t, map[string][]byte{
		"plugin.json":       []byte(manifest),
		"server.wasm":       testkit(t),
		"player/index.html": []byte("<!doctype html><title>player</title>"),
		"player/app.js":     []byte("console.log('hi')"),
		"notes.txt":         []byte("not served"),
	})
}

func quietLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func openManager(t *testing.T) (*Manager, *store.Store) {
	t.Helper()
	st := storetest.Open(t)
	m, err := Open(context.Background(), t.TempDir(), st, quietLog())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close(context.Background()) })
	return m, st
}

func write(t *testing.T, path, content string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestManifestChecks(t *testing.T) {
	good := `{"id":"ok","name":"OK","version":"1","api":1,"players":[{"itemKinds":["game"],"entry":"player/index.html"}]}`
	if _, err := ParseManifest([]byte(good)); err != nil {
		t.Fatalf("good manifest: %v", err)
	}
	for name, bad := range map[string]string{
		"bad id":           `{"id":"Bad Id","name":"x","version":"1","api":1,"players":[{"itemKinds":["game"],"entry":"p.html"}]}`,
		"no parts":         `{"id":"ok","name":"x","version":"1","api":1}`,
		"kinds no server":  `{"id":"ok","name":"x","version":"1","api":1,"libraryKinds":[{"name":"games","label":"G","extensions":[".z"],"itemKinds":["game"]}],"players":[{"itemKinds":["game"],"entry":"p.html"}]}`,
		"builtin kind":     `{"id":"ok","name":"x","version":"1","api":1,"server":"s.wasm","libraryKinds":[{"name":"movies","label":"M","extensions":[".z"],"itemKinds":["game"]}]}`,
		"builtin item":     `{"id":"ok","name":"x","version":"1","api":1,"server":"s.wasm","libraryKinds":[{"name":"games","label":"G","extensions":[".z"],"itemKinds":["movie"]}]}`,
		"escaping path":    `{"id":"ok","name":"x","version":"1","api":1,"server":"../s.wasm"}`,
		"absolute entry":   `{"id":"ok","name":"x","version":"1","api":1,"players":[{"itemKinds":["game"],"entry":"/p.html"}]}`,
		"plays movies":     `{"id":"ok","name":"x","version":"1","api":1,"players":[{"itemKinds":["movie"],"entry":"p.html"}]}`,
		"metadata movies":  `{"id":"ok","name":"x","version":"1","api":1,"server":"s.wasm","metadata":["movie"]}`,
		"unknown event":    `{"id":"ok","name":"x","version":"1","api":1,"server":"s.wasm","events":["user.deleted"]}`,
		"ip host":          `{"id":"ok","name":"x","version":"1","api":1,"server":"s.wasm","permissions":{"network":["10.0.0.1"]}}`,
		"wildcard all":     `{"id":"ok","name":"x","version":"1","api":1,"server":"s.wasm","permissions":{"network":["*"]}}`,
		"extension no dot": `{"id":"ok","name":"x","version":"1","api":1,"server":"s.wasm","libraryKinds":[{"name":"games","label":"G","extensions":["zip"],"itemKinds":["game"]}]}`,
	} {
		if _, err := ParseManifest([]byte(bad)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	p := Permissions{Network: []string{"api.example.com", "*.cdn.example.org"}}
	for host, want := range map[string]bool{
		"api.example.com": true, "API.example.com.": true, "example.com": false,
		"a.cdn.example.org": true, "cdn.example.org": false, "evil-cdn.example.org": false,
	} {
		if p.AllowsHost(host) != want {
			t.Errorf("AllowsHost(%q) = %v", host, !want)
		}
	}
}

func TestBadPackages(t *testing.T) {
	m, _ := openManager(t)
	ctx := context.Background()
	manifest := `{"id":"ok","name":"OK","version":"1","api":1,"players":[{"itemKinds":["game"],"entry":"player/index.html"}]}`
	for name, files := range map[string]map[string][]byte{
		"no manifest":   {"player/index.html": nil},
		"missing entry": {"plugin.json": []byte(manifest)},
		"zip slip":      {"plugin.json": []byte(manifest), "player/index.html": nil, "../evil": []byte("x")},
		"not wasm": {
			"plugin.json": []byte(`{"id":"ok","name":"OK","version":"1","api":1,"server":"s.wasm","events":["item.added"]}`),
			"s.wasm":      []byte("nope"),
		},
	} {
		if _, err := m.Install(ctx, pkg(t, files)); err == nil {
			t.Errorf("%s: installed", name)
		}
	}
	if _, err := m.Install(ctx, []byte("not a zip")); err == nil {
		t.Error("not a zip: installed")
	}
	if len(m.List()) != 0 {
		t.Fatalf("left plugins behind: %+v", m.List())
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(m.Dir), "evil")); err == nil {
		t.Fatal("zip slip wrote outside the plugins folder")
	}
}

func TestInstallScanLookupAndEvents(t *testing.T) {
	m, st := openManager(t)
	ctx := context.Background()
	changes := 0
	m.OnChange = func() { changes++ }

	v, err := m.Install(ctx, testkitPackage(t, ""))
	if err != nil {
		t.Fatal(err)
	}
	if v.Enabled || v.Running || !v.Supported {
		t.Fatalf("a new plugin should start off: %+v", v)
	}
	if _, ok := scan.KindByName("games"); ok {
		t.Fatal("kind registered before the plugin was turned on")
	}
	if _, err := m.SetConfig(ctx, "testkit", map[string]string{"greeting": "hello", "token": "s3cret", "other": "x"}); err != nil {
		t.Fatal(err)
	}
	if v, err = m.SetEnabled(ctx, "testkit", true); err != nil || !v.Running {
		t.Fatalf("enable: %+v %v", v, err)
	}
	if v.Config["token"] != "••••••" || v.Config["greeting"] != "hello" || v.Config["other"] != "" {
		t.Fatalf("config shown: %+v", v.Config)
	}
	if changes != 2 {
		t.Fatalf("OnChange ran %d times", changes)
	}
	kind, ok := scan.KindByName("games")
	if !ok {
		t.Fatal("games kind not registered")
	}

	root := t.TempDir()
	write(t, filepath.Join(root, "Pac.game"), `{"title":"Pac","year":1980,"system":"Arcade"}`)
	write(t, filepath.Join(root, "Pac.png"), "png")
	write(t, filepath.Join(root, "sub", "Gal.game"), `{"title":"Gal","year":1981,"system":"Arcade"}`)
	write(t, filepath.Join(root, "Skip.game"), `{"skip":true}`)
	write(t, filepath.Join(root, "Boom.game"), `{"panic":true}`)
	write(t, filepath.Join(root, "Wrong.game"), `{"title":"Wrong","kind":"movie"}`)
	write(t, filepath.Join(root, "readme.txt"), "ignored")
	lib, err := st.CreateLibrary(ctx, "Games", "games", []string{root})
	if err != nil {
		t.Fatal(err)
	}
	var added []int64
	s := &scan.Scanner{Store: st, Prober: probe.Prober{Path: "/nonexistent/ffprobe"}, Log: quietLog(),
		Added: func(ctx context.Context, lib store.Library, id int64) {
			added = append(added, id)
			m.ItemAdded(ctx, id)
		},
		AfterScan: func(ctx context.Context, lib store.Library) {
			m.Emit(LibraryEvent(EventLibraryScanned, lib.ID, lib.Name, lib.Kind))
		},
	}
	stats, err := s.Scan(ctx, lib)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Added != 2 || stats.Unplayable != 1 || stats.Failed != 2 {
		t.Fatalf("stats: %+v", stats)
	}
	top, err := st.TopLevel(ctx, 0, lib.ID)
	if err != nil || len(top) != 1 || top[0].Kind != "system" || top[0].Title != "Arcade" {
		t.Fatalf("top level: %+v %v", top, err)
	}
	games, err := st.Children(ctx, 0, top[0].ID)
	if err != nil || len(games) != 2 {
		t.Fatalf("games: %+v %v", games, err)
	}
	pac := games[1]
	if games[0].Title == "Pac" {
		pac = games[0]
	}
	pac, err = st.Item(ctx, 0, pac.ID)
	if err != nil {
		t.Fatal(err)
	}
	if pac.Kind != "game" || pac.Year == nil || *pac.Year != 1980 || !pac.Playable {
		t.Fatalf("pac: %+v", pac)
	}
	if pac.Overview != "Says hello" || len(pac.Genres) != 1 || len(pac.Facts) != 1 || pac.Facts[0].Value != "Pac.game" {
		t.Fatalf("pac details: %q %v %v", pac.Overview, pac.Genres, pac.Facts)
	}
	if string(pac.PluginData) != `{"system":"Arcade"}` {
		t.Fatalf("pac data: %s", pac.PluginData)
	}
	if pac.Poster != filepath.Join(root, "Pac.png") {
		t.Fatalf("pac poster: %q", pac.Poster)
	}
	if kind.Wants(filepath.Join(root, "readme.txt")) || !kind.Wants(filepath.Join(root, "X.GAME")) {
		t.Fatal("Wants goes by the manifest's extensions")
	}

	// Lookups go to the plugin for its own kinds.
	f := &metadata.Fetcher{Store: st, Log: quietLog(), Plugins: m.Providers}
	if k := f.Kinds(); len(k) != 1 || k[0] != "game" {
		t.Fatalf("metadata kinds: %v", k)
	}
	d, err := m.Providers()[0].Lookup(ctx, metadata.Query{Kind: "game", Title: "Gal", Data: pac.PluginData})
	if err != nil || d == nil || d.Overview != "Looked up Gal" || d.Facts[0].Value != `{"system":"Arcade"}` {
		t.Fatalf("lookup: %+v %v", d, err)
	}
	if d, err := m.Providers()[0].Lookup(ctx, metadata.Query{Kind: "game", Title: "Nothing"}); d != nil || err != nil {
		t.Fatalf("lookup with no match: %+v %v", d, err)
	}

	// Playback: a report starts it, finishing stops it.
	m.Played(store.User{ID: 1, Username: "sam"}, pac, 1000, false)
	m.Played(store.User{ID: 1, Username: "sam"}, pac, 2000, false)
	m.Played(store.User{ID: 1, Username: "sam"}, pac, 3000, true)
	want := []string{"item.added Gal", "item.added Pac", "library.scanned ", "playback.started Pac", "playback.stopped Pac"}
	log := filepath.Join(m.Dir, "testkit.data", "events.log")
	var got string
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		b, _ := os.ReadFile(log)
		if got = string(b); strings.Count(got, "\n") >= len(want) {
			break
		}
	}
	lines := strings.Split(strings.TrimSpace(got), "\n")
	if len(lines) != len(want) {
		t.Fatalf("events:\n%s", got)
	}
	// Items are added in walk order; the rest in order.
	if !(lines[0] == want[0] && lines[1] == want[1] || lines[0] == want[1] && lines[1] == want[0]) || strings.Join(lines[2:], "|") != strings.Join(want[2:], "|") {
		t.Fatalf("events:\n%s", got)
	}

	// The web player gets the player's folder, nothing else.
	if _, ok := m.File("testkit", "player/app.js"); !ok {
		t.Fatal("player file not served")
	}
	for _, name := range []string{"notes.txt", "server.wasm", "plugin.json", "player/../notes.txt", "player"} {
		if _, ok := m.File("testkit", name); ok {
			t.Fatalf("%s served", name)
		}
	}
	if p, ok := m.PlayerFor("game"); !ok || p != "testkit" {
		t.Fatalf("player for games: %q %v", p, ok)
	}

	// Turning it off takes its kind away; the library's items stay.
	if _, err := m.SetEnabled(ctx, "testkit", false); err != nil {
		t.Fatal(err)
	}
	if _, ok := scan.KindByName("games"); ok {
		t.Fatal("kind still registered")
	}
	if _, ok := m.File("testkit", "player/app.js"); ok {
		t.Fatal("a stopped plugin's player is still served")
	}
	if _, err := s.Scan(ctx, lib); err == nil {
		t.Fatal("scanned a library whose plugin is off")
	}
	if top, _ := st.TopLevel(ctx, 0, lib.ID); len(top) != 1 {
		t.Fatal("items gone when the plugin was turned off")
	}

	// Removing it forgets everything.
	st.SetPluginValue(ctx, "testkit", 1, "save", []byte("x"))
	if err := m.Remove(ctx, "testkit"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(m.Dir, "testkit")); !os.IsNotExist(err) {
		t.Fatal("folder left behind")
	}
	if _, err := st.PluginValue(ctx, "testkit", 1, "save"); err != store.ErrNotFound {
		t.Fatalf("storage left behind: %v", err)
	}
}

// An update keeps a plugin on, unless it asks to reach more of the
// internet; and the state survives a restart.
func TestUpdatesAndRestart(t *testing.T) {
	st := storetest.Open(t)
	ctx := context.Background()
	dir := t.TempDir()
	m, err := Open(ctx, dir, st, quietLog())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Install(ctx, testkitPackage(t, "")); err != nil {
		t.Fatal(err)
	}
	if _, err := m.SetEnabled(ctx, "testkit", true); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile("testdata/testkit/plugin.json")
	v2 := strings.Replace(string(b), `"version": "1.0.0"`, `"version": "1.1.0"`, 1)
	v, err := m.Install(ctx, testkitPackage(t, v2))
	if err != nil || !v.Running || v.Version != "1.1.0" {
		t.Fatalf("update: %+v %v", v, err)
	}
	m.Close(ctx)

	m, err = Open(ctx, dir, st, quietLog())
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close(ctx)
	if v, _ := m.Get("testkit"); !v.Running {
		t.Fatalf("not running after a restart: %+v", v)
	}
	v3 := strings.Replace(v2, `"example.test"`, `"example.test", "more.example.com"`, 1)
	v, err = m.Install(ctx, testkitPackage(t, v3))
	if err != nil || v.Running || !v.Enabled || !strings.Contains(v.Problem, "more.example.com") {
		t.Fatalf("an update asking for more should wait for the admin: %+v %v", v, err)
	}
	if v, _ = m.SetEnabled(ctx, "testkit", true); !v.Running {
		t.Fatalf("turning it on again agrees: %+v", v)
	}
	// A plugin made for another API version is listed but stays off.
	v4 := strings.Replace(v3, `"api": 1`, `"api": 99`, 1)
	if v, err = m.Install(ctx, testkitPackage(t, v4)); err != nil || v.Running || v.Supported {
		t.Fatalf("unsupported api: %+v %v", v, err)
	}
	if _, err := m.SetEnabled(ctx, "testkit", true); err != ErrNotSupported {
		t.Fatalf("enabled an unsupported plugin: %v", err)
	}
}

// Plugins fetch only from the hosts they list, over https.
func TestFetchPermissions(t *testing.T) {
	m, st := openManager(t)
	ctx := context.Background()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/elsewhere" {
			http.Redirect(w, r, "https://other.test/", http.StatusFound)
			return
		}
		io.WriteString(w, "fetched "+r.Host)
	}))
	defer srv.Close()
	// Every host name leads to the test server.
	m.rt.client.Transport = &http.Transport{
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, srv.Listener.Addr().String())
		},
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	if _, err := m.Install(ctx, testkitPackage(t, "")); err != nil {
		t.Fatal(err)
	}
	if _, err := m.SetEnabled(ctx, "testkit", true); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	write(t, filepath.Join(root, "ok.game"), `{"title":"OK","fetch":"https://example.test/hi"}`)
	write(t, filepath.Join(root, "other.game"), `{"title":"Other","fetch":"https://other.test/hi"}`)
	write(t, filepath.Join(root, "plain.game"), `{"title":"Plain","fetch":"http://example.test/hi"}`)
	write(t, filepath.Join(root, "redirect.game"), `{"title":"Redirect","fetch":"https://example.test/elsewhere"}`)
	lib, _ := st.CreateLibrary(ctx, "Games", "games", []string{root})
	s := &scan.Scanner{Store: st, Log: quietLog()}
	stats, err := s.Scan(ctx, lib)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Added != 1 || stats.Failed != 3 {
		t.Fatalf("stats: %+v", stats)
	}
	top, _ := st.TopLevel(ctx, 0, lib.ID)
	if len(top) != 1 {
		t.Fatalf("items: %+v", top)
	}
	it, _ := st.Item(ctx, 0, top[0].ID)
	if len(it.Facts) != 2 || it.Facts[1].Value != "fetched example.test" {
		t.Fatalf("facts: %+v", it.Facts)
	}
}

func TestGuestPath(t *testing.T) {
	if got := guestPath("/media/games/"); got != "/media/games" {
		t.Fatalf("got %q", got)
	}
}
