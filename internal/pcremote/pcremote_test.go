package pcremote

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// The test binary stands in for matinee-pcremote when FAKE_PCREMOTE is set.
func TestMain(m *testing.M) {
	if os.Getenv("FAKE_PCREMOTE") != "" {
		os.Exit(fakeBridge(os.Args[1:]))
	}
	os.Exit(m.Run())
}

func msgBytes(typ byte, b []byte) []byte {
	h := make([]byte, 8)
	h[0] = typ
	binary.LittleEndian.PutUint32(h[4:], uint32(len(b)))
	return append(h, b...)
}

func fakeBridge(args []string) int {
	switch args[0] {
	case "query":
		fmt.Println(`{"launchQuery":"&corever=1"}`)
	case "play":
		if os.Getenv("MATINEE_PC_RTSP") != "rtsp://pc:48010" || len(os.Getenv("MATINEE_PC_RIKEY")) != 32 {
			os.Stdout.Write(msgBytes('E', []byte(`{"error":"bad launch `+os.Getenv("MATINEE_PC_RTSP")+`"}`)))
			return 1
		}
		os.Stdout.Write(msgBytes('I', []byte(`{"width":1920,"height":1080,"fps":60}`)))
		os.Stdout.Write(msgBytes('V', []byte{0, 0, 0, 1, 0x67, 1, 0, 0, 0, 1, 0x68, 2, 0, 0, 0, 1, 0x65, 3}))
		h := make([]byte, 8)
		for {
			if _, err := io.ReadFull(os.Stdin, h); err != nil {
				return 0
			}
			b := make([]byte, binary.LittleEndian.Uint32(h[4:]))
			io.ReadFull(os.Stdin, b)
			if h[0] == 'Q' {
				return 0
			}
			os.Stdout.Write(msgBytes('E', []byte(`{"error":"got `+string(h[0])+hex.EncodeToString(b)+`"}`)))
		}
	}
	return 0
}

// sunshine is a fake Sunshine: its pairing, as the real one does it, and
// enough of the rest to launch.
type sunshine struct {
	t        *testing.T
	pin      string
	cert     *x509.Certificate
	certPEM  []byte
	key      *rsa.PrivateKey
	http     *httptest.Server
	https    *httptest.Server
	mu       sync.Mutex
	aesKey   []byte
	client   *x509.Certificate
	clientCh []byte
	serverCh []byte
	secret   []byte
	hash     []byte
	paired   bool
	launched string
	canceled int
	current  int
}

func newSunshine(t *testing.T, pin string) *sunshine {
	id, err := NewIdentity()
	if err != nil {
		t.Fatal(err)
	}
	cert, key, _ := id.parse()
	s := &sunshine{t: t, pin: pin, cert: cert, certPEM: []byte(id.Cert), key: key}
	s.http = httptest.NewServer(http.HandlerFunc(s.serveHTTP))
	s.https = httptest.NewUnstartedServer(http.HandlerFunc(s.serveHTTPS))
	s.https.TLS = &tls.Config{
		Certificates: []tls.Certificate{{Certificate: [][]byte{cert.Raw}, PrivateKey: key}},
		ClientAuth:   tls.RequireAnyClientCert,
	}
	s.https.StartTLS()
	t.Cleanup(func() { s.http.Close(); s.https.Close() })
	return s
}

func port(u string) int {
	_, p, _ := net.SplitHostPort(strings.TrimPrefix(strings.TrimPrefix(u, "http://"), "https://"))
	n, _ := strconv.Atoi(p)
	return n
}

func (s *sunshine) xml(w http.ResponseWriter, inner string) {
	fmt.Fprintf(w, `<?xml version="1.0" encoding="utf-8"?><root status_code="200">%s</root>`, inner)
}

func (s *sunshine) info(paired bool) string {
	state := "SUNSHINE_SERVER_FREE"
	if s.current != 0 {
		state = "SUNSHINE_SERVER_BUSY"
	}
	p := 0
	if paired {
		p = 1
	}
	return fmt.Sprintf(`<hostname>GAMING-PC</hostname><appversion>7.1.431.-1</appversion><GfeVersion>3.23.0.74</GfeVersion>`+
		`<HttpsPort>%d</HttpsPort><mac>aa:bb:cc:dd:ee:ff</mac><ServerCodecModeSupport>3843</ServerCodecModeSupport>`+
		`<PairStatus>%d</PairStatus><currentgame>%d</currentgame><state>%s</state>`, port(s.https.URL), p, s.current, state)
}

func (s *sunshine) serveHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	q := r.URL.Query()
	switch r.URL.Path {
	case "/serverinfo":
		s.xml(w, s.info(false))
	case "/unpair":
		s.paired = false
		s.xml(w, "")
	case "/pair":
		dec := func(k string) []byte {
			b, err := hex.DecodeString(q.Get(k))
			if err != nil {
				s.t.Errorf("%s: %v", k, err)
			}
			return b
		}
		switch {
		case q.Get("phrase") == "getservercert":
			salt := dec("salt")
			h := sha256.Sum256(append(salt, []byte(s.pin)...))
			s.aesKey = h[:16]
			pb, _ := pem.Decode(dec("clientcert"))
			s.client, _ = x509.ParseCertificate(pb.Bytes)
			s.xml(w, "<paired>1</paired><plaincert>"+hex.EncodeToString(s.certPEM)+"</plaincert>")
		case q.Has("clientchallenge"):
			s.clientCh = ecb(s.aesKey, dec("clientchallenge"), false)
			s.serverCh, s.secret = random(16), random(16)
			h := sha256.Sum256(concat(s.clientCh, s.cert.Signature, s.secret))
			s.xml(w, "<paired>1</paired><challengeresponse>"+hex.EncodeToString(ecb(s.aesKey, concat(h[:], s.serverCh), true))+"</challengeresponse>")
		case q.Has("serverchallengeresp"):
			s.hash = ecb(s.aesKey, dec("serverchallengeresp"), false)
			h := sha256.Sum256(s.secret)
			sig, _ := rsa.SignPKCS1v15(rand.Reader, s.key, crypto.SHA256, h[:])
			s.xml(w, "<paired>1</paired><pairingsecret>"+hex.EncodeToString(concat(s.secret, sig))+"</pairingsecret>")
		case q.Has("clientpairingsecret"):
			b := dec("clientpairingsecret")
			secret, sig := b[:16], b[16:]
			h := sha256.Sum256(secret)
			okSig := rsa.VerifyPKCS1v15(s.client.PublicKey.(*rsa.PublicKey), crypto.SHA256, h[:], sig) == nil
			want := sha256.Sum256(concat(s.serverCh, s.client.Signature, secret))
			if !okSig || !bytes.Equal(want[:], s.hash) {
				s.xml(w, "<paired>0</paired>")
				return
			}
			s.paired = true
			s.xml(w, "<paired>1</paired>")
		}
	default:
		http.NotFound(w, r)
	}
}

func (s *sunshine) serveHTTPS(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	known := s.paired && len(r.TLS.PeerCertificates) > 0 && s.client != nil && bytes.Equal(r.TLS.PeerCertificates[0].Raw, s.client.Raw)
	if !known {
		fmt.Fprint(w, `<root status_code="401" status_message="The client is not authorized. Certificate verification failed."/>`)
		return
	}
	q := r.URL.Query()
	switch r.URL.Path {
	case "/pair":
		s.xml(w, "<paired>1</paired>")
	case "/serverinfo":
		s.xml(w, s.info(true))
	case "/applist":
		s.xml(w, "<App><AppTitle>Desktop</AppTitle><ID>1</ID></App><App><AppTitle>Steam Big Picture</AppTitle><ID>2</ID></App>")
	case "/launch", "/resume":
		if q.Get("corever") != "1" || len(q.Get("rikey")) != 32 || q.Get("mode") != "1920x1080x60" {
			s.t.Errorf("launch %s", r.URL.RawQuery)
		}
		s.launched = r.URL.Path + " " + q.Get("appid")
		s.current, _ = strconv.Atoi(q.Get("appid"))
		if r.URL.Path == "/resume" {
			s.xml(w, "<resume>1</resume><sessionUrl0>rtsp://pc:48010</sessionUrl0>")
			return
		}
		s.xml(w, "<gamesession>1</gamesession><sessionUrl0>rtsp://pc:48010</sessionUrl0>")
	case "/cancel":
		s.canceled++
		s.current = 0
		s.xml(w, "<cancel>1</cancel>")
	case "/unpair":
		s.paired = false
		s.xml(w, "")
	}
}

func pairedClient(t *testing.T, s *sunshine) (*Client, PC) {
	id, err := NewIdentity()
	if err != nil {
		t.Fatal(err)
	}
	c := &Client{Identity: id, Timeout: 5 * time.Second}
	pc, err := c.Pair(context.Background(), "127.0.0.1", port(s.http.URL), s.pin, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return c, pc
}

func TestPair(t *testing.T) {
	s := newSunshine(t, "1234")
	c, pc := pairedClient(t, s)
	if pc.Name != "GAMING-PC" || pc.HTTPSPort != port(s.https.URL) || !strings.Contains(pc.Cert, "CERTIFICATE") {
		t.Fatalf("pc %+v", pc)
	}
	info, err := c.Info(context.Background(), pc)
	if err != nil || !info.Paired || info.Name != "GAMING-PC" {
		t.Fatalf("info %+v %v", info, err)
	}
	apps, err := c.Apps(context.Background(), pc)
	if err != nil || len(apps) != 2 {
		t.Fatalf("apps %v %v", apps, err)
	}
	if a, ok := SteamApp(apps); !ok || a.ID != 2 {
		t.Fatalf("steam app %+v", a)
	}

	// Another certificate for the PC is refused.
	other := pc
	other.Cert = c.Identity.Cert
	if _, err := c.Info(context.Background(), other); err == nil {
		t.Fatal("a different PC certificate was taken")
	}
	// Once the PC forgets Matinee, it says so.
	if err := c.Unpair(context.Background(), pc); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Info(context.Background(), pc); err != ErrNotPaired {
		t.Fatalf("after unpairing: %v", err)
	}
}

func TestPairWrongPIN(t *testing.T) {
	s := newSunshine(t, "1234")
	id, _ := NewIdentity()
	c := &Client{Identity: id, Timeout: 5 * time.Second}
	_, err := c.Pair(context.Background(), "127.0.0.1", port(s.http.URL), "9999", 5*time.Second)
	if err == nil || !strings.Contains(err.Error(), "PIN didn't match") {
		t.Fatalf("err %v", err)
	}
	if s.paired {
		t.Fatal("paired with the wrong PIN")
	}
}

func TestNoSunshine(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	p := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	c := &Client{Timeout: time.Second}
	if _, err := c.Find(context.Background(), "127.0.0.1", p); err != ErrNoAnswer {
		t.Fatalf("err %v", err)
	}
}

func fake(t *testing.T) *Bridge {
	t.Setenv("FAKE_PCREMOTE", "1")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return &Bridge{Path: exe}
}

func TestPlay(t *testing.T) {
	s := newSunshine(t, "1234")
	c, pc := pairedClient(t, s)
	b := fake(t)
	st, err := b.Play(context.Background(), c, pc, 2, DefaultSettings, nil)
	if err != nil {
		t.Fatal(err)
	}
	if st.Hello.Width != 1920 || s.launched != "/launch 2" {
		t.Fatalf("hello %+v launched %q", st.Hello, s.launched)
	}
	for range 3 {
		if k, _, err := st.Next(); err != nil || k != KindVideo {
			t.Fatalf("next %x %v", k, err)
		}
	}
	// A: the bottom face button, on the first controller.
	st.Input([]byte{1, 0, 1, 0})
	_, _, err = st.Next()
	want := "got C" + hex.EncodeToString([]byte{0, 1, 0, 0x00, 0x10, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0})
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("input: %v, want %s", err, want)
	}
	st.Close()
	if s.canceled != 1 {
		t.Fatalf("game not ended on the PC: %d", s.canceled)
	}

	// A game already running is gone back to.
	s.current = 2
	st, err = b.Play(context.Background(), c, pc, 2, DefaultSettings, nil)
	if err != nil {
		t.Fatal(err)
	}
	st.Close()
	if s.launched != "/resume 2" {
		t.Fatalf("launched %q", s.launched)
	}
}

func TestInput(t *testing.T) {
	in := newInput(1920, 1080)
	// Select and Start: the Xbox button alone.
	m := in.apply([]byte{1, 1, 1<<rSelect | 1<<rStart, 0})
	if len(m) != 1 || m[0].b[0] != 1 || binary.LittleEndian.Uint16(m[0].b[1:]) != 2 || binary.LittleEndian.Uint32(m[0].b[3:]) != mlSpecial {
		t.Fatalf("guide %v", m)
	}
	// Left stick all the way up is +32767 on an Xbox pad.
	m = in.apply([]byte{3, 0, 1, 0x80})
	if y := int16(binary.LittleEndian.Uint16(m[0].b[11:])); y != 32767 {
		t.Fatalf("up %d", y)
	}
	if binary.LittleEndian.Uint16(m[0].b[1:]) != 3 {
		t.Fatal("both pads should be present")
	}
	// Shift then A: A comes with Shift held.
	in.apply([]byte{4, 42, 0, 1})
	m = in.apply([]byte{4, 30, 0, 1})
	if len(m) != 1 || m[0].typ != 'k' || binary.LittleEndian.Uint16(m[0].b) != 0x8000|'A' || m[0].b[3] != modShift {
		t.Fatalf("key %v", m)
	}
	// The pointer to the middle.
	m = in.apply([]byte{5, 0, 0x80, 0, 0x80})
	if x := binary.LittleEndian.Uint16(m[0].b); x != 960 || binary.LittleEndian.Uint16(m[0].b[2:]) != 540 {
		t.Fatalf("pointer %v", m[0].b)
	}
	// Right button down.
	m = in.apply([]byte{7, 2, 1})
	if m[0].typ != 'b' || m[0].b[0] != 3 || m[0].b[1] != 1 {
		t.Fatalf("button %v", m)
	}
	// Everything is let go at the end.
	rel := in.release()
	var kinds string
	for _, r := range rel {
		kinds += string(r.typ)
	}
	if strings.Count(kinds, "k") != 2 || strings.Count(kinds, "b") != 1 || strings.Count(kinds, "C") != 2 {
		t.Fatalf("released %q", kinds)
	}
}

func TestSplitAddress(t *testing.T) {
	for _, c := range []struct {
		in   string
		host string
		port int
		bad  bool
	}{
		{"192.168.1.30", "192.168.1.30", DefaultPort, false},
		{"192.168.1.30:47990", "192.168.1.30", DefaultPort, false},
		{"https://gaming-pc.local:48989/", "gaming-pc.local", 48989, false},
		{"8.8.8.8", "", 0, true},
		{"", "", 0, true},
	} {
		h, p, err := SplitAddress(c.in)
		if (err != nil) != c.bad || h != c.host || p != c.port {
			t.Errorf("%q: %q %d %v", c.in, h, p, err)
		}
	}
}
