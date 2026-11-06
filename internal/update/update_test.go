package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestNewer(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"v0.2.3", "v0.2.2", true},
		{"0.10.0", "v0.9.9", true},
		{"v0.2.2", "v0.2.2", false},
		{"v0.2.1", "v0.2.2", false},
		{"v1.0.0", "v1.0.0-beta.2", true},
		{"v1.0.0-beta.2", "v1.0.0-beta.1", true},
		{"v1.0.0-beta.10", "v1.0.0-beta.9", true},
		{"v1.0.0-rc.1", "v1.0.0-beta.9", true},
		{"v1.0.0-beta.1", "v1.0.0", false},
		{"main-abc1234", "v0.1.0", false},
		{"v0.1.0", "dev", false},
	} {
		if got := Newer(c.a, c.b); got != c.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
	if Display("main-5f52f64aa0b1c2d3e4f5a6b7c8d9e0f1a2b3c4d5") != "main-5f52f64" || Display("0.2.2") != "v0.2.2" || Display("dev") != "dev" {
		t.Error("Display")
	}
}

type fakeSys struct {
	env   map[string]string
	exe   string
	files map[string]bool
	ro    bool
	goos  string
}

func (f fakeSys) sys() System {
	return System{
		Getenv:     func(k string) string { return f.env[k] },
		Executable: func() (string, error) { return f.exe, nil },
		Exists:     func(p string) bool { return f.files[p] },
		Writable:   func(string) bool { return !f.ro },
		GOOS:       cmpStr(f.goos, "linux"),
	}
}

func cmpStr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func TestDetect(t *testing.T) {
	for _, c := range []struct {
		name     string
		sys      fakeSys
		version  string
		kind     Kind
		canApply bool
		why      string
		beta     bool
	}{
		{"binary", fakeSys{exe: "/opt/matinee/matinee"}, "0.2.2", KindBinary, true, "", false},
		{"binary read-only", fakeSys{exe: "/usr/local/bin/matinee", ro: true}, "0.2.2", KindBinary, false, "read-only", false},
		{"binary on windows", fakeSys{exe: `C:\matinee\matinee.exe`, goos: "windows"}, "0.2.2", KindBinary, false, "windows", false},
		{"dev build", fakeSys{exe: "/home/me/matinee"}, "dev", KindBinary, false, "dev", false},
		{"docker", fakeSys{exe: "/usr/local/bin/matinee", files: map[string]bool{"/.dockerenv": true}}, "main-abc1234", KindDocker, false, "", true},
		{"kubernetes", fakeSys{exe: "/usr/local/bin/matinee", env: map[string]string{"KUBERNETES_SERVICE_HOST": "10.0.0.1"}, files: map[string]bool{"/.dockerenv": true}}, "0.2.2", KindKubernetes, false, "", true},
		{"argo", fakeSys{exe: "/usr/local/bin/matinee", env: map[string]string{"MATINEE_INSTALL": "kubernetes", "MATINEE_UPDATES_MANAGED_BY": "ArgoCD"}}, "main-abc1234", KindKubernetes, false, "managed", true},
		{"deb with helper", fakeSys{exe: "/usr/bin/matinee", env: map[string]string{"MATINEE_INSTALL": "package"}, files: map[string]bool{"/var/lib/dpkg/info/matinee.list": true, "/usr/lib/systemd/system/matinee-update.path": true}}, "0.2.2", KindDeb, true, "", false},
		{"rpm without helper", fakeSys{exe: "/usr/bin/matinee", files: map[string]bool{"/usr/bin/rpm": true}}, "0.2.2", KindRPM, false, "no-helper", false},
		{"homebrew", fakeSys{exe: "/opt/homebrew/Cellar/matinee/0.2.2/bin/matinee", files: map[string]bool{"/opt/homebrew/bin/brew": true}}, "0.2.2", KindHomebrew, true, "", false},
		{"mac app", fakeSys{exe: "/Applications/Matinee Server.app/Contents/MacOS/matinee"}, "0.2.2", KindMacApp, false, "", false},
		{"windows app", fakeSys{exe: `C:\Program Files\Matinee\matinee.exe`, goos: "windows", env: map[string]string{"MATINEE_STOP_ON_STDIN_CLOSE": "1"}}, "0.2.2", KindWindowsApp, false, "", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			in := Detect(c.sys.sys(), c.version)
			if in.Kind != c.kind || in.CanApply != c.canApply || in.Why != c.why || in.Beta != c.beta {
				t.Fatalf("got %+v", in)
			}
		})
	}
}

// github fakes the parts of GitHub's API and downloads Matinee uses.
func github(t *testing.T, files map[string][]byte) *Checker {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/yeixio/matinee/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"tag_name":"v0.3.0","html_url":"https://github.com/yeixio/matinee/releases/tag/v0.3.0","body":"Plays everything well","published_at":"2026-11-10T12:00:00Z"}`)
	})
	mux.HandleFunc("GET /repos/yeixio/matinee/branches/edge", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"commit":{"sha":"eeeeeee1111111","commit":{"committer":{"date":"2026-11-11T08:00:00Z"}}}}`)
	})
	mux.HandleFunc("GET /repos/yeixio/matinee/compare/{spec}", func(w http.ResponseWriter, r *http.Request) {
		base, head, _ := strings.Cut(r.PathValue("spec"), "...")
		switch {
		case base == "aaaaaaa" && head == "v0.3.0": // an older main build
			fmt.Fprint(w, `{"status":"ahead","ahead_by":4}`)
		case base == "bbbbbbb" && head == "v0.3.0": // a main build after the release
			fmt.Fprint(w, `{"status":"behind","ahead_by":0}`)
		case head == "eeeeeee1111111":
			fmt.Fprint(w, `{"status":"ahead","ahead_by":2,"html_url":"https://github.com/yeixio/matinee/compare/x...y","commits":[
				{"sha":"1111111aaaa","html_url":"u1","commit":{"message":"Add a thing (#1)\n\nmore"}},
				{"sha":"2222222bbbb","html_url":"u2","commit":{"message":"Fix a thing (#2)"}}]}`)
		default:
			http.NotFound(w, r)
		}
	})
	mux.HandleFunc("GET /yeixio/matinee/releases/download/v0.3.0/{name}", func(w http.ResponseWriter, r *http.Request) {
		b, ok := files[r.PathValue("name")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write(b)
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	c := NewChecker("v0.2.2")
	c.API, c.Web, c.Client = ts.URL, ts.URL, ts.Client()
	return c
}

func TestCheck(t *testing.T) {
	c := github(t, nil)
	ctx := context.Background()

	latest, ahead, err := c.Check(ctx, "v0.2.2", ChannelStable)
	if err != nil || ahead || latest == nil || latest.Version != "v0.3.0" || latest.Notes != "Plays everything well" {
		t.Fatalf("stable from an older release: %+v %v %v", latest, ahead, err)
	}
	if latest, ahead, err = c.Check(ctx, "0.3.0", ChannelStable); err != nil || latest != nil || ahead {
		t.Fatalf("stable, up to date: %+v %v %v", latest, ahead, err)
	}
	if latest, ahead, err = c.Check(ctx, "v0.3.1-beta.1", ChannelStable); err != nil || latest != nil || !ahead {
		t.Fatalf("stable from a newer pre-release: %+v %v %v", latest, ahead, err)
	}
	if latest, _, err = c.Check(ctx, "main-aaaaaaa", ChannelStable); err != nil || latest == nil || latest.Version != "v0.3.0" {
		t.Fatalf("stable from an older main build: %+v %v", latest, err)
	}
	if latest, ahead, err = c.Check(ctx, "main-bbbbbbb", ChannelStable); err != nil || latest != nil || !ahead {
		t.Fatalf("stable from a newer main build: %+v %v %v", latest, ahead, err)
	}

	latest, _, err = c.Check(ctx, "main-ccccccc", ChannelBeta)
	if err != nil || latest == nil || latest.Version != "main-eeeeeee" || len(latest.Commits) != 2 || latest.Commits[0].Title != "Fix a thing (#2)" || latest.Commits[1].SHA != "1111111" {
		t.Fatalf("beta: %+v %v", latest, err)
	}
	if latest, _, err = c.Check(ctx, "main-eeeeeee", ChannelBeta); err != nil || latest != nil {
		t.Fatalf("beta, up to date: %+v %v", latest, err)
	}
	if _, _, err = c.Check(ctx, "dev", ChannelStable); err != ErrUnknownVersion {
		t.Fatalf("dev build: %v", err)
	}
}

func tarGz(t *testing.T, name string, body []byte) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	tw.WriteHeader(&tar.Header{Name: "README.md", Mode: 0o644, Size: 2, Typeflag: tar.TypeReg})
	tw.Write([]byte("hi"))
	tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg})
	tw.Write(body)
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

func TestInstallBinary(t *testing.T) {
	name := archiveName("0.3.0", runtime.GOOS, runtime.GOARCH, goarm())
	archive := tarGz(t, strings.TrimSuffix(name, ".tar.gz")+"/matinee", []byte("new server"))
	sum := sha256.Sum256(archive)
	files := map[string][]byte{name: archive, "SHA256SUMS.txt": []byte(hex.EncodeToString(sum[:]) + "  " + name + "\n")}
	c := github(t, files)

	exe := filepath.Join(t.TempDir(), "matinee")
	os.WriteFile(exe, []byte("old server"), 0o750)
	if err := c.installBinary(context.Background(), "v0.3.0", exe); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(exe)
	st, _ := os.Stat(exe)
	if string(b) != "new server" || st.Mode().Perm() != 0o750 {
		t.Fatalf("installed %q mode %v", b, st.Mode())
	}

	// A swapped download is refused and the server is left alone.
	files[name] = tarGz(t, "x/matinee", []byte("evil"))
	os.WriteFile(exe, []byte("old server"), 0o755)
	if err := c.installBinary(context.Background(), "v0.3.0", exe); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("swapped download: %v", err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "old server" {
		t.Fatalf("server replaced with %q", b)
	}
}

func TestPackageResult(t *testing.T) {
	if r := packageResult("started\nresult: ok\n"); r != "ok" {
		t.Fatal(r)
	}
	if r := packageResult("started\nReading package lists...\n"); r != "" {
		t.Fatal(r)
	}
}

func TestRequestPackage(t *testing.T) {
	dir := t.TempDir()
	req, logPath := filepath.Join(dir, "update", "request"), filepath.Join(dir, "update.log")
	go func() {
		for {
			if _, err := os.Stat(req); err == nil {
				os.Remove(req)
				time.Sleep(10 * time.Millisecond)
				os.WriteFile(logPath, []byte("matinee is already the newest version\nresult: unchanged\n"), 0o644)
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := requestPackage(ctx, req, logPath); err == nil || !strings.Contains(err.Error(), "no newer") {
		t.Fatalf("got %v", err)
	}
}

func TestSteps(t *testing.T) {
	stable := &Release{Version: "v0.3.0", URL: "https://github.com/yeixio/matinee/releases/tag/v0.3.0"}
	beta := &Release{Version: "main-eeeeeee", commit: "eeeeeee1111111"}
	for _, c := range []struct {
		in      Install
		channel string
		rel     *Release
		want    string
	}{
		{Install{Kind: KindDocker}, ChannelStable, stable, "docker pull ghcr.io/yeixio/matinee:0.3.0"},
		{Install{Kind: KindDocker}, ChannelBeta, beta, "docker pull ghcr.io/yeixio/matinee:main"},
		{Install{Kind: KindKubernetes, Release: "media", Namespace: "home"}, ChannelStable, stable, "helm upgrade media oci://ghcr.io/yeixio/charts/matinee -n home --reuse-values --version 0.3.0"},
		{Install{Kind: KindKubernetes}, ChannelBeta, beta, "helm upgrade matinee oci://ghcr.io/yeixio/charts/matinee --reuse-values --set image.tag=sha-eeeeeee"},
		{Install{Kind: KindKubernetes, ManagedBy: "argocd"}, ChannelBeta, beta, "image:\n  tag: sha-eeeeeee"},
		{Install{Kind: KindDeb}, ChannelStable, stable, "sudo apt update && sudo apt install --only-upgrade matinee"},
		{Install{Kind: KindRPM}, ChannelStable, stable, "sudo dnf upgrade --refresh matinee"},
		{Install{Kind: KindHomebrew}, ChannelStable, stable, "brew upgrade matinee"},
		{Install{Kind: KindBinary}, ChannelStable, stable, stable.URL},
	} {
		if got := Steps(c.in, c.channel, c.rel); len(got) == 0 || got[0] != c.want {
			t.Errorf("%s %s: %q", c.in.Kind, c.channel, got)
		}
	}
	if Steps(Install{Kind: KindMacApp}, ChannelStable, stable) != nil {
		t.Error("the Mac app updates itself")
	}
}

type memStore map[string]string

func (m memStore) Setting(_ context.Context, k string) (string, error) { return m[k], nil }
func (m memStore) SetSetting(_ context.Context, k, v string) error     { m[k] = v; return nil }

func TestManager(t *testing.T) {
	name := archiveName("0.3.0", runtime.GOOS, runtime.GOARCH, goarm())
	archive := tarGz(t, "x/matinee", []byte("new server"))
	sum := sha256.Sum256(archive)
	c := github(t, map[string][]byte{name: archive, "SHA256SUMS.txt": []byte(hex.EncodeToString(sum[:]) + "  " + name + "\n")})
	exe := filepath.Join(t.TempDir(), "matinee")
	os.WriteFile(exe, []byte("old server"), 0o755)
	restarted := make(chan string, 1)
	m := &Manager{
		Store: memStore{}, Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Version: "v0.2.2",
		Install: Install{Kind: KindBinary, CanApply: true, exe: exe}, Checker: c,
		Restart: func(exe string) { restarted <- exe },
	}
	ctx := context.Background()
	if s := m.Settings(ctx); s != DefaultSettings {
		t.Fatalf("defaults %+v", s)
	}
	if err := m.SaveSettings(ctx, Settings{Channel: ChannelBeta, Mode: ModeInstall}); err != ErrBadSettings {
		t.Fatalf("beta isn't offered for binaries: %v", err)
	}
	if err := m.Apply(); err != ErrNothingNew {
		t.Fatalf("apply before a check: %v", err)
	}
	if err := m.Check(ctx); err != nil {
		t.Fatal(err)
	}
	st := m.Status(ctx)
	if st.Current != "v0.2.2" || st.Latest == nil || st.Latest.Version != "v0.3.0" || len(st.Steps) != 1 {
		t.Fatalf("status %+v", st)
	}
	if err := m.Apply(); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-restarted:
		if got != exe {
			t.Fatalf("restarted %q", got)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("no restart: %+v", m.Status(ctx).Apply)
	}
	if b, _ := os.ReadFile(exe); string(b) != "new server" {
		t.Fatalf("installed %q", b)
	}
	if st := m.Status(ctx); st.Apply.State != StateRestarting || st.Apply.Version != "v0.3.0" {
		t.Fatalf("apply %+v", st.Apply)
	}
}
