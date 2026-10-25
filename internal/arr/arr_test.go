package arr_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yeixio/matinee/internal/arr"
	"github.com/yeixio/matinee/internal/arr/arrtest"
)

func TestNormalizeURL(t *testing.T) {
	for in, want := range map[string]string{
		"radarr:7878":                     "http://radarr:7878",
		" https://example.com/sonarr/ ":   "https://example.com/sonarr",
		"http://user:pw@radarr:7878/?x=1": "http://radarr:7878",
	} {
		if got, err := arr.NormalizeURL(in); err != nil || got != want {
			t.Errorf("NormalizeURL(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "ftp://radarr", "http://"} {
		if _, err := arr.NormalizeURL(bad); err == nil {
			t.Errorf("NormalizeURL(%q) accepted", bad)
		}
	}
}

func TestStatus(t *testing.T) {
	ctx := context.Background()
	radarr := arrtest.New(t, arr.Radarr)
	c, _ := arr.New(arr.Radarr, arr.Config{URL: radarr.URL, APIKey: radarr.Key})
	if _, err := c.Status(ctx); err != nil {
		t.Fatal(err)
	}
	// The Sonarr slot pointed at Radarr.
	c, _ = arr.New(arr.Sonarr, arr.Config{URL: radarr.URL, APIKey: radarr.Key})
	if _, err := c.Status(ctx); err == nil || !strings.Contains(err.Error(), "not Sonarr") {
		t.Fatalf("wrong app: %v", err)
	}
	c, _ = arr.New(arr.Radarr, arr.Config{URL: radarr.URL, APIKey: "nope"})
	if _, err := c.Status(ctx); err == nil || !strings.Contains(err.Error(), "API key") {
		t.Fatalf("wrong key: %v", err)
	}
}

// An app with a URL base redirects requests that leave it out; the client
// moves to the base, not the redirected request's whole address.
func TestURLBase(t *testing.T) {
	radarr := arrtest.New(t, arr.Radarr)
	radarr.Base = "/radarr"
	c, _ := arr.New(arr.Radarr, arr.Config{URL: radarr.URL + "/", APIKey: radarr.Key})
	if _, err := c.Status(context.Background()); err != nil {
		t.Fatal(err)
	}
	if c.URL() != radarr.URL+"/radarr" {
		t.Fatalf("URL = %s", c.URL())
	}
	if _, err := c.QualityProfiles(context.Background()); err != nil {
		t.Fatalf("after moving to the base: %v", err)
	}
}

// The API key travels in a header that a redirect would carry to wherever it points.
func TestNoRedirects(t *testing.T) {
	var leaked bool
	elsewhere := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		leaked = r.Header.Get("X-Api-Key") != ""
	}))
	defer elsewhere.Close()
	redirect := httptest.NewServer(http.RedirectHandler(elsewhere.URL+"/api/v3/system/status", http.StatusFound))
	defer redirect.Close()
	c, _ := arr.New(arr.Radarr, arr.Config{URL: redirect.URL, APIKey: "key"})
	if _, err := c.Status(context.Background()); err == nil || !strings.Contains(err.Error(), "redirects to "+elsewhere.URL+";") {
		t.Fatalf("redirect: %v", err)
	}
	if leaked {
		t.Fatal("followed the redirect with the key")
	}
}

// What a web page at the address means: the wrong address or port.
func TestNotTheAPI(t *testing.T) {
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<!doctype html><title>Router</title>"))
	}))
	defer page.Close()
	c, _ := arr.New(arr.Radarr, arr.Config{URL: page.URL, APIKey: "key"})
	if _, err := c.Status(context.Background()); err == nil || !strings.Contains(err.Error(), "doesn't look like Radarr") {
		t.Fatalf("web page: %v", err)
	}
}
