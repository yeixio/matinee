package main

import (
	"context"
	"encoding/json"
	"errors"
	"html"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

// waitingMessage is what people see while the server waits for its database.
const waitingMessage = "Matinee is waiting for its database"

// movingMessage is what they see while it moves its database (database.go).
const movingMessage = "Matinee is moving its database"

// moving is how far a database move is, while one runs; nil otherwise.
var moving atomic.Pointer[string]

// serveWhileWaiting answers on the server's addresses while it waits for a
// PostgreSQL server that doesn't answer yet, so the server doesn't give up
// and exit: an install nobody watches would stay down, and Kubernetes would
// hold off restarts for minutes after the database came back. It's alive
// (/healthz) but not ready (/readyz), so Kubernetes keeps it and sends no
// traffic; a browser that gets here anyway sees a page that says what's
// going on and checks again, and the apps get a 503 they retry.
//
// The returned stop closes it, before the real server takes the addresses.
func serveWhileWaiting(addrs []string, log *slog.Logger) (stop func()) {
	h := waitingHandler()
	var servers []*http.Server
	var listeners []net.Listener
	for _, addr := range addrs {
		if addr == "" {
			continue
		}
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			// The real server reports a busy address when it starts.
			log.Warn("can't answer while waiting for the database", "addr", addr, "err", err)
			continue
		}
		srv := &http.Server{Handler: h, ReadHeaderTimeout: 10 * time.Second}
		servers = append(servers, srv)
		listeners = append(listeners, ln)
		go func() {
			if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Warn("waiting page stopped", "addr", addr, "err", err)
			}
		}()
	}
	return func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		for _, s := range servers {
			s.Shutdown(ctx)
		}
		// Shutdown only closes listeners Serve has started on; one whose
		// goroutine hasn't run yet (a SQLite database opens at once) would
		// keep the address from the real server.
		for _, ln := range listeners {
			ln.Close()
		}
	}
}

func waitingHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/healthz":
			w.Write([]byte("ok"))
			return
		case r.URL.Path == "/readyz":
			http.Error(w, "waiting for the database", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Retry-After", "10")
		w.Header().Set("Cache-Control", "no-store")
		message, code, page := waitingMessage, "waiting_for_the_database", waitingPage
		if p := moving.Load(); p != nil {
			message, code = movingMessage, "moving_the_database"
			page = strings.Replace(movingPage, "{progress}", html.EscapeString(*p), 1)
		}
		if strings.HasPrefix(r.URL.Path, "/api/") || !strings.Contains(r.Header.Get("Accept"), "text/html") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]string{"error": message, "code": code})
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(page))
	})
}

const waitingPage = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta http-equiv="refresh" content="10">
<title>Matinee</title>
<style>
:root { color-scheme: light dark; --bg: #faf7f5; --fg: #1d1a19; --muted: #6b6461; --accent: #e8604c; }
@media (prefers-color-scheme: dark) { :root { --bg: #151312; --fg: #f3efed; --muted: #a39b98; } }
body { margin: 0; min-height: 100vh; display: grid; place-items: center; background: var(--bg); color: var(--fg);
  font: 16px/1.5 system-ui, -apple-system, "Segoe UI", sans-serif; }
main { max-width: 28rem; padding: 0 16px; text-align: center; }
h1 { font-size: 1.4rem; margin: 0 0 .5rem; }
p { color: var(--muted); margin: 0 0 .75rem; }
.dot { display: inline-block; width: .6rem; height: .6rem; border-radius: 50%; background: var(--accent);
  margin-right: .4rem; animation: pulse 1.4s ease-in-out infinite; }
@keyframes pulse { 50% { opacity: .25; } }
</style>
</head>
<body>
<main>
<h1><span class="dot"></span>Matinee is waiting for its database</h1>
<p>The server is running, but its PostgreSQL database isn't answering yet. Matinee keeps trying and opens as soon as it does.</p>
<p>This page checks again every few seconds.</p>
</main>
</body>
</html>
`

// movingPage is waitingPage for a database move an admin asked for.
var movingPage = strings.Replace(strings.Replace(waitingPage,
	"<h1><span class=\"dot\"></span>Matinee is waiting for its database</h1>\n<p>The server is running, but its PostgreSQL database isn't answering yet. Matinee keeps trying and opens as soon as it does.</p>",
	"<h1><span class=\"dot\"></span>Matinee is moving its database</h1>\n<p>An admin asked to move Matinee's database. It opens again as soon as everything is copied and checked. {progress}</p>", 1),
	`content="10"`, `content="5"`, 1)
