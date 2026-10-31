package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestMain lets the test binary stand in for the matinee server: it answers
// /api/v1/health and, like the real one, stops when its stdin closes.
func TestMain(m *testing.M) {
	switch os.Getenv("FAKE_MATINEE") {
	case "serve":
		if os.Getenv("MATINEE_STOP_ON_STDIN_CLOSE") != "1" {
			os.Exit(3)
		}
		ln, err := net.Listen("tcp", os.Getenv("FAKE_ADDR"))
		if err != nil {
			os.Exit(4)
		}
		go http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"status":"ok","version":"0.1.8","state":"running"}`)
		}))
		io.Copy(io.Discard, os.Stdin)
		fmt.Println("stopped cleanly")
		os.Exit(0)
	case "crash":
		os.Exit(2)
	}
	os.Exit(m.Run())
}

// syncBuffer is a log both processes write to.
type syncBuffer struct {
	mu sync.Mutex
	b  strings.Builder
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func freeAddr(t *testing.T) string {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().String()
}

func waitFor(t *testing.T, s *Server, want State) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if st, _ := s.State(context.Background()); st == want {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	st, _ := s.State(context.Background())
	t.Fatalf("server state %d, want %d", st, want)
}

func TestServerStartsAndStopsByClosingStdin(t *testing.T) {
	exe, _ := os.Executable()
	addr := freeAddr(t)
	log := &syncBuffer{}
	s := &Server{Exe: exe, Env: []string{"FAKE_MATINEE=serve", "FAKE_ADDR=" + addr}, Log: log, HealthURL: "http://" + addr + "/api/v1/health"}
	if st, _ := s.State(context.Background()); st != Stopped {
		t.Fatalf("before starting: %d", st)
	}
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, s, Running)
	if _, ver := s.State(context.Background()); ver != "0.1.8" {
		t.Fatalf("version %q", ver)
	}
	s.Stop(5 * time.Second)
	if st, _ := s.State(context.Background()); st != Stopped {
		t.Fatalf("after stopping: %d", st)
	}
	if !strings.Contains(log.String(), "stopped cleanly") {
		t.Fatalf("the server was killed rather than stopped: %q", log.String())
	}
	if s.LastError() != nil {
		t.Fatalf("stopping on purpose is no error: %v", s.LastError())
	}
}

func TestServerThatKeepsCrashingStaysStopped(t *testing.T) {
	exe, _ := os.Executable()
	log := &syncBuffer{}
	s := &Server{Exe: exe, Env: []string{"FAKE_MATINEE=crash"}, Log: log, HealthURL: "http://127.0.0.1:1/", RestartDelay: 10 * time.Millisecond}
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, s, Failed)
	if n := strings.Count(log.String(), "starting it again"); n != crashLimit-1 {
		t.Fatalf("restarted %d times, want %d: %s", n, crashLimit-1, log.String())
	}
	if s.LastError() == nil {
		t.Fatal("no error after crashing")
	}
	// Start tries again from scratch.
	s.Start()
	waitFor(t, s, Failed)
}

func TestNewer(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"0.1.8", "0.1.7", true},
		{"v0.2.0", "0.1.9", true},
		{"0.1.10", "0.1.9", true},
		{"0.1.7", "0.1.7", false},
		{"0.1.6", "0.1.7", false},
		{"1.0.0", "dev", false},
		{"1.0.0", "0.1.7-3-gabc", false},
		{"0.2.0-beta.1", "0.1.7", false},
	} {
		if got := Newer(c.a, c.b); got != c.want {
			t.Errorf("Newer(%q, %q) = %v", c.a, c.b, got)
		}
	}
}

func TestUpdate(t *testing.T) {
	installer := []byte("pretend installer")
	sum := sha256.Sum256(installer)
	sums := hex.EncodeToString(sum[:]) + "  Matinee-Setup-arm64.exe\n" + strings.Repeat("0", 64) + "  matinee_0.1.8_linux_amd64.tar.gz\n"
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	mux.HandleFunc("/latest", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"tag_name":"v0.1.8","html_url":"https://example.com/r","assets":[
			{"name":"Matinee-Setup-x64.exe","browser_download_url":"%[1]s/x64"},
			{"name":"Matinee-Setup-arm64.exe","browser_download_url":"%[1]s/arm64"},
			{"name":"SHA256SUMS.txt","browser_download_url":"%[1]s/sums"}]}`, srv.URL)
	})
	mux.HandleFunc("/arm64", func(w http.ResponseWriter, r *http.Request) { w.Write(installer) })
	mux.HandleFunc("/x64", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("not what the sums say")) })
	mux.HandleFunc("/sums", func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, sums) })
	ctx := context.Background()

	if _, ok, err := LatestRelease(ctx, srv.Client(), srv.URL+"/latest", "0.1.8", "arm64"); err != nil || ok {
		t.Fatalf("the same version is no update: %v %v", ok, err)
	}
	rel, ok, err := LatestRelease(ctx, srv.Client(), srv.URL+"/latest", "0.1.7", "arm64")
	if err != nil || !ok || rel.Version != "0.1.8" || rel.InstallerURL != srv.URL+"/arm64" {
		t.Fatalf("update: %+v %v %v", rel, ok, err)
	}
	dir := t.TempDir()
	path, err := Download(ctx, srv.Client(), rel, dir)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); string(b) != string(installer) {
		t.Fatalf("downloaded %q", b)
	}

	// A download that doesn't match its checksum is thrown away.
	x64, _, _ := LatestRelease(ctx, srv.Client(), srv.URL+"/latest", "0.1.7", "x64")
	x64.Installer = "Matinee-Setup-arm64.exe" // checked against arm64's sum
	if _, err := Download(ctx, srv.Client(), x64, dir); err == nil {
		t.Fatal("a mismatched installer was kept")
	}
	if exists(path) {
		t.Fatal("the mismatched download was left behind")
	}
}

func TestSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "matinee.env")
	os.WriteFile(path, []byte("# comment\n\nMATINEE_ADDR=:8080\nMATINEE_SERVER_NAME=\"Den PC\"\nnot a setting\n"), 0o644)
	env := readEnvFile(path)
	if len(env) != 2 || lookupEnv(env, "MATINEE_SERVER_NAME") != "Den PC" {
		t.Fatalf("env %q", env)
	}
	if u := localURL(lookupEnv(env, "MATINEE_ADDR")); u != "http://localhost:8080" {
		t.Fatalf("url %q", u)
	}
	if u := localURL(""); u != "http://localhost:7171" {
		t.Fatalf("default url %q", u)
	}
	if u := localURL("192.168.1.5:7171"); u != "http://192.168.1.5:7171" {
		t.Fatalf("url %q", u)
	}
	if readEnvFile(filepath.Join(t.TempDir(), "missing")) != nil {
		t.Fatal("a missing file has settings")
	}
}
