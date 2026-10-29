package api

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/yeixio/matinee/internal/connect"
	"github.com/yeixio/matinee/internal/remote"
)

func TestRemoteAccessSetting(t *testing.T) {
	m := &remote.Manager{Addr: "127.0.0.1:0", Dir: t.TempDir(), Handler: http.NotFoundHandler(), Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	c, _ := newTestServer(t, func(s *Server) { s.Remote = m })
	var login struct{ Token string }
	c.do("POST", "/api/v1/setup", credentials{"admin", "correct horse"}, &login)
	c.token = login.Token

	var set serverSettings
	c.do("GET", "/api/v1/settings", nil, &set)
	if set.RemoteAccess || set.Remote == nil || set.Remote.State != remote.StateOff {
		t.Fatalf("off by default: %+v %+v", set, set.Remote)
	}
	if code := c.do("PUT", "/api/v1/settings", map[string]any{"remoteAccess": true}, &set); code != 200 || !set.RemoteAccess {
		t.Fatalf("turn on: %d %+v", code, set)
	}
	// Run isn't going, so it stays at setting up.
	if set.Remote.State != remote.StateWorking || !m.Status().Enabled {
		t.Fatalf("status %+v", set.Remote)
	}
	// What a client got back can be sent as is.
	set.RemoteAccess = false
	if code := c.do("PUT", "/api/v1/settings", set, &set); code != 200 || set.RemoteAccess || set.Remote.State != remote.StateOff {
		t.Fatalf("round trip: %d %+v", code, set)
	}

	// No address yet, so the apps get none.
	var info struct {
		RemoteURL   *string
		RemoteReady bool
	}
	c.do("GET", "/api/v1/server", nil, &info)
	if info.RemoteURL == nil || *info.RemoteURL != "" || info.RemoteReady {
		t.Fatalf("remoteURL %v ready %v", info.RemoteURL, info.RemoteReady)
	}
}

func TestRemoteForwardedPortSetting(t *testing.T) {
	m := &remote.Manager{Addr: "127.0.0.1:0", Dir: t.TempDir(), Handler: http.NotFoundHandler(), Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	c, _ := newTestServer(t, func(s *Server) { s.Remote = m })
	var login struct{ Token string }
	c.do("POST", "/api/v1/setup", credentials{"admin", "correct horse"}, &login)
	c.token = login.Token

	var set serverSettings
	if code := c.do("PUT", "/api/v1/settings", map[string]any{"remoteForwardedPort": 7443}, &set); code != http.StatusBadRequest {
		t.Fatalf("port the certificate check can't use: %d", code)
	}
	if code := c.do("PUT", "/api/v1/settings", map[string]any{"remoteForwardedPort": 443}, &set); code != 200 || set.RemoteForwardedPort != 443 {
		t.Fatalf("set 443: %d %+v", code, set)
	}
	c.do("GET", "/api/v1/settings", nil, &set)
	if set.RemoteForwardedPort != 443 {
		t.Fatalf("kept %d", set.RemoteForwardedPort)
	}

	// Without an outside address, the apps get no lookup either.
	var info struct{ ServerID, LookupURL *string }
	c.do("GET", "/api/v1/server", nil, &info)
	if info.ServerID == nil || *info.ServerID != "" || info.LookupURL == nil || *info.LookupURL != "" {
		t.Fatalf("server %+v", info)
	}
}

func TestPublicURLIsRemoteURL(t *testing.T) {
	c, _ := newTestServer(t, func(s *Server) {
		s.PublicURL = "https://matinee.example.com"
		s.LookupHost = resolveTo("203.0.113.7")
	})
	var info struct {
		RemoteURL   string
		RemoteReady bool
	}
	c.do("GET", "/api/v1/server", nil, &info)
	if info.RemoteURL != "https://matinee.example.com" || !info.RemoteReady {
		t.Fatalf("server %+v", info)
	}
	var login struct{ Token string }
	c.do("POST", "/api/v1/setup", credentials{"admin", "correct horse"}, &login)
	c.token = login.Token
	var set serverSettings
	c.do("GET", "/api/v1/settings", nil, &set)
	if set.PublicURL != "https://matinee.example.com" || set.PublicURLHomeOnly || set.Remote != nil {
		t.Fatalf("settings %+v", set)
	}
}

func resolveTo(addrs ...string) func(context.Context, string) ([]string, error) {
	return func(context.Context, string) ([]string, error) { return addrs, nil }
}

// A public URL whose name only leads to the home network doesn't stand in
// for remote access.
func TestHomeOnlyPublicURL(t *testing.T) {
	for _, tc := range []struct {
		url   string
		addrs []string
		home  bool
	}{
		{"https://matinee.example.com", []string{"203.0.113.7"}, false},
		{"https://matinee.example.com", []string{"192.168.1.20"}, true},
		{"https://matinee.example.com", []string{"203.0.113.7", "10.0.0.5"}, true},
		{"https://matinee.example.com", nil, true},
		{"https://100.101.102.103:7171", nil, true},
		{"https://[fd00::1]", nil, true},
		{"https://198.51.100.2", nil, false},
	} {
		s := &Server{PublicURL: tc.url, LookupHost: resolveTo(tc.addrs...)}
		if got := !s.publicURLIsPublic(context.Background()); got != tc.home {
			t.Errorf("%s at %v: home only %v, want %v", tc.url, tc.addrs, got, tc.home)
		}
		// Away from home the apps still get it when nothing better exists:
		// it may work through a VPN.
		if got := s.remoteURL(context.Background()); got != tc.url {
			t.Errorf("%s: remoteURL %q", tc.url, got)
		}
		if got := s.remoteReady(context.Background()); got == tc.home {
			t.Errorf("%s: remoteReady %v", tc.url, got)
		}
	}
}

// A device on the home network that comes in through the remote access port
// (or a router that makes outside connections look local) signs in.
func TestRemotePortIsNeverHome(t *testing.T) {
	c, _ := newTestServer(t)
	var login struct{ Token string }
	c.do("POST", "/api/v1/setup", credentials{"admin", "correct horse"}, &login)
	ts := httptest.NewServer(c.srv.RemoteHandler())
	defer ts.Close()
	remote := &client{t: t, base: ts.URL, srv: c.srv}
	var info struct{ HomeAccess bool }
	remote.do("GET", "/api/v1/server", nil, &info)
	if info.HomeAccess {
		t.Fatal("remote port gave home access")
	}
	if code := remote.do("GET", "/api/v1/home", nil, nil); code != http.StatusUnauthorized {
		t.Fatalf("home rows without signing in: %d", code)
	}
	// The main port, from the same loopback address, does count.
	c.do("GET", "/api/v1/server", nil, &info)
	if !info.HomeAccess {
		t.Fatal("main port should give home access from loopback")
	}
}

func TestProveServer(t *testing.T) {
	_, key, _ := ed25519.GenerateKey(nil)
	m := &remote.Manager{Addr: "127.0.0.1:0", Dir: t.TempDir(), Handler: http.NotFoundHandler(), Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Connect: &connect.Client{Key: key}}
	c, _ := newTestServer(t, func(s *Server) { s.Remote = m; s.PublicURL = "https://Matinee.example.com" })

	nonce := []byte("0123456789abcdef0123")
	prove := func(host string) (int, []byte, []byte) {
		body, _ := json.Marshal(map[string]any{"nonce": nonce})
		r := httptest.NewRequest("POST", "/api/v1/server/proof", bytes.NewReader(body))
		r.Host = host
		w := httptest.NewRecorder()
		c.srv.Handler().ServeHTTP(w, r)
		var out struct{ Key, Signature []byte }
		json.Unmarshal(w.Body.Bytes(), &out)
		return w.Code, out.Key, out.Signature
	}

	for _, host := range []string{"matinee.example.com", "matinee.example.com:443"} {
		code, pub, sig := prove(host)
		if code != http.StatusOK {
			t.Fatalf("%s: %d", host, code)
		}
		if connect.ID(pub) != m.Connect.ID() {
			t.Fatalf("%s: key leads to %s, want %s", host, connect.ID(pub), m.Connect.ID())
		}
		if !ed25519.Verify(pub, connect.ProofMessage("matinee.example.com", nonce), sig) {
			t.Fatalf("%s: signature doesn't check out", host)
		}
	}
	// Someone else's address, as when another server passes the request on.
	if code, _, _ := prove("203.0.113.9:7443"); code != http.StatusForbidden {
		t.Fatalf("another address: %d", code)
	}
	body, _ := json.Marshal(map[string]any{"nonce": []byte("short")})
	r := httptest.NewRequest("POST", "/api/v1/server/proof", bytes.NewReader(body))
	r.Host = "matinee.example.com"
	w := httptest.NewRecorder()
	c.srv.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("short nonce: %d", w.Code)
	}
}
