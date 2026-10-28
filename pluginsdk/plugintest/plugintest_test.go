//go:build !wasip1

package plugintest

import (
	"fmt"
	"net/http"
	"slices"
	"testing"

	plugin "github.com/yeixio/matinee/pluginsdk"
)

func TestNetwork(t *testing.T) {
	Network(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprintf(w, "%s %s %s", r.Method, r.Host, r.Header.Get("X-Key"))
	}))
	res, err := plugin.Fetch(plugin.Request{URL: "https://api.example.com/x", Headers: map[string]string{"X-Key": "k"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != 200 || string(res.Body) != "GET api.example.com k" || res.Headers["content-type"] != "text/plain" {
		t.Errorf("got %d %q %v", res.Status, res.Body, res.Headers)
	}
	if _, err := plugin.Get("http://api.example.com/x"); err == nil {
		t.Error("an http URL was fetched")
	}
}

func TestLogs(t *testing.T) {
	l := Logs(t)
	plugin.Info("found %d games", 3)
	if !slices.Equal(l.Lines(), []string{"found 3 games"}) {
		t.Errorf("got %v", l.Lines())
	}
}

func TestAllows(t *testing.T) {
	hosts := []string{"example.com", "*.images.example.com"}
	for host, want := range map[string]bool{"example.com": true, "a.images.example.com": true, "images.example.com": false, "evil.com": false} {
		if allows(hosts, host) != want {
			t.Errorf("%s: want %v", host, want)
		}
	}
}
