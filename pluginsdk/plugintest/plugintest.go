//go:build !wasip1

// Package plugintest helps test a Matinee plugin's server part with go
// test, outside Matinee: it answers the plugin's fetches from an
// http.Handler, as Matinee would from the internet, and collects what the
// plugin logs. The plugin's handlers are plain functions, so a test calls
// them directly:
//
//	func TestLookup(t *testing.T) {
//		plugintest.Network(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
//			fmt.Fprint(w, `{"poster": "https://example.com/p.png"}`)
//		}))
//		d, err := lookup(plugin.LookupRequest{Kind: "game", Title: "Pac-Man"})
//		...
//	}
package plugintest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	plugin "github.com/yeixio/matinee/pluginsdk"
)

// Network answers the plugin's fetches with h until the test ends, as if h
// were every host the plugin may reach. Like Matinee, it refuses URLs that
// aren't https, and hosts the plugin's plugin.json doesn't list under
// permissions.network, when it finds plugin.json in the test's folder or
// one above it in the same module.
func Network(t testing.TB, h http.Handler) {
	t.Helper()
	hosts, checked := manifestHosts()
	plugin.TestFetch = func(req plugin.Request) (*plugin.Response, error) {
		u, err := url.Parse(req.URL)
		switch {
		case err != nil:
			return nil, err
		case u.Scheme != "https":
			return nil, fmt.Errorf("only https URLs can be fetched")
		case checked && !allows(hosts, u.Hostname()):
			return nil, fmt.Errorf("%s isn't in the plugin's network permissions", u.Hostname())
		}
		method := req.Method
		if method == "" {
			method = http.MethodGet
		}
		r := httptest.NewRequest(method, req.URL, bytes.NewReader(req.Body))
		for k, v := range req.Headers {
			r.Header.Set(k, v)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		res := rec.Result()
		body, _ := io.ReadAll(res.Body)
		headers := map[string]string{}
		for k := range res.Header {
			headers[strings.ToLower(k)] = res.Header.Get(k)
		}
		return &plugin.Response{Status: res.StatusCode, Headers: headers, Body: body}, nil
	}
	t.Cleanup(func() { plugin.TestFetch = nil })
}

// manifestHosts reads permissions.network from the nearest plugin.json.
func manifestHosts() ([]string, bool) {
	dir, err := os.Getwd()
	if err != nil {
		return nil, false
	}
	for {
		b, err := os.ReadFile(filepath.Join(dir, "plugin.json"))
		if err == nil {
			var m struct {
				Permissions struct {
					Network []string `json:"network"`
				} `json:"permissions"`
			}
			if json.Unmarshal(b, &m) != nil {
				return nil, false
			}
			return m.Permissions.Network, true
		}
		// The plugin's own module ends the search.
		_, mod := os.Stat(filepath.Join(dir, "go.mod"))
		up := filepath.Dir(dir)
		if mod == nil || up == dir {
			return nil, false
		}
		dir = up
	}
}

func allows(hosts []string, host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	for _, h := range hosts {
		if h == host || strings.HasPrefix(h, "*.") && strings.HasSuffix(host, h[1:]) {
			return true
		}
	}
	return false
}

// Log is what the plugin logged during a test.
type Log struct {
	mu    sync.Mutex
	lines []string
}

// Logs collects the plugin's log lines until the test ends, and writes them
// to the test's output too, shown with go test -v or when it fails.
func Logs(t testing.TB) *Log {
	t.Helper()
	l := &Log{}
	levels := []string{"debug", "info", "warn", "error"}
	plugin.TestLog = func(level int, msg string) {
		l.mu.Lock()
		l.lines = append(l.lines, msg)
		l.mu.Unlock()
		t.Logf("plugin %s: %s", levels[min(max(level, 0), 3)], msg)
	}
	t.Cleanup(func() { plugin.TestLog = nil })
	return l
}

// Lines are the messages logged so far.
func (l *Log) Lines() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.lines...)
}
