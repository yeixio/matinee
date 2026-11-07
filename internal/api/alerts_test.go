package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/yeixio/matinee/internal/alerts"
	"github.com/yeixio/matinee/internal/store"
)

// Admins set up alerts, the secrets stay on the server, and a test alert
// reaches the webhook.
func TestAlertSettings(t *testing.T) {
	var mu sync.Mutex
	var got []map[string]any
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var v map[string]any
		json.NewDecoder(r.Body).Decode(&v)
		mu.Lock()
		got = append(got, v)
		mu.Unlock()
	}))
	defer hook.Close()

	admin, _ := newTestServer(t)
	admin.do("POST", "/api/v1/setup", credentials{"admin", "correct horse"}, nil)
	admin, _ = tryLogin(t, admin, "admin", "correct horse")
	var bob store.User
	admin.do("POST", "/api/v1/users", map[string]any{"username": "bob", "password": "bob password"}, &bob)
	user, _ := tryLogin(t, admin, "bob", "bob password")
	for _, path := range []string{"/api/v1/admin/alerts", "/api/v1/admin/metrics"} {
		if code := user.do("GET", path, nil, nil); code != http.StatusForbidden {
			t.Fatalf("%s for someone who isn't an admin: %d", path, code)
		}
	}

	var v alertsView
	if code := admin.do("GET", "/api/v1/admin/alerts", nil, &v); code != http.StatusOK {
		t.Fatalf("get: %d", code)
	}
	if !v.Rules.Disk.On || v.Rules.Disk.Percent != 90 || len(v.Webhooks) != 0 || v.Email.Port != 587 {
		t.Fatalf("defaults: %+v", v)
	}
	if code := admin.do("POST", "/api/v1/admin/alerts/test", nil, nil); code != http.StatusConflict {
		t.Fatalf("test with nowhere to send: %d", code)
	}

	bad := alerts.Config{Rules: alerts.DefaultRules(), Webhooks: []alerts.Webhook{{Kind: alerts.KindDiscord, URL: "discord.com/x"}}}
	if code := admin.do("PUT", "/api/v1/admin/alerts", bad, nil); code != http.StatusBadRequest {
		t.Fatalf("an address without https://: %d", code)
	}
	badMail := alerts.Config{Rules: alerts.DefaultRules(), Email: alerts.Email{On: true, Host: "mail.example.com", From: "me@example.com"}}
	if code := admin.do("PUT", "/api/v1/admin/alerts", badMail, nil); code != http.StatusBadRequest {
		t.Fatalf("email with no one to send to: %d", code)
	}

	secret := hook.URL + "/api/webhook/secret-id"
	c := alerts.Config{
		Rules:    alerts.Rules{Disk: alerts.DiskRule{On: true, Percent: 80}, Errors: alerts.RateRule{On: true, Count: 5, Minutes: 10}},
		Webhooks: []alerts.Webhook{{Kind: alerts.KindHomeAssistant, URL: secret}},
		Email:    alerts.Email{Host: "mail.example.com", Username: "me", Password: "hunter2", From: "me@example.com", To: "me@example.com"},
	}
	if code := admin.do("PUT", "/api/v1/admin/alerts", c, &v); code != http.StatusOK {
		t.Fatalf("put: %d", code)
	}
	if len(v.Webhooks) != 1 || v.Webhooks[0].ID == "" || !strings.HasSuffix(v.Webhooks[0].Hint, "/…t-id") || v.Rules.Disk.Percent != 80 || v.Rules.Restart.On {
		t.Fatalf("saved: %+v", v)
	}
	if !v.Email.HasPassword || v.Email.Password != "" {
		t.Fatalf("email password: %+v", v.Email)
	}
	raw, _ := json.Marshal(v)
	if strings.Contains(string(raw), "secret-id") || strings.Contains(string(raw), "hunter2") {
		t.Fatalf("a secret left the server: %s", raw)
	}

	// Saving again without the address or password keeps them.
	again := alerts.Config{Rules: v.Rules, Webhooks: []alerts.Webhook{{ID: v.Webhooks[0].ID, Kind: alerts.KindHomeAssistant, Name: "Home Assistant"}},
		Email: alerts.Email{Host: "mail.example.com", Username: "me", From: "me@example.com", To: "me@example.com"}}
	if code := admin.do("PUT", "/api/v1/admin/alerts", again, &v); code != http.StatusOK {
		t.Fatalf("put again: %d", code)
	}
	saved, _ := admin.srv.alerts().Config(t.Context())
	if saved.Webhooks[0].URL != secret || saved.Email.Password != "hunter2" || saved.Webhooks[0].Name != "Home Assistant" {
		t.Fatalf("kept: %+v", saved)
	}

	var a alerts.Alert
	if code := admin.do("POST", "/api/v1/admin/alerts/test", nil, &a); code != http.StatusOK {
		t.Fatalf("test: %d", code)
	}
	if a.Sent != 1 || len(a.Failed) != 0 || len(got) != 1 || got[0]["title"] != "Test alert" || got[0]["rule"] != "test" {
		t.Fatalf("test alert %+v, webhook got %v", a, got)
	}
	admin.do("GET", "/api/v1/admin/alerts", nil, &v)
	if len(v.History) != 1 || v.History[0].Title != "Test alert" {
		t.Fatalf("history: %+v", v.History)
	}
}

// Prometheus metrics are off until an admin makes a token, and need it.
func TestMetrics(t *testing.T) {
	admin, _ := newTestServer(t, func(s *Server) { s.Storage = []StoragePath{{Use: "data", Path: s.MediaRoot}} })
	admin.do("POST", "/api/v1/setup", credentials{"admin", "correct horse"}, nil)
	admin, _ = tryLogin(t, admin, "admin", "correct horse")
	get := func(path, token string) (int, string) {
		req, _ := http.NewRequest("GET", admin.base+path, nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(b)
	}
	scrape := func(token string) (int, string) { return get("/metrics", token) }
	if code, _ := scrape(""); code != http.StatusNotFound {
		t.Fatalf("metrics while off: %d", code)
	}
	var m metricsView
	if code := admin.do("POST", "/api/v1/admin/metrics/token", nil, &m); code != http.StatusOK || !m.On || len(m.Token) < 32 {
		t.Fatalf("turn on: %d %+v", code, m)
	}
	if code, _ := scrape("wrong"); code != http.StatusUnauthorized {
		t.Fatalf("wrong token: %d", code)
	}
	if code, _ := scrape(admin.token); code != http.StatusUnauthorized {
		t.Fatalf("an admin's sign-in token isn't the metrics token: %d", code)
	}
	code, body := scrape(m.Token)
	if code != http.StatusOK {
		t.Fatalf("scrape: %d %s", code, body)
	}
	for _, want := range []string{"# TYPE matinee_http_requests_total counter", "matinee_build_info{version=", "matinee_disk_free_bytes{path=", `matinee_alerts_firing{rule="disk"} 0`, "matinee_transcodes_failed_total 0",
		"matinee_ffmpeg_processes 0", "matinee_transcodes_superseded_total 0", `matinee_transcode_start_seconds_bucket{le="+Inf"} 0`,
		"# TYPE matinee_segment_wait_seconds histogram", "matinee_heap_live_bytes "} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics lack %q:\n%s", want, body)
		}
	}

	// So does the profiler, which is how a slow or leaking server is looked into.
	if code, _ := get("/debug/pprof/goroutine?debug=1", "wrong"); code != http.StatusUnauthorized {
		t.Fatalf("profiler with a wrong token: %d", code)
	}
	if code, body := get("/debug/pprof/goroutine?debug=1", m.Token); code != http.StatusOK || !strings.Contains(body, "goroutine profile:") {
		t.Fatalf("goroutine profile: %d %.200s", code, body)
	}
	if code, _ := get("/debug/pprof/cmdline", m.Token); code != http.StatusNotFound {
		t.Fatalf("command line: %d", code)
	}

	// The dashboard summary takes the same token.
	if code, _ := get("/stats", "wrong"); code != http.StatusUnauthorized {
		t.Fatalf("stats with a wrong token: %d", code)
	}
	code, body = get("/stats", m.Token)
	var st stats
	if code != http.StatusOK || json.Unmarshal([]byte(body), &st) != nil || st.Users != 1 || st.Playing != 0 || st.Movies != 0 {
		t.Fatalf("stats: %d %s", code, body)
	}

	if code := admin.do("DELETE", "/api/v1/admin/metrics/token", nil, nil); code != http.StatusNoContent {
		t.Fatalf("turn off: %d", code)
	}
	if code, _ := scrape(m.Token); code != http.StatusNotFound {
		t.Fatalf("metrics after turning off: %d", code)
	}
	if code, _ := get("/stats", m.Token); code != http.StatusNotFound {
		t.Fatalf("stats after turning off: %d", code)
	}
}

func TestMetricsLabels(t *testing.T) {
	var b metricsWriter
	b.sample("x", 1.5, "path", `C:\media "new"`)
	if got := b.String(); got != `x{path="C:\\media \"new\""} 1.5`+"\n" {
		t.Fatalf("%q", got)
	}
}
