package api

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/yeixio/matinee/internal/scan"
	"github.com/yeixio/matinee/internal/store/storetest"
)

// Players hear that the server is restarting before it goes, from a request
// for commands they're already waiting on and from /health.
func TestDrainTellsPlayers(t *testing.T) {
	db := storetest.Open(t)
	srv := &Server{Store: db, Scanner: &scan.Scanner{Store: db}, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	admin := &client{t: t, base: ts.URL}
	var login struct{ Token string }
	admin.do("POST", "/api/v1/setup", credentials{"admin", "correct horse"}, &login)
	admin.token = login.Token

	type health struct{ State, BootID string }
	var before health
	if code := admin.do("GET", "/api/v1/health", nil, &before); code != 200 || before.State != "ok" || before.BootID == "" {
		t.Fatalf("health: %d %+v", code, before)
	}
	if res, _ := http.Get(ts.URL + "/readyz"); res.StatusCode != 200 {
		t.Fatalf("readyz: %d", res.StatusCode)
	}

	admin.do("PUT", "/api/v1/clients/web-1234567", map[string]any{"name": "Web", "platform": "web"}, nil)
	type polled struct {
		code int
		took time.Duration
		res  struct{ Restarting bool }
	}
	done := make(chan polled)
	go func() {
		var p polled
		start := time.Now()
		p.code = admin.do("GET", "/api/v1/clients/web-1234567/commands?wait=25", nil, &p.res)
		p.took = time.Since(start)
		done <- p
	}()
	time.Sleep(100 * time.Millisecond)
	srv.Drain()
	select {
	case p := <-done:
		if p.code != 200 || !p.res.Restarting || p.took > 5*time.Second {
			t.Fatalf("poll on drain: %+v", p)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a waiting poll didn't hear about the restart")
	}

	var during health
	admin.do("GET", "/api/v1/health", nil, &during)
	if during.State != "restarting" || during.BootID != before.BootID {
		t.Fatalf("health while draining: %+v", during)
	}
	res, err := http.Get(ts.URL + "/api/v1/server")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.Header.Get(HeaderRestarting) != "1" {
		t.Fatalf("answers while draining lack %s", HeaderRestarting)
	}
	if res, _ := http.Get(ts.URL + "/readyz"); res.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("readyz while draining: %d", res.StatusCode)
	}
}
