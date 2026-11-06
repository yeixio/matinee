package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/yeixio/matinee/internal/kube"
)

func TestParseBuild(t *testing.T) {
	for v, want := range map[string]Build{
		"v0.2.2":        {Version: "v0.2.2", Release: "0.2.2"},
		"0.2.2":         {Version: "0.2.2", Release: "0.2.2"},
		"v0.3.0-beta.1": {Version: "v0.3.0-beta.1", Release: "0.3.0-beta.1"},
		"main-fc7623c5ad4ee2bbb3c4b7f87947bda888be77f5": {Version: "main-fc7623c5ad4ee2bbb3c4b7f87947bda888be77f5", Commit: "fc7623c5ad4ee2bbb3c4b7f87947bda888be77f5"},
		"dev":                     {Version: "dev"},
		"v0.2.2-3-gabc1234":       {Version: "v0.2.2-3-gabc1234"},
		"v0.2.2-3-gabc1234-dirty": {Version: "v0.2.2-3-gabc1234-dirty"},
		"fc7623c":                 {Version: "fc7623c"},
	} {
		if got := ParseBuild(v); got != want {
			t.Errorf("ParseBuild(%q) = %+v, want %+v", v, got, want)
		}
	}
	b := ParseBuild("main-fc7623c5ad4ee2bbb3c4b7f87947bda888be77f5")
	if b.ImageTag() != "sha-fc7623c" || b.Revision() != "fc7623c5ad4ee2bbb3c4b7f87947bda888be77f5" || b.Channel() != ChannelMain {
		t.Errorf("main build: %s %s %s", b.ImageTag(), b.Revision(), b.Channel())
	}
	r := ParseBuild("v1.2.3")
	if r.ImageTag() != "1.2.3" || r.Revision() != "v1.2.3" || r.Channel() != ChannelRelease {
		t.Errorf("release build: %s %s %s", r.ImageTag(), r.Revision(), r.Channel())
	}
}

func TestNewer(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"v0.2.3", "0.2.2", true},
		{"0.2.2", "0.2.2", false},
		{"0.2.1", "0.2.2", false},
		{"1.0.0", "0.99.99", true},
		{"0.3.0", "0.3.0-beta.1", true},
		{"0.3.0-beta.1", "0.3.0", false},
		{"0.3.0-beta.2", "0.3.0-beta.1", true},
		{"0.3.0-beta.10", "0.3.0-beta.9", true},
		{"0.3.0-rc.1", "0.3.0-beta.1", true},
		{"dev", "0.1.0", false},
	} {
		if got := Newer(c.a, c.b); got != c.want {
			t.Errorf("Newer(%s, %s) = %v", c.a, c.b, got)
		}
	}
}

func env(vars map[string]string, files []string, exe string) Env {
	return Env{
		Getenv:     func(k string) string { return vars[k] },
		Exists:     func(p string) bool { return slices.Contains(files, p) },
		Executable: exe,
		GOOS:       "linux",
		Writable:   func(string) bool { return true },
	}
}

func TestDetect(t *testing.T) {
	rel := ParseBuild("v0.2.2")
	kube := map[string]string{"KUBERNETES_SERVICE_HOST": "10.0.0.1", "MATINEE_INSTALL": "docker"}
	argo := map[string]string{"KUBERNETES_SERVICE_HOST": "10.0.0.1", "MATINEE_INSTALL": "helm", "MATINEE_ARGOCD_APPLICATION": "argocd/matinee"}
	for _, c := range []struct {
		name     string
		b        Build
		env      Env
		kind     string
		canApply bool
		main     bool
	}{
		{"argo", rel, env(argo, nil, "/usr/local/bin/matinee"), KindArgoCD, true, true},
		{"chart", rel, env(kube, nil, "/usr/local/bin/matinee"), KindKubernetes, false, true},
		{"image", rel, env(map[string]string{"MATINEE_INSTALL": "docker"}, nil, "/usr/local/bin/matinee"), KindDocker, false, true},
		{"other container", rel, env(nil, []string{"/run/.containerenv"}, "/usr/local/bin/matinee"), KindDocker, false, true},
		{"deb", rel, env(map[string]string{"MATINEE_INSTALL": "package"}, []string{"/etc/debian_version"}, "/usr/bin/matinee"), KindApt, false, false},
		{"rpm", rel, env(nil, nil, "/usr/bin/matinee"), KindDnf, false, false},
		{"brew", rel, env(nil, nil, "/opt/homebrew/Cellar/matinee/0.2.2/bin/matinee"), KindHomebrew, false, false},
		{"brew it can run", rel, env(nil, []string{"/opt/homebrew/bin/brew"}, "/opt/homebrew/Cellar/matinee/0.2.2/bin/matinee"), KindHomebrew, true, false},
		{"deb with its helper", rel, env(map[string]string{"MATINEE_INSTALL": "package"}, []string{"/etc/debian_version", "/usr/lib/systemd/system/matinee-update.path", packageHelper}, "/usr/bin/matinee"), KindApt, true, false},
		{"rpm with its helper", rel, env(nil, []string{"/lib/systemd/system/matinee-update.path", packageHelper}, "/usr/bin/matinee"), KindDnf, true, false},
		{"deb with its helper turned off", rel, env(map[string]string{"MATINEE_INSTALL": "package"}, []string{"/etc/debian_version", "/usr/lib/systemd/system/matinee-update.path"}, "/usr/bin/matinee"), KindApt, false, false},
		{"mac app", rel, env(nil, nil, "/Applications/Matinee Server.app/Contents/MacOS/matinee"), KindApp, false, false},
		{"windows app", rel, env(map[string]string{"MATINEE_STOP_ON_STDIN_CLOSE": "1"}, nil, `C:\Program Files\Matinee\matinee.exe`), KindApp, false, false},
		{"binary", rel, env(nil, nil, "/opt/matinee/matinee"), KindBinary, true, false},
		{"from source", ParseBuild("dev"), env(nil, nil, "/home/me/matinee/bin/matinee"), KindSource, false, false},
	} {
		in := Detect(c.b, c.env)
		if in.Kind != c.kind || in.CanApply != c.canApply || slices.Contains(in.Channels, ChannelMain) != c.main {
			t.Errorf("%s: got %+v", c.name, in)
		}
	}
	read := env(nil, nil, "/opt/matinee/matinee")
	read.Writable = func(string) bool { return false }
	if Detect(rel, read).CanApply {
		t.Error("a binary in a folder it can't write to can't update itself")
	}
	if ns, name := ArgoApplication("matinee"); ns != "argocd" || name != "matinee" {
		t.Errorf("ArgoApplication: %s/%s", ns, name)
	}
}

// fakeGitHub answers like GitHub's API for a repository whose edge is at
// edgeSHA, whose latest release is tag, and where compare answers from ahead.
type fakeGitHub struct {
	tag     string
	edgeSHA string
	ahead   map[string]int // "base...head" → commits head has that base doesn't
	assets  []asset
	calls   int
}

func (f *fakeGitHub) server(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.calls++
		switch {
		case r.URL.Path == "/repos/x/m/releases/latest":
			json.NewEncoder(w).Encode(release{Tag: f.tag, URL: "https://github.com/x/m/releases/tag/" + f.tag, Assets: f.assets})
		case r.URL.Path == "/repos/x/m/branches/edge":
			fmt.Fprintf(w, `{"commit":{"sha":%q,"commit":{"committer":{"date":"2026-11-05T12:00:00Z"}}}}`, f.edgeSHA)
		case strings.HasPrefix(r.URL.Path, "/repos/x/m/compare/"):
			n, ok := f.ahead[strings.TrimPrefix(r.URL.Path, "/repos/x/m/compare/")]
			status := "behind"
			if ok {
				status = "ahead"
			}
			fmt.Fprintf(w, `{"status":%q,"ahead_by":%d}`, status, n)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

const (
	shaOld  = "1111111aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	shaEdge = "2222222bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func TestFind(t *testing.T) {
	f := &fakeGitHub{tag: "v0.2.3", edgeSHA: shaEdge, ahead: map[string]int{
		shaOld + "..." + shaEdge: 4,
		"v0.2.3..." + shaEdge:    2,
	}}
	g := &GitHub{Base: f.server(t).URL + "/repos/x/m"}
	ctx := context.Background()

	c, err := g.Find(ctx, ParseBuild("v0.2.2"), ChannelRelease)
	if err != nil || c == nil || c.Version != "v0.2.3" || c.ImageTag != "0.2.3" || c.Revision != "v0.2.3" {
		t.Fatalf("release after release: %+v %v", c, err)
	}
	if c, _ := g.Find(ctx, ParseBuild("v0.2.3"), ChannelRelease); c != nil {
		t.Errorf("the latest release was offered to itself: %+v", c)
	}
	// A build of main newer than the release isn't offered the release.
	if c, _ := g.Find(ctx, ParseBuild("main-"+shaEdge), ChannelRelease); c != nil {
		t.Errorf("a main build was offered an older release: %+v", c)
	}
	// One the release has everything of is.
	f.ahead["1111111aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa...v0.2.3"] = 9
	if c, _ := g.Find(ctx, ParseBuild("main-"+shaOld), ChannelRelease); c == nil || c.Version != "v0.2.3" {
		t.Errorf("an older main build wasn't offered the release: %+v", c)
	}

	c, err = g.Find(ctx, ParseBuild("main-"+shaOld), ChannelMain)
	if err != nil || c == nil || c.Version != "main-2222222" || c.ImageTag != "sha-2222222" || c.Commits != 4 || c.Revision != shaEdge {
		t.Fatalf("main after main: %+v %v", c, err)
	}
	if !strings.Contains(c.URL, "/compare/"+shaOld+"...") {
		t.Errorf("main's link: %s", c.URL)
	}
	if c, _ := g.Find(ctx, ParseBuild("main-"+shaEdge), ChannelMain); c != nil {
		t.Errorf("edge was offered to itself: %+v", c)
	}
	if c, _ := g.Find(ctx, ParseBuild("v0.2.3"), ChannelMain); c == nil || c.Commits != 2 {
		t.Errorf("a release wasn't offered main: %+v", c)
	}
	calls := f.calls
	if c, err := g.Find(ctx, ParseBuild("dev"), ChannelRelease); c != nil || err != nil || f.calls != calls {
		t.Errorf("a dev build looked for updates")
	}
}

type memStore map[string]string

func (m memStore) Setting(_ context.Context, k string) (string, error) { return m[k], nil }
func (m memStore) SetSetting(_ context.Context, k, v string) error     { m[k] = v; return nil }

type fakeArgo struct {
	src  kube.Source
	sets []kube.Source
}

func (a *fakeArgo) Source(context.Context) (kube.Source, error) { return a.src, nil }
func (a *fakeArgo) SetSource(_ context.Context, s kube.Source) error {
	a.src = s
	a.sets = append(a.sets, s)
	return nil
}

func argoManager(t *testing.T, build string, src kube.Source) (*Manager, *fakeArgo, *fakeGitHub) {
	f := &fakeGitHub{tag: "v0.2.3", edgeSHA: shaEdge, ahead: map[string]int{shaOld + "..." + shaEdge: 4, shaOld + "...v0.2.3": 3}}
	argo := &fakeArgo{src: src}
	b := ParseBuild(build)
	m := &Manager{
		Store: memStore{}, Build: b, Argo: argo,
		Install: Install{Kind: KindArgoCD, CanApply: true, Channels: []string{ChannelRelease, ChannelMain}},
		GitHub:  &GitHub{Base: f.server(t).URL + "/repos/x/m"},
	}
	return m, argo, f
}

func TestArgoFollowingEdge(t *testing.T) {
	ctx := context.Background()
	m, argo, _ := argoManager(t, "main-"+shaOld, kube.Source{TargetRevision: "edge", ImageTag: followImageTag})
	// An Application that follows edge is main, automatic, until the admin chooses.
	if s := m.Settings(ctx); s != (Settings{Channel: ChannelMain, Auto: true}) {
		t.Fatalf("settings while following edge: %+v", s)
	}
	m.Check(ctx)
	st := m.Status(ctx)
	if !st.Following || st.Available != nil || len(argo.sets) != 0 {
		t.Fatalf("following edge, Matinee shouldn't install anything: %+v %+v", st, argo.sets)
	}

	// Installing by hand pins the Application to what's running.
	if _, err := m.SaveSettings(ctx, Settings{Channel: ChannelMain}); err != nil {
		t.Fatal(err)
	}
	want := kube.Source{TargetRevision: shaOld, ImageTag: "sha-1111111"}
	if argo.src != want {
		t.Fatalf("pinned to %+v, want %+v", argo.src, want)
	}
	st = m.Status(ctx)
	if st.Following || st.Available == nil || st.Available.Version != "main-2222222" {
		t.Fatalf("after pinning: %+v", st)
	}
	if err := m.Apply(ctx); err != nil {
		t.Fatal(err)
	}
	if argo.src != (kube.Source{TargetRevision: shaEdge, ImageTag: "sha-2222222"}) {
		t.Fatalf("applied %+v", argo.src)
	}
	if m.Status(ctx).Applying != "main-2222222" {
		t.Error("the update doesn't show as on its way")
	}

	// Automatic main follows edge again.
	m.SaveSettings(ctx, Settings{Channel: ChannelMain, Auto: true})
	if argo.src != (kube.Source{TargetRevision: "edge", ImageTag: followImageTag}) {
		t.Fatalf("automatic main: %+v", argo.src)
	}
}

func TestArgoReleaseAutomatic(t *testing.T) {
	ctx := context.Background()
	m, argo, _ := argoManager(t, "v0.2.2", kube.Source{TargetRevision: "v0.2.2", ImageTag: "0.2.2"})
	if s := m.Settings(ctx); s != DefaultSettings {
		t.Fatalf("default settings: %+v", s)
	}
	m.Check(ctx)
	if len(argo.sets) != 0 {
		t.Fatal("installed without being asked to")
	}
	m.SaveSettings(ctx, Settings{Channel: ChannelRelease, Auto: true})
	if argo.src != (kube.Source{TargetRevision: "v0.2.3", ImageTag: "0.2.3"}) {
		t.Fatalf("automatic release: %+v", argo.src)
	}
}

func TestArgoReleaseFromNewerMain(t *testing.T) {
	ctx := context.Background()
	// Running edge, newer than the latest release: switching to releases
	// stays on this build rather than going back to the release.
	m, argo, _ := argoManager(t, "main-"+shaEdge, kube.Source{TargetRevision: "edge", ImageTag: followImageTag})
	m.SaveSettings(ctx, Settings{Channel: ChannelRelease, Auto: true})
	if argo.src != (kube.Source{TargetRevision: shaEdge, ImageTag: "sha-2222222"}) || len(argo.sets) != 1 {
		t.Fatalf("got %+v after %d changes", argo.src, len(argo.sets))
	}
	if st := m.Status(ctx); st.Available != nil {
		t.Fatalf("offered %+v", st.Available)
	}
}

func TestCantApply(t *testing.T) {
	m := &Manager{Store: memStore{}, Build: ParseBuild("v0.2.2"), Install: Install{Kind: KindDocker, Channels: []string{ChannelRelease, ChannelMain}}}
	if err := m.Apply(context.Background()); err != ErrCantApply {
		t.Fatalf("Apply in a container: %v", err)
	}
	// A channel the install can't follow falls back to releases.
	m.Install = Install{Kind: KindApt, Channels: []string{ChannelRelease}}
	if s := m.normalize(Settings{Channel: ChannelMain, Auto: true}); s.Channel != ChannelRelease || !s.Auto {
		t.Fatalf("normalize: %+v", s)
	}
}

func tarGz(t *testing.T, name string, body []byte) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	tw.WriteHeader(&tar.Header{Name: "matinee_0.2.3_linux_amd64/README.md", Mode: 0o644, Size: 2, Typeflag: tar.TypeReg})
	tw.Write([]byte("hi"))
	tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg})
	tw.Write(body)
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

func TestBinaryUpdate(t *testing.T) {
	archive := tarGz(t, "matinee_0.2.3_linux_amd64/matinee", []byte("new matinee"))
	sum := sha256.Sum256(archive)
	sums := hex.EncodeToString(sum[:]) + "  matinee_0.2.3_linux_amd64.tar.gz\n"
	files := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/a.tar.gz":
			w.Write(archive)
		case "/sums":
			w.Write([]byte(sums))
		}
	}))
	defer files.Close()
	f := &fakeGitHub{tag: "v0.2.3", assets: []asset{
		{Name: "matinee_0.2.3_linux_amd64.tar.gz", URL: files.URL + "/a.tar.gz"},
		{Name: "SHA256SUMS.txt", URL: files.URL + "/sums"},
	}}
	dir := t.TempDir()
	exe := filepath.Join(dir, "matinee")
	os.WriteFile(exe, []byte("old matinee"), 0o755)

	ctx := context.Background()
	c, err := (&GitHub{Base: f.server(t).URL + "/repos/x/m"}).Find(ctx, ParseBuild("0.2.2"), ChannelRelease)
	if err != nil || c == nil {
		t.Fatal(c, err)
	}
	if err := replaceBinary(ctx, nil, c, exe, "linux", "amd64", ""); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "new matinee" {
		t.Fatalf("the binary is %q", b)
	}
	if fi, _ := os.Stat(exe); fi.Mode().Perm()&0o100 == 0 {
		t.Error("the new binary can't run")
	}

	// A download that doesn't match its checksum is never put in place.
	os.WriteFile(exe, []byte("old matinee"), 0o755)
	sums = strings.Repeat("0", 64) + "  matinee_0.2.3_linux_amd64.tar.gz\n"
	if err := replaceBinary(ctx, nil, c, exe, "linux", "amd64", ""); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("a bad download: %v", err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "old matinee" {
		t.Fatal("a bad download replaced the binary")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Errorf("left files behind: %v", entries)
	}
	// No archive for this computer.
	if err := replaceBinary(ctx, nil, c, exe, "freebsd", "amd64", ""); err == nil {
		t.Fatal("updated without an archive for this computer")
	}
}

func TestArchiveName(t *testing.T) {
	if n := archiveName("v1.2.3", "linux", "arm", "7"); n != "matinee_1.2.3_linux_armv7.tar.gz" {
		t.Error(n)
	}
	if n := archiveName("1.2.3", "darwin", "arm64", ""); n != "matinee_1.2.3_darwin_arm64.tar.gz" {
		t.Error(n)
	}
}

func TestApplyingExpires(t *testing.T) {
	ctx := context.Background()
	m, _, _ := argoManager(t, "v0.2.2", kube.Source{TargetRevision: "v0.2.2", ImageTag: "0.2.2"})
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	m.Now = func() time.Time { return now }
	m.Check(ctx)
	if err := m.Apply(ctx); err != nil {
		t.Fatal(err)
	}
	if m.Status(ctx).Applying == "" {
		t.Fatal("not applying")
	}
	now = now.Add(applyingFor + time.Minute)
	if a := m.Status(ctx).Applying; a != "" {
		t.Fatalf("still applying %s after the rollout should have finished", a)
	}
}

func TestAutomaticWaitsForNobodyWatching(t *testing.T) {
	ctx := context.Background()
	f := &fakeGitHub{tag: "v0.2.3"}
	watching := true
	m := &Manager{
		Store: memStore{}, Build: ParseBuild("v0.2.2"),
		Install:    Install{Kind: KindBinary, CanApply: true, Channels: []string{ChannelRelease}},
		GitHub:     &GitHub{Base: f.server(t).URL + "/repos/x/m"},
		Executable: filepath.Join(t.TempDir(), "matinee"),
		Busy:       func() bool { return watching },
	}
	if _, err := m.SaveSettings(ctx, Settings{Channel: ChannelRelease, Auto: true}); err != nil {
		t.Fatal(err)
	}
	st := m.Status(ctx)
	if !st.Waiting || st.Applying != "" || st.LastError != "" || st.Available == nil {
		t.Fatalf("someone is watching, so it should wait: %+v", st)
	}
	// Once nobody is, it installs (and here fails: the release has no archive).
	watching = false
	m.Check(ctx)
	if st := m.Status(ctx); st.Waiting || st.LastError == "" {
		t.Fatalf("nobody is watching, so it should have tried: %+v", st)
	}
}

func TestPackageHelper(t *testing.T) {
	if r := packageResult("upgrading\nresult: ok\n"); r != "ok" {
		t.Error(r)
	}
	if r := packageResult("upgrading\nReading package lists...\n"); r != "" {
		t.Error(r)
	}
	dir := t.TempDir()
	req, log := filepath.Join(dir, "update", "request"), filepath.Join(dir, "update.log")
	// The helper: removes the request, then finds nothing newer.
	go func() {
		for {
			if _, err := os.Stat(req); err == nil {
				os.Remove(req)
				time.Sleep(20 * time.Millisecond)
				os.WriteFile(log, []byte("matinee is already the newest version\nresult: unchanged\n"), 0o644)
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := requestPackage(ctx, req, log); err == nil || !strings.Contains(err.Error(), "doesn't have the new version") {
		t.Fatalf("got %v", err)
	}
}
