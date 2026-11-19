package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yeixio/matinee/internal/gamestream"
	"github.com/yeixio/matinee/internal/psremote"
)

// fakePSRemote stands in for matinee-psremote.
const fakePSRemote = `#!/bin/sh
case "$1" in
status) if [ "$2" = 192.168.1.20 ]; then echo '{"state":"standby","ps5":true,"name":"PS5-123"}'; else echo '{"error":"no PlayStation answered at '"$2"'"}'; exit 1; fi ;;
regist)
	if [ "$MATINEE_PS_PIN" != 12345678 ] || [ "$MATINEE_PS_ACCOUNT" != 0102030405060708 ]; then echo '{"error":"the console didn'"'"'t accept the pairing"}'; exit 1; fi
	echo '{"name":"PS5-123","mac":"aa:bb:cc:dd:ee:ff","target":1000100,"registKey":"3132333400000000000000000000000a","rpKey":"00112233445566778899aabbccddeeff"}' ;;
wake) echo "$MATINEE_PS_REGIST_KEY" > "$(dirname "$0")/woken"; echo '{}' ;;
esac
`

func TestPlayStationSetup(t *testing.T) {
	dir := t.TempDir()
	bridge := filepath.Join(dir, "matinee-psremote")
	if err := os.WriteFile(bridge, []byte(fakePSRemote), 0o755); err != nil {
		t.Fatal(err)
	}
	games, err := gamestream.NewManager(gamestream.Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer games.Close()
	admin, _ := newTestServer(t, func(s *Server) {
		s.Games = games
		s.PlayStation = &psremote.Bridge{Path: bridge}
	})
	signInAdmin(t, admin)

	var info struct {
		Available, SignedIn bool
		SignInURL           string `json:"signInUrl"`
		Consoles            []consoleView
	}
	if code := admin.do("GET", "/api/v1/games/playstation", nil, &info); code != 200 || !info.Available || info.SignedIn || !strings.Contains(info.SignInURL, "sonyentertainmentnetwork") {
		t.Fatalf("info: %d %+v", code, info)
	}
	// Pairing needs the account first.
	pair := map[string]any{"host": "192.168.1.20", "ps5": true, "pin": "12345678"}
	if code := admin.do("POST", "/api/v1/games/playstation/consoles", pair, nil); code != 400 {
		t.Fatalf("pair before sign-in: %d", code)
	}
	if code := admin.do("PUT", "/api/v1/games/playstation/account", map[string]string{"accountId": "nope"}, nil); code != 400 {
		t.Fatalf("bad account: %d", code)
	}
	if code := admin.do("PUT", "/api/v1/games/playstation/account", map[string]string{"redirect": "https://example.com/?x=1"}, nil); code != 400 {
		t.Fatalf("redirect without a code: %d", code)
	}
	if code := admin.do("PUT", "/api/v1/games/playstation/account", map[string]string{"accountId": "AQIDBAUGBwg="}, nil); code != 204 {
		t.Fatalf("account: %d", code)
	}
	// Only consoles on the home network.
	if code := admin.do("POST", "/api/v1/games/playstation/consoles", map[string]any{"host": "8.8.8.8", "pin": "12345678"}, nil); code != 400 {
		t.Fatalf("pair outside: %d", code)
	}
	if code := admin.do("POST", "/api/v1/games/playstation/consoles", map[string]any{"host": "192.168.1.20", "ps5": true, "pin": "11111111"}, nil); code != 400 {
		t.Fatalf("wrong code: %d", code)
	}
	var paired consoleView
	if code := admin.do("POST", "/api/v1/games/playstation/consoles", pair, &paired); code != 200 || paired.Name != "PS5-123" || paired.ID == "" {
		t.Fatalf("pair: %d %+v", code, paired)
	}
	// Pairing it again replaces it.
	if code := admin.do("POST", "/api/v1/games/playstation/consoles", pair, &paired); code != 200 {
		t.Fatalf("pair again: %d", code)
	}
	if code := admin.do("GET", "/api/v1/games/playstation", nil, &info); code != 200 || !info.SignedIn || len(info.Consoles) != 1 ||
		info.Consoles[0].State != "standby" || info.Consoles[0].ID != paired.ID {
		t.Fatalf("after pairing: %d %+v", code, info)
	}
	// Its keys never leave the server.
	var rawMsg json.RawMessage
	admin.do("GET", "/api/v1/games/playstation", nil, &rawMsg)
	raw := string(rawMsg)
	if strings.Contains(raw, "registKey") || strings.Contains(raw, "0102030405060708") || strings.Contains(raw, "00112233") {
		t.Fatalf("keys shown: %s", raw)
	}
	if code := admin.do("POST", "/api/v1/games/playstation/consoles/"+paired.ID+"/wake", nil, nil); code != 204 {
		t.Fatalf("wake: %d", code)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "woken")); strings.TrimSpace(string(b)) != "3132333400000000000000000000000a" {
		t.Fatalf("woken with %q", b)
	}
	if code := admin.do("POST", "/api/v1/games/play", map[string]any{"playstation": "nope", "direct": true}, nil); code != 404 {
		t.Fatalf("play unknown console: %d", code)
	}
	if code := admin.do("DELETE", "/api/v1/games/playstation/consoles/"+paired.ID, nil, nil); code != 204 {
		t.Fatalf("remove: %d", code)
	}
	if admin.do("GET", "/api/v1/games/playstation", nil, &info); len(info.Consoles) != 0 {
		t.Fatalf("still there: %+v", info.Consoles)
	}
	if code := admin.do("DELETE", "/api/v1/games/playstation/account", nil, nil); code != 204 {
		t.Fatalf("forget: %d", code)
	}
	if admin.do("GET", "/api/v1/games/playstation", nil, &info); info.SignedIn {
		t.Fatal("still signed in")
	}
}

func TestPlayStationUnavailable(t *testing.T) {
	admin, _ := newTestServer(t)
	signInAdmin(t, admin)
	var info struct{ Available bool }
	if code := admin.do("GET", "/api/v1/games/playstation", nil, &info); code != 200 || info.Available {
		t.Fatalf("info: %d %+v", code, info)
	}
	if code := admin.do("POST", "/api/v1/games/playstation/consoles", map[string]any{"host": "192.168.1.20"}, nil); code != 404 {
		t.Fatalf("pair: %d", code)
	}
}

func signInAdmin(t *testing.T, c *client) {
	t.Helper()
	var login struct{ Token string }
	if code := c.do("POST", "/api/v1/setup", credentials{"admin", "correct horse"}, &login); code != 200 {
		t.Fatalf("setup: %d", code)
	}
	c.token = login.Token
}
