package connect

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// serve runs svc behind a test server that says every request comes from ip.
func serve(t *testing.T, svc *Service, ip *string) *httptest.Server {
	t.Helper()
	svc.ClientIPHeader = "X-Real-IP"
	h := svc.Handler()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Header.Set("X-Real-IP", *ip)
		h.ServeHTTP(w, r)
	}))
	t.Cleanup(ts.Close)
	return ts
}

func newClient(t *testing.T, url string) *Client {
	t.Helper()
	key, err := LoadKey(filepath.Join(t.TempDir(), "server.key"))
	if err != nil {
		t.Fatal(err)
	}
	return &Client{URL: url, Key: key}
}

func lookup(t *testing.T, url, id string) (Found, int) {
	t.Helper()
	resp, err := http.Get(url + "/v1/servers/" + id)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var f Found
	json.NewDecoder(resp.Body).Decode(&f)
	return f, resp.StatusCode
}

func TestRegisterAndLookUp(t *testing.T) {
	ip := "203.0.113.7"
	svc := &Service{}
	ts := serve(t, svc, &ip)
	c := newClient(t, ts.URL)
	ctx := context.Background()

	if _, code := lookup(t, ts.URL, c.ID()); code != http.StatusNotFound {
		t.Fatalf("unregistered lookup %d", code)
	}
	reg, err := c.Register(ctx, 7443)
	if err != nil {
		t.Fatal(err)
	}
	if reg.ID != c.ID() || reg.URL != "https://203.0.113.7:7443" {
		t.Fatalf("registered %+v", reg)
	}
	if f, code := lookup(t, ts.URL, strings.ToUpper(c.ID())); code != 200 || len(f.URLs) != 1 || f.URLs[0] != reg.URL {
		t.Fatalf("lookup %d %+v", code, f)
	}

	// A new address replaces the old one of its family; IPv6 sits beside it.
	time.Sleep(1100 * time.Millisecond) // registrations are ordered by the second
	ip = "198.51.100.9"
	if _, err := c.Register(ctx, 443); err != nil {
		t.Fatal(err)
	}
	ip = "2001:db8::1"
	svc.AllowPrivate = true // 2001:db8::/32 is for documentation
	time.Sleep(1100 * time.Millisecond)
	if _, err := c.Register(ctx, 7443); err != nil {
		t.Fatal(err)
	}
	f, _ := lookup(t, ts.URL, c.ID())
	if want := []string{"https://[2001:db8::1]:7443", "https://198.51.100.9"}; strings.Join(f.URLs, " ") != strings.Join(want, " ") {
		t.Fatalf("urls %v, want %v", f.URLs, want)
	}

	// Port 0 takes the address of its family down.
	time.Sleep(1100 * time.Millisecond)
	if _, err := c.Register(ctx, 0); err != nil {
		t.Fatal(err)
	}
	if f, _ := lookup(t, ts.URL, c.ID()); len(f.URLs) != 1 || f.URLs[0] != "https://198.51.100.9" {
		t.Fatalf("after taking IPv6 down %v", f.URLs)
	}
}

func TestRegistrationsExpire(t *testing.T) {
	ip := "203.0.113.7"
	now := time.Now()
	svc := &Service{Now: func() time.Time { return now }}
	ts := serve(t, svc, &ip)
	c := newClient(t, ts.URL)
	if _, err := c.Register(context.Background(), 7443); err != nil {
		t.Fatal(err)
	}
	now = now.Add(ttl + time.Minute)
	if _, code := lookup(t, ts.URL, c.ID()); code != http.StatusNotFound {
		t.Fatalf("expired lookup %d", code)
	}
	svc.sweep()
	if len(svc.servers) != 0 {
		t.Fatal("sweep kept an expired server")
	}
}

func TestRegisterRefuses(t *testing.T) {
	ip := "203.0.113.7"
	svc := &Service{}
	ts := serve(t, svc, &ip)
	c := newClient(t, ts.URL)
	other := newClient(t, ts.URL)

	post := func(in signed) int {
		b, _ := json.Marshal(in)
		resp, err := http.Post(ts.URL+"/v1/register", "application/json", strings.NewReader(string(b)))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	payload := func(at time.Time) []byte {
		b, _ := json.Marshal(registration{Time: at.Unix(), Port: 7443})
		return b
	}
	sign := func(k ed25519.PrivateKey, p []byte) []byte { return ed25519.Sign(k, append([]byte(signContext), p...)) }
	pub := c.Key.Public().(ed25519.PublicKey)

	p := payload(time.Now())
	if code := post(signed{Key: pub, Payload: p, Sig: sign(other.Key, p)}); code != http.StatusForbidden {
		t.Errorf("someone else's signature: %d", code)
	}
	old := payload(time.Now().Add(-10 * time.Minute))
	if code := post(signed{Key: pub, Payload: old, Sig: sign(c.Key, old)}); code != http.StatusBadRequest {
		t.Errorf("stale registration: %d", code)
	}
	if code := post(signed{Key: pub, Payload: p, Sig: sign(c.Key, p)}); code != http.StatusOK {
		t.Fatalf("good registration: %d", code)
	}
	if code := post(signed{Key: pub, Payload: p, Sig: sign(c.Key, p)}); code != http.StatusConflict {
		t.Errorf("replayed registration: %d", code)
	}

	// Private and shared addresses can't be reached, so aren't registered.
	for _, ip = range []string{"192.168.1.20", "100.64.3.4"} {
		if _, err := c.Register(context.Background(), 7443); err == nil {
			t.Errorf("registered from %s", ip)
		}
	}
}

func TestCheckOnlyDialsTheCaller(t *testing.T) {
	ip := "203.0.113.7"
	var dialed []string
	svc := &Service{Dial: func(ctx context.Context, network, addr string) (net.Conn, error) {
		dialed = append(dialed, addr)
		if addr == "203.0.113.7:443" {
			a, b := net.Pipe()
			b.Close()
			return a, nil
		}
		return nil, errors.New("refused")
	}}
	ts := serve(t, svc, &ip)
	c := newClient(t, ts.URL)
	got, ok, err := c.Check(context.Background(), 443)
	if err != nil || got.String() != ip || !ok {
		t.Fatalf("check 443: %v %v %v", got, ok, err)
	}
	if _, ok, _ := c.Check(context.Background(), 7443); ok {
		t.Fatal("closed port reachable")
	}
	if strings.Join(dialed, " ") != "203.0.113.7:443 203.0.113.7:7443" {
		t.Fatalf("dialed %v", dialed)
	}
}

func TestRateLimit(t *testing.T) {
	ip := "203.0.113.7"
	svc := &Service{}
	ts := serve(t, svc, &ip)
	id := newClient(t, ts.URL).ID()
	for i := range perMinute["lookup"] + 1 {
		_, code := lookup(t, ts.URL, id)
		if i < perMinute["lookup"] && code != http.StatusNotFound || i == perMinute["lookup"] && code != http.StatusTooManyRequests {
			t.Fatalf("lookup %d: %d", i, code)
		}
	}
}

func TestLoadKeyKeepsTheID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "remote", "server.key")
	a, err := LoadKey(path)
	if err != nil {
		t.Fatal(err)
	}
	b, err := LoadKey(path)
	if err != nil {
		t.Fatal(err)
	}
	if ID(a.Public().(ed25519.PublicKey)) != ID(b.Public().(ed25519.PublicKey)) || !validID(ID(a.Public().(ed25519.PublicKey))) {
		t.Fatal("id changed or invalid")
	}
}
