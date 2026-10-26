package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFromHome(t *testing.T) {
	req := func(addr string, headers ...string) *http.Request {
		r := httptest.NewRequest("GET", "/api/v1/home", nil)
		r.RemoteAddr = addr
		for i := 0; i+1 < len(headers); i += 2 {
			r.Header.Set(headers[i], headers[i+1])
		}
		return r
	}
	s := &Server{}
	for _, tc := range []struct {
		r    *http.Request
		want bool
	}{
		{req("192.168.1.20:5000"), true},
		{req("10.0.0.7:5000"), true},
		{req("[fd00::5]:5000"), true},
		{req("[fe80::1]:5000"), true},
		{req("127.0.0.1:5000"), true},
		{req("[::ffff:192.168.1.20]:5000"), true},
		{req("203.0.113.9:5000"), false},
		{req("[2001:db8::1]:5000"), false},
		// Through a proxy every request looks private, so none count.
		{req("10.42.0.15:5000", "X-Forwarded-For", "203.0.113.9"), false},
		{req("10.42.0.15:5000", "X-Forwarded-For", "192.168.1.20"), false},
		{req("10.42.0.15:5000", "X-Real-IP", "192.168.1.20"), false},
		{req("10.42.0.15:5000", "Forwarded", "for=192.168.1.20"), false},
		{req("not an address"), false},
	} {
		if got := s.fromHome(tc.r); got != tc.want {
			t.Errorf("%s %v: fromHome = %v, want %v", tc.r.RemoteAddr, tc.r.Header, got, tc.want)
		}
	}

	// With its own home listener, the main address never counts.
	s = &Server{HomeListener: true}
	r := req("192.168.1.20:5000")
	if s.fromHome(r) {
		t.Error("main listener counted as home")
	}
	if !s.fromHome(r.WithContext(context.WithValue(r.Context(), homeKey{}, true))) {
		t.Error("home listener didn't count")
	}
}

// The test server listens on 127.0.0.1, so its requests come from home.
func TestHomeAccess(t *testing.T) {
	c, _ := newTestServer(t)
	guest := &client{t: t, base: c.base}

	var info struct{ HomeAccess bool }
	guest.do("GET", "/api/v1/server", nil, &info)
	if info.HomeAccess {
		t.Fatal("a server with no admin yet offered home access")
	}
	if code := guest.do("GET", "/api/v1/home", nil, nil); code != http.StatusUnauthorized {
		t.Fatalf("home before setup: %d", code)
	}

	var login struct{ Token string }
	c.do("POST", "/api/v1/setup", credentials{"admin", "correct horse"}, &login)
	c.token = login.Token

	guest.do("GET", "/api/v1/server", nil, &info)
	if !info.HomeAccess {
		t.Fatal("no home access by default")
	}
	var me struct {
		Username string
		Guest    bool
		IsAdmin  bool
	}
	if code := guest.do("GET", "/api/v1/me", nil, &me); code != 200 || !me.Guest || me.IsAdmin || me.Username != "Home" {
		t.Fatalf("guest /me: %d %+v", code, me)
	}
	for _, path := range []string{"/api/v1/home", "/api/v1/libraries", "/api/v1/search?q=x"} {
		if code := guest.do("GET", path, nil, nil); code != 200 {
			t.Errorf("guest %s: %d", path, code)
		}
	}
	// Anything beyond looking and playing still needs a sign-in.
	for _, rt := range []struct{ method, path string }{
		{"GET", "/api/v1/devices"}, {"GET", "/api/v1/downloads"}, {"POST", "/api/v1/auth/device/ABCD-EFGH/approve"},
		{"GET", "/api/v1/users"}, {"GET", "/api/v1/settings"}, {"POST", "/api/v1/libraries"},
	} {
		if code := guest.do(rt.method, rt.path, nil, nil); code != http.StatusUnauthorized {
			t.Errorf("guest %s %s: %d, want 401", rt.method, rt.path, code)
		}
	}
	// The guest isn't a person on the server and can't sign in.
	var users []struct{ Username string }
	c.do("GET", "/api/v1/users", nil, &users)
	if len(users) != 1 {
		t.Errorf("users = %+v", users)
	}

	// An admin can require signing in at home too.
	var set serverSettings
	if code := c.do("PUT", "/api/v1/settings", serverSettings{RequireSignInAtHome: true}, &set); code != 200 || !set.RequireSignInAtHome {
		t.Fatalf("settings: %d %+v", code, set)
	}
	if code := guest.do("GET", "/api/v1/home", nil, nil); code != http.StatusUnauthorized {
		t.Fatalf("home with sign-in required: %d", code)
	}
	guest.do("GET", "/api/v1/server", nil, &info)
	if info.HomeAccess {
		t.Fatal("home access offered with sign-in required")
	}
	c.do("GET", "/api/v1/settings", nil, &set)
	if !set.RequireSignInAtHome {
		t.Fatal("setting didn't stick")
	}
	// A bad token is still refused rather than treated as a guest.
	c.do("PUT", "/api/v1/settings", serverSettings{}, nil)
	bad := &client{t: t, base: c.base, token: "nope"}
	if code := bad.do("GET", "/api/v1/home", nil, nil); code != http.StatusUnauthorized {
		t.Fatalf("bad token at home: %d", code)
	}
}

func TestHardwareAccelerationSetting(t *testing.T) {
	c, _ := newTestServer(t)
	var login struct{ Token string }
	c.do("POST", "/api/v1/setup", credentials{"admin", "correct horse"}, &login)
	c.token = login.Token
	var set serverSettings
	c.do("GET", "/api/v1/settings", nil, &set)
	if set.HardwareAcceleration != "auto" || set.Hardware == nil || set.Hardware.Active != nil {
		t.Fatalf("defaults: %+v", set)
	}
	if code := c.do("PUT", "/api/v1/settings", map[string]any{"hardwareAcceleration": "vaapi"}, &set); code != 200 || set.HardwareAcceleration != "vaapi" {
		t.Fatalf("choose vaapi: %d %+v", code, set)
	}
	if code := c.do("PUT", "/api/v1/settings", map[string]any{"hardwareAcceleration": "gpu please"}, nil); code != http.StatusBadRequest {
		t.Fatalf("bad choice: %d", code)
	}
	// What a client got back can be sent as is.
	set.HardwareAcceleration = "off"
	if code := c.do("PUT", "/api/v1/settings", set, &set); code != 200 || set.HardwareAcceleration != "off" {
		t.Fatalf("round trip: %d %+v", code, set)
	}
}
