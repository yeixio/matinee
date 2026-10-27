package remote

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yeixio/matinee/internal/connect"
)

func TestPublic(t *testing.T) {
	for ip, want := range map[string]bool{
		"203.0.113.5": true, "2001:db8::1": true, "8.8.8.8": true,
		"192.168.1.1": false, "10.0.0.1": false, "100.64.1.2": false, "100.127.255.1": false, "127.0.0.1": false, "0.0.0.0": false,
	} {
		if got := public(netip.MustParseAddr(ip)); got != want {
			t.Errorf("public(%s) = %v, want %v", ip, got, want)
		}
	}
}

func TestURL(t *testing.T) {
	ip := netip.MustParseAddr("203.0.113.5")
	if got := url(ip, 443); got != "https://203.0.113.5" {
		t.Error(got)
	}
	if got := url(ip, 7443); got != "https://203.0.113.5:7443" {
		t.Error(got)
	}
}

// The remote port takes the CA's plain-HTTP check alongside HTTPS.
func TestMixedListener(t *testing.T) {
	ts := httptest.NewTLSServer(nil)
	defer ts.Close()
	cfg := &tls.Config{Certificates: ts.TLS.Certificates}

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ml := newMixedListener(l, cfg)
	go http.Serve(ml, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.TLS != nil {
			io.WriteString(w, "tls")
		} else {
			io.WriteString(w, "plain")
		}
	}))
	defer ml.Close()
	addr := l.Addr().String()

	// A client that connects and sends nothing mustn't hold up others.
	idle, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer idle.Close()

	get := func(c *http.Client, u string) string {
		t.Helper()
		resp, err := c.Get(u)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return string(b)
	}
	if got := get(http.DefaultClient, "http://"+addr+"/"); got != "plain" {
		t.Errorf("plain HTTP got %q", got)
	}
	if got := get(ts.Client(), "https://"+addr+"/"); got != "tls" {
		t.Errorf("HTTPS got %q", got)
	}
}

func TestCurrent(t *testing.T) {
	ip := netip.MustParseAddr("203.0.113.5")
	mk := func(addr string, left time.Duration) *tls.Certificate {
		return &tls.Certificate{Leaf: &x509.Certificate{
			IPAddresses: []net.IP{net.ParseIP(addr)},
			NotBefore:   time.Now().Add(left - 6*24*time.Hour), NotAfter: time.Now().Add(left),
		}}
	}
	if !current(mk("203.0.113.5", 5*24*time.Hour), ip) {
		t.Error("fresh certificate should be current")
	}
	if current(mk("203.0.113.5", 24*time.Hour), ip) {
		t.Error("a certificate with a day left should be renewed")
	}
	if current(mk("203.0.113.6", 5*24*time.Hour), ip) {
		t.Error("a certificate for the old address should be replaced")
	}
	if current(nil, ip) {
		t.Error("nil")
	}
}

// fakeRouter stands in for a home router on the loopback interface: a
// forward from ext to internal is a proxy from 127.0.0.1:ext.
type fakeRouter struct {
	ip      netip.Addr
	refuse  map[int]bool // external ports already taken
	mu      sync.Mutex
	proxies map[int]net.Listener
	maps    []int
}

func (r *fakeRouter) Name() string                                   { return "UPnP" }
func (r *fakeRouter) ExternalIP(context.Context) (netip.Addr, error) { return r.ip, nil }
func (r *fakeRouter) Map(_ context.Context, internal, ext int, _ time.Duration) (int, error) {
	if r.refuse[ext] {
		return 0, errors.New("conflict in mapping entry")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.maps = append(r.maps, ext)
	if ext == internal || r.proxies[ext] != nil {
		return ext, nil
	}
	l, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(ext))
	if err != nil {
		return 0, err
	}
	if r.proxies == nil {
		r.proxies = map[int]net.Listener{}
	}
	r.proxies[ext] = l
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				d, err := net.Dial("tcp", "127.0.0.1:"+strconv.Itoa(internal))
				if err != nil {
					return
				}
				defer d.Close()
				go io.Copy(d, c)
				io.Copy(c, d)
			}()
		}
	}()
	return ext, nil
}

func (r *fakeRouter) Unmap(_ context.Context, _, ext int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if l := r.proxies[ext]; l != nil {
		l.Close()
		delete(r.proxies, ext)
	}
	return nil
}

func TestSharedAddress(t *testing.T) {
	m := &Manager{Addr: "127.0.0.1:0", Dir: t.TempDir(), Log: slog.New(slog.DiscardHandler), Handler: http.NotFoundHandler(),
		FindRouter: func(context.Context) (router, error) { return &fakeRouter{ip: netip.MustParseAddr("100.70.1.2")}, nil }}
	m.SetEnabled(true)
	m.check(context.Background())
	defer m.stop(context.Background())
	st := m.Status()
	if st.State != StateFailed || !st.SharedAddress || st.PublicIP != "100.70.1.2" {
		t.Fatalf("status %+v", st)
	}
	if m.URL() != "" {
		t.Error("no URL without a public address")
	}
}

func TestNoRouter(t *testing.T) {
	m := &Manager{Addr: "127.0.0.1:0", Dir: t.TempDir(), Log: slog.New(slog.DiscardHandler), Handler: http.NotFoundHandler(),
		FindRouter: func(context.Context) (router, error) { return nil, errors.New("timeout") }}
	m.SetEnabled(true)
	m.check(context.Background())
	defer m.stop(context.Background())
	if st := m.Status(); st.State != StateFailed || !strings.HasPrefix(st.Problem, "No router") || !strings.HasSuffix(st.Problem, ".") {
		t.Fatalf("status %+v", st)
	}
	m.SetEnabled(false)
	if st := m.Status(); st.State != StateOff {
		t.Fatalf("status %+v", st)
	}
}

func TestForwardedPortNotReachable(t *testing.T) {
	m := &Manager{Addr: "127.0.0.1:0", Dir: t.TempDir(), Log: slog.New(slog.DiscardHandler), Handler: http.NotFoundHandler()}
	m.SetForwardedPort(443)
	m.SetEnabled(true)
	m.check(context.Background())
	if st := m.Status(); st.State != StateFailed || !strings.Contains(st.Problem, "needs the connect service") {
		t.Fatalf("without the service: %+v", st)
	}
	m.stop(context.Background())

	// The service can't reach the forwarded port.
	var dialed string
	svc := &connect.Service{AllowPrivate: true, Dial: func(_ context.Context, _, addr string) (net.Conn, error) {
		dialed = addr
		return nil, errors.New("refused")
	}}
	ts := httptest.NewServer(svc.Handler())
	defer ts.Close()
	key, _ := connect.LoadKey(filepath.Join(t.TempDir(), "server.key"))
	m = &Manager{Addr: "127.0.0.1:0", Dir: t.TempDir(), Log: slog.New(slog.DiscardHandler), Handler: http.NotFoundHandler(),
		Connect: &connect.Client{URL: ts.URL, Key: key}}
	m.SetForwardedPort(443)
	m.SetEnabled(true)
	m.check(context.Background())
	defer m.stop(context.Background())
	st := m.Status()
	want := fmt.Sprintf("forward TCP port 443 to port %d on this computer", m.port)
	if st.State != StateFailed || st.PublicIP != "127.0.0.1" || st.ForwardedPort != 443 || !strings.Contains(st.Problem, want) || dialed != "127.0.0.1:443" {
		t.Fatalf("status %+v, dialed %s", st, dialed)
	}
}

// TestPebble gets a real certificate from Pebble, Let's Encrypt's test CA,
// through a pretend router. It runs when the pebble binary is on PATH:
// go install github.com/letsencrypt/pebble/v2/cmd/pebble@latest
func TestPebble(t *testing.T) {
	bin, err := exec.LookPath("pebble")
	if err != nil {
		t.Skip("pebble isn't installed")
	}
	src := os.Getenv("PEBBLE_DIR") // its source, for the test certificates
	if src == "" {
		out, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", "github.com/letsencrypt/pebble/v2").Output()
		if err != nil || len(out) == 0 {
			t.Skip("set PEBBLE_DIR to pebble's source directory")
		}
		src = string(out[:len(out)-1])
	}
	for _, c := range []struct {
		name   string
		refuse map[int]bool
		byHand bool
	}{{"tls-alpn-01", nil, false}, {"http-01 when 443 is taken", map[int]bool{15001: true}, false}, {"port forwarded by hand", nil, true}} {
		t.Run(c.name, func(t *testing.T) { runPebble(t, bin, src, c.refuse, c.byHand) })
	}
}

// runPebble gets a certificate through a router that opens ports, or with
// byHand through a port the admin forwarded (here, the CA's TLS port
// straight to the server) and the connect service.
func runPebble(t *testing.T, bin, src string, refuse map[int]bool, byHand bool) {
	dir := t.TempDir()
	cfg := map[string]any{"pebble": map[string]any{
		"listenAddress": "127.0.0.1:14000", "managementListenAddress": "127.0.0.1:15000",
		"certificate": filepath.Join(src, "test/certs/localhost/cert.pem"), "privateKey": filepath.Join(src, "test/certs/localhost/key.pem"),
		"httpPort": 15002, "tlsPort": 15001, "keyAlgorithm": "ecdsa",
		"profiles": map[string]any{"default": map[string]any{"validityPeriod": 7776000}, "shortlived": map[string]any{"validityPeriod": 518400}},
	}}
	b, _ := json.Marshal(cfg)
	os.WriteFile(filepath.Join(dir, "pebble.json"), b, 0o644)
	cmd := exec.Command(bin, "-config", filepath.Join(dir, "pebble.json"), "-dnsserver", "127.0.0.1:1")
	cmd.Env = append(os.Environ(), "PEBBLE_VA_NOSLEEP=1", "PEBBLE_WFE_NONCEREJECT=0", "PEBBLE_AUTHZREUSE=0")
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	if testing.Verbose() {
		cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { cmd.Process.Kill(); cmd.Wait() }()

	roots := x509.NewCertPool()
	ca, _ := os.ReadFile(filepath.Join(src, "test/certs/pebble.minica.pem"))
	roots.AppendCertsFromPEM(ca)
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots}}}
	for i := 0; ; i++ {
		if _, err := client.Get("https://127.0.0.1:14000/dir"); err == nil {
			break
		} else if i > 50 {
			t.Fatal("pebble didn't start:", err)
		}
		time.Sleep(100 * time.Millisecond)
	}

	r := &fakeRouter{ip: netip.MustParseAddr("127.0.0.1"), refuse: refuse}
	m := &Manager{
		Addr: "127.0.0.1:0", Dir: t.TempDir(), Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "matinee") }),
		CA:      "https://127.0.0.1:14000/dir", HTTPClient: client, AllowPrivate: true,
		FindRouter: func(context.Context) (router, error) { return r, nil },
		Challenges: []challenge{{"tls-alpn-01", 15001}, {"http-01", 15002}},
	}
	svc := &connect.Service{AllowPrivate: true}
	ts := httptest.NewServer(svc.Handler())
	defer ts.Close()
	key, err := connect.LoadKey(filepath.Join(t.TempDir(), "server.key"))
	if err != nil {
		t.Fatal(err)
	}
	m.Connect = &connect.Client{URL: ts.URL, Key: key}
	if byHand {
		m.Addr, m.FindRouter = "127.0.0.1:15001", nil
		m.SetForwardedPort(15001)
	}
	m.SetEnabled(true)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	m.check(ctx)
	defer m.stop(context.Background())
	st := m.Status()
	if st.State != StateReady {
		t.Fatalf("status %+v", st)
	}
	router := "UPnP"
	if byHand {
		router = ""
	}
	if st.URL != fmt.Sprintf("https://127.0.0.1:%d", m.mapped) || st.CertificateExpires == nil || st.Router != router || !st.Findable {
		t.Fatalf("status %+v", st)
	}
	// The apps can look the server up.
	resp, err := http.Get(m.Connect.LookupURL())
	if err != nil {
		t.Fatal(err)
	}
	var found connect.Found
	json.NewDecoder(resp.Body).Decode(&found)
	resp.Body.Close()
	if len(found.URLs) != 1 || found.URLs[0] != st.URL {
		t.Fatalf("lookup %+v", found)
	}
	if d := time.Until(*st.CertificateExpires); d > 7*24*time.Hour {
		t.Errorf("expected a short-lived certificate, got one for %v", d)
	}
	// The temporary forward for the check is gone.
	r.mu.Lock()
	if len(r.proxies) != 0 {
		t.Errorf("forwards left open: %v", r.proxies)
	}
	r.mu.Unlock()

	// The server answers over HTTPS with the new certificate, which pebble's
	// issuing root (fetched from its management port) vouches for.
	resp, err = client.Get("https://127.0.0.1:15000/roots/0")
	if err != nil {
		t.Fatal(err)
	}
	issuer, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(issuer) {
		t.Fatal("no pebble root")
	}
	c := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}}}
	resp, err = c.Get(st.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(body) != "matinee" {
		t.Errorf("got %q", body)
	}

	// A restart reuses the saved certificate.
	m2 := &Manager{Dir: m.Dir}
	if got := m2.loadCert(); got == nil || !current(got, r.ip) {
		t.Error("saved certificate didn't load")
	}
}
