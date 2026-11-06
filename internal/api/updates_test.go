package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/yeixio/matinee/internal/update"
)

// Admins see the running version and any newer one, and choose the channel.
func TestUpdates(t *testing.T) {
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/releases/latest":
			fmt.Fprint(w, `{"tag_name":"v0.2.3","html_url":"https://github.com/yeixio/matinee/releases/tag/v0.2.3"}`)
		case "/branches/edge":
			fmt.Fprint(w, `{"commit":{"sha":"2222222bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}}`)
		case "/compare/v0.2.2...2222222bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb":
			fmt.Fprint(w, `{"status":"ahead","ahead_by":3}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer gh.Close()
	admin, _, _ := newTestServerStore(t, func(s *Server) {
		s.Updates = &update.Manager{
			Store: s.Store, Build: update.ParseBuild("v0.2.2"), GitHub: &update.GitHub{Base: gh.URL},
			Install: update.Install{Kind: update.KindDocker, Channels: []string{update.ChannelRelease, update.ChannelMain}},
		}
	})
	admin.do("POST", "/api/v1/setup", credentials{"admin", "correct horse"}, nil)
	admin, _ = tryLogin(t, admin, "admin", "correct horse")
	admin.do("POST", "/api/v1/users", map[string]any{"username": "bob", "password": "bob password"}, nil)
	user, _ := tryLogin(t, admin, "bob", "bob password")
	if code := user.do("GET", "/api/v1/admin/updates", nil, nil); code != http.StatusForbidden {
		t.Fatalf("not an admin: %d", code)
	}

	var st update.Status
	if code := admin.do("GET", "/api/v1/admin/updates", nil, &st); code != http.StatusOK || st.Settings != update.DefaultSettings || st.Build.Release != "0.2.2" || st.Available != nil {
		t.Fatalf("get: %d %+v", code, st)
	}
	if code := admin.do("POST", "/api/v1/admin/updates/check", nil, &st); code != http.StatusOK || st.Available == nil || st.Available.Version != "v0.2.3" || st.Checked.IsZero() {
		t.Fatalf("check: %d %+v", code, st)
	}
	// A container can't install it; it says why rather than failing.
	if code := admin.do("POST", "/api/v1/admin/updates/apply", nil, &st); code != http.StatusOK || st.LastError == "" || st.Applying != "" {
		t.Fatalf("apply in a container: %d %+v", code, st)
	}
	if code := admin.do("PUT", "/api/v1/admin/updates", update.Settings{Channel: "main", Auto: true}, &st); code != http.StatusOK ||
		st.Settings != (update.Settings{Channel: "main", Auto: true}) || st.Available == nil || st.Available.Version != "main-2222222" {
		t.Fatalf("main: %d %+v", code, st)
	}
	if code := admin.do("PUT", "/api/v1/admin/updates", map[string]any{"channel": "nightly"}, nil); code != http.StatusBadRequest {
		t.Fatalf("an unknown channel: %d", code)
	}
}
