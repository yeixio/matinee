package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yeixio/matinee/internal/gamestream"
	"github.com/yeixio/matinee/internal/xboxremote"
)

// fakeMicrosoft signs in and lists one console.
func fakeMicrosoft(t *testing.T) *httptest.Server {
	var mu sync.Mutex
	polls := 0
	srv := httptest.NewServer(nil)
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		r.ParseForm()
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		enc := json.NewEncoder(w)
		switch r.URL.Path {
		case "/consumers/oauth2/v2.0/devicecode":
			enc.Encode(map[string]any{"user_code": "XBOX1234", "device_code": "dev", "verification_uri": "https://www.microsoft.com/link", "expires_in": 60, "interval": 1})
		case "/consumers/oauth2/v2.0/token":
			if r.PostForm.Get("grant_type") == "refresh_token" {
				enc.Encode(map[string]any{"access_token": "a", "refresh_token": "rotated"})
				return
			}
			if polls++; polls < 2 {
				w.WriteHeader(400)
				enc.Encode(map[string]string{"error": "authorization_pending"})
				return
			}
			enc.Encode(map[string]any{"access_token": "a", "refresh_token": "first"})
		case "/user/authenticate":
			enc.Encode(map[string]any{"Token": "user"})
		case "/xsts/authorize":
			enc.Encode(map[string]any{"Token": "x", "DisplayClaims": map[string]any{"xui": []any{map[string]string{"gtg": "Couch Player"}}}})
		case "/v2/login/user":
			enc.Encode(map[string]any{"gsToken": "gs", "durationInSeconds": 3600,
				"offeringSettings": map[string]any{"regions": []any{map[string]any{"baseUri": srv.URL, "isDefault": true}}}})
		case "/v6/servers/home":
			enc.Encode(map[string]any{"results": []any{map[string]string{"deviceName": "Den", "serverId": "F1", "powerState": "On", "consoleType": "XboxSeriesS"}}})
		default:
			t.Errorf("unexpected %s %s %s", r.Method, r.URL.Path, body)
			w.WriteHeader(404)
		}
	})
	t.Cleanup(srv.Close)
	return srv
}

func TestXboxSetup(t *testing.T) {
	ms := fakeMicrosoft(t)
	games, err := gamestream.NewManager(gamestream.Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer games.Close()
	u := ms.URL
	admin, _, db := newTestServerStore(t, func(s *Server) {
		s.Games = games
		s.Xbox = &xboxremote.Client{End: xboxremote.Endpoints{Login: u, Live: u, UserAuth: u, XSTS: u, XHome: u}}
	})
	signInAdmin(t, admin)

	type info struct {
		Available, SignedIn bool
		Gamertag            string
		SignIn              *struct{ Code, URL string }
		Consoles            []xboxremote.Console
		Error               string
	}
	var got info
	if code := admin.do("GET", "/api/v1/games/xbox", nil, &got); code != 200 || !got.Available || got.SignedIn || got.SignIn != nil {
		t.Fatalf("info: %d %+v", code, got)
	}
	if code := admin.do("POST", "/api/v1/games/play", map[string]any{"xbox": "F1", "direct": true}, nil); code != 404 {
		t.Fatalf("play before sign-in: %d", code)
	}
	var started struct{ Code, URL string }
	if code := admin.do("POST", "/api/v1/games/xbox/signin", nil, &started); code != 200 || started.Code != "XBOX1234" || started.URL != "https://www.microsoft.com/link" {
		t.Fatalf("sign in: %d %+v", code, started)
	}
	if admin.do("GET", "/api/v1/games/xbox", nil, &got); got.SignIn == nil || got.SignIn.Code != "XBOX1234" {
		t.Fatalf("waiting sign-in not shown: %+v", got)
	}
	deadline := time.Now().Add(10 * time.Second)
	for !got.SignedIn {
		if time.Now().After(deadline) {
			t.Fatalf("never signed in: %+v", got)
		}
		time.Sleep(100 * time.Millisecond)
		got = info{}
		admin.do("GET", "/api/v1/games/xbox", nil, &got)
	}
	if got.Gamertag != "Couch Player" || got.SignIn != nil || len(got.Consoles) != 1 || got.Consoles[0].Name != "Den" || got.Consoles[0].State != "ready" {
		t.Fatalf("signed in: %+v", got)
	}
	// The refresh token Microsoft hands back replaces the first, and it
	// never reaches players.
	if v, _ := db.Setting(context.Background(), xboxKey); !strings.Contains(v, `"rotated"`) {
		t.Errorf("setting = %s", v)
	}
	var raw json.RawMessage
	admin.do("GET", "/api/v1/games/xbox", nil, &raw)
	if strings.Contains(string(raw), "rotated") || strings.Contains(string(raw), "first") {
		t.Errorf("the sign-in reached the browser: %s", raw)
	}
	if code := admin.do("POST", "/api/v1/games/play", map[string]any{"xbox": "nope", "direct": true}, nil); code != 404 {
		t.Fatalf("unknown console: %d", code)
	}
	if code := admin.do("DELETE", "/api/v1/games/xbox/account", nil, nil); code != 204 {
		t.Fatalf("sign out: %d", code)
	}
	got = info{}
	if admin.do("GET", "/api/v1/games/xbox", nil, &got); got.SignedIn {
		t.Fatalf("still signed in: %+v", got)
	}

	// A server that can't play games doesn't offer it.
	plain, _ := newTestServer(t)
	signInAdmin(t, plain)
	got = info{}
	if code := plain.do("GET", "/api/v1/games/xbox", nil, &got); code != 200 || got.Available {
		t.Fatalf("no games: %d %+v", code, got)
	}
}
