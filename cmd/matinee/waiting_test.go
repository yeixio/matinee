package main

import (
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// While the server waits for its database it's alive but not ready, the
// apps get a JSON 503 and a browser gets a page that says what's going on.
func TestWaitingHandler(t *testing.T) {
	h := waitingHandler()
	get := func(path, accept string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		if accept != "" {
			r.Header.Set("Accept", accept)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if w := get("/healthz", ""); w.Code != http.StatusOK {
		t.Errorf("/healthz = %d", w.Code)
	}
	if w := get("/readyz", ""); w.Code != http.StatusServiceUnavailable {
		t.Errorf("/readyz = %d", w.Code)
	}
	w := get("/api/v1/home", "application/json")
	if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), "waiting_for_the_database") || w.Header().Get("Retry-After") == "" {
		t.Errorf("api = %d %q", w.Code, w.Body.String())
	}
	w = get("/movies", "text/html,application/xhtml+xml")
	if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), "waiting for its database") {
		t.Errorf("page = %d", w.Code)
	}
}

// Stopping the waiting page frees its addresses at once, even when it's
// stopped before it started serving (a SQLite database opens straight away).
func TestStopWaitingFreesTheAddress(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	for range 50 {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		addr := ln.Addr().String()
		ln.Close()
		serveWhileWaiting([]string{addr}, log)()
		again, err := net.Listen("tcp", addr)
		if err != nil {
			t.Fatalf("the address is still taken: %v", err)
		}
		again.Close()
	}
}
