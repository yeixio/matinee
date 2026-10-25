package api

import (
	"net/http"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/yeixio/matinee/internal/arr"
	"github.com/yeixio/matinee/internal/arr/arrtest"
	"github.com/yeixio/matinee/internal/store"
)

func TestRadarrAndSonarr(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	admin, tmp := newTestServer(t)
	var login struct{ Token string }
	admin.do("POST", "/api/v1/setup", credentials{"admin", "correct horse"}, &login)
	admin.token = login.Token
	admin.do("POST", "/api/v1/users", map[string]any{"username": "ellen", "password": "password123"}, nil)
	ellen := &client{t: t, base: admin.base}
	admin.do("POST", "/api/v1/auth/login", credentials{"ellen", "password123"}, &login)
	ellen.token = login.Token
	guest := &client{t: t, base: admin.base} // at home, not signed in

	radarr := arrtest.New(t, arr.Radarr)
	radarr.Catalog = []map[string]any{
		{"tmdbId": 693134, "title": "Dune: Part Two", "year": 2024, "images": []map[string]any{{"coverType": "poster", "remoteUrl": "https://image.tmdb.org/dune.jpg"}}},
		{"tmdbId": 329865, "title": "Arrival", "year": 2016},
	}
	arrivalID := radarr.Add(map[string]any{"tmdbId": 329865, "title": "Arrival", "year": 2016, "qualityProfileId": 1, "hasFile": true, "movieFile": map[string]any{"id": 7}})
	sonarr := arrtest.New(t, arr.Sonarr)
	sonarr.Catalog = []map[string]any{
		{"tvdbId": 371980, "tmdbId": 95396, "title": "Severance", "year": 2022, "seasons": []map[string]any{{"seasonNumber": 0}, {"seasonNumber": 1}}},
	}

	// Nothing is connected at first, and there's nothing to ask for.
	var opts requestOptions
	ellen.do("GET", "/api/v1/requests/options", nil, &opts)
	if opts.Movies || opts.CanRequest || !opts.CanReport {
		t.Fatalf("options before connecting: %+v", opts)
	}

	// Connecting checks the address and key.
	if code := admin.do("POST", "/api/v1/integrations/radarr/test", arr.Config{URL: radarr.URL, APIKey: "wrong"}, nil); code != http.StatusBadGateway {
		t.Fatalf("test with a wrong key: %d", code)
	}
	if code := admin.do("PUT", "/api/v1/integrations/radarr", arr.Config{URL: sonarr.URL, APIKey: sonarr.Key}, nil); code == http.StatusOK {
		t.Fatal("connected Sonarr as Radarr")
	}
	var checked connectionCheck
	if code := admin.do("POST", "/api/v1/integrations/radarr/test", arr.Config{URL: radarr.URL, APIKey: radarr.Key}, &checked); code != 200 || len(checked.QualityProfiles) != 2 {
		t.Fatalf("test: %d %+v", code, checked)
	}
	if code := ellen.do("PUT", "/api/v1/integrations/radarr", arr.Config{URL: radarr.URL, APIKey: radarr.Key}, nil); code != http.StatusForbidden {
		t.Fatalf("non-admin connecting: %d", code)
	}
	var iv integrationView
	if code := admin.do("PUT", "/api/v1/integrations/radarr", arr.Config{URL: radarr.URL, APIKey: radarr.Key}, &iv); code != 200 {
		t.Fatalf("connect radarr: %d", code)
	}
	if !iv.Connected || !iv.HasAPIKey || iv.QualityProfileID != 1 || iv.RootFolder != "/media/movie" {
		t.Fatalf("radarr connection: %+v", iv)
	}
	admin.do("PUT", "/api/v1/integrations/sonarr", arr.Config{URL: sonarr.URL, APIKey: sonarr.Key}, nil)
	// The key never leaves the server.
	if body := get(t, admin, "/api/v1/integrations"); strings.Contains(body, radarr.Key) || strings.Contains(body, sonarr.Key) {
		t.Fatalf("integrations show the API key: %s", body)
	}
	// Leaving the key out keeps it, but only for the same address.
	if code := admin.do("PUT", "/api/v1/integrations/radarr", arr.Config{URL: radarr.URL, QualityProfileID: 4}, &iv); code != 200 || iv.QualityProfileID != 4 {
		t.Fatalf("change default profile: %d %+v", code, iv)
	}
	if code := admin.do("PUT", "/api/v1/integrations/radarr", arr.Config{URL: "http://elsewhere.example"}, nil); code != http.StatusBadRequest {
		t.Fatalf("new address without the key: %d", code)
	}
	admin.do("PUT", "/api/v1/integrations/radarr", arr.Config{URL: radarr.URL, QualityProfileID: 1}, nil)

	// People ask; an admin approves.
	ellen.do("GET", "/api/v1/requests/options", nil, &opts)
	if !opts.Movies || !opts.Shows || !opts.CanRequest || opts.AutoApprove || len(opts.QualityProfiles["movie"]) != 2 || opts.DefaultQualityProfile["movie"] != 1 {
		t.Fatalf("options: %+v", opts)
	}
	var found []titleView
	ellen.do("GET", "/api/v1/requests/search?kind=movie&q=dune", nil, &found)
	if len(found) != 1 || found[0].TMDBID != 693134 || found[0].Status != "" || found[0].Poster == "" {
		t.Fatalf("search: %+v", found)
	}
	var req requestView
	if code := ellen.do("POST", "/api/v1/requests", map[string]any{"kind": "movie", "tmdbId": 693134}, &req); code != http.StatusCreated || req.State != store.RequestPending || req.Title != "Dune: Part Two" {
		t.Fatalf("request: %d %+v", code, req)
	}
	if code := ellen.do("POST", "/api/v1/requests", map[string]any{"kind": "movie", "tmdbId": 693134}, nil); code != http.StatusConflict {
		t.Fatalf("asking twice: %d", code)
	}
	ellen.do("GET", "/api/v1/requests/search?kind=movie&q=dune", nil, &found)
	if found[0].Status != titleRequested {
		t.Fatalf("search after asking: %+v", found)
	}
	// What Radarr already has can't be asked for.
	ellen.do("GET", "/api/v1/requests/search?kind=movie&q=arrival", nil, &found)
	if len(found) != 1 || found[0].Status != titleAdded {
		t.Fatalf("search for a title Radarr has: %+v", found)
	}
	if code := guest.do("POST", "/api/v1/requests", map[string]any{"kind": "movie", "tmdbId": 693134}, nil); code != http.StatusUnauthorized {
		t.Fatalf("guest asking: %d", code)
	}
	guest.do("GET", "/api/v1/requests/options", nil, &opts)
	if opts.CanRequest || opts.CanReport {
		t.Fatalf("guest options: %+v", opts)
	}
	if code := ellen.do("POST", "/api/v1/requests/"+req.ID+"/approve", nil, nil); code != http.StatusForbidden {
		t.Fatalf("approving your own request: %d", code)
	}

	admin.do("GET", "/api/v1/requests/options", nil, &opts)
	if opts.PendingRequests != 1 {
		t.Fatalf("admin options: %+v", opts)
	}
	var all []requestView
	admin.do("GET", "/api/v1/requests?all=1", nil, &all)
	if len(all) != 1 || all[0].Username != "ellen" {
		t.Fatalf("all requests: %+v", all)
	}
	if code := admin.do("POST", "/api/v1/requests/"+req.ID+"/approve", map[string]any{"qualityProfileId": 4}, &req); code != 200 || req.State != store.RequestApproved {
		t.Fatalf("approve: %d %+v", code, req)
	}
	dune, _ := findTitle(radarr, 693134)
	if dune == nil || num(dune["qualityProfileId"]) != 4 || dune["rootFolderPath"] != "/media/movie" || dune["monitored"] != true {
		t.Fatalf("Radarr got: %+v", dune)
	}

	// With requests off, only admins add; with auto, people's requests go straight in.
	admin.do("PUT", "/api/v1/settings", map[string]any{"requests": "off"}, nil)
	if code := ellen.do("GET", "/api/v1/requests/search?kind=show&q=sev", nil, nil); code != http.StatusForbidden {
		t.Fatalf("search with requests off: %d", code)
	}
	var set serverSettings
	admin.do("PUT", "/api/v1/settings", map[string]any{"requests": "auto"}, &set)
	if set.Requests != "auto" {
		t.Fatalf("settings: %+v", set)
	}
	if code := ellen.do("POST", "/api/v1/requests", map[string]any{"kind": "show", "tvdbId": 371980}, &req); code != http.StatusCreated || req.State != store.RequestApproved {
		t.Fatalf("auto request: %d %+v", code, req)
	}
	sev, _ := findTitle(sonarr, 371980)
	if sev == nil || sev["seasonFolder"] != true {
		t.Fatalf("Sonarr got: %+v", sev)
	}
	if seasons := sev["seasons"].([]any); seasons[0].(map[string]any)["monitored"] != false || seasons[1].(map[string]any)["monitored"] != true {
		t.Fatalf("seasons: %+v", seasons)
	}

	// Coming soon, from both calendars, soonest first, leaving out what's in already.
	soon := time.Now().Add(48 * time.Hour).UTC()
	radarr.Calendar = []map[string]any{
		{"title": "Dune: Part Two", "tmdbId": 693134, "digitalRelease": soon.Add(72 * time.Hour)},
		{"title": "Arrival", "tmdbId": 329865, "hasFile": true, "digitalRelease": soon},
	}
	sonarr.Calendar = []map[string]any{
		{"title": "Hello, Ms. Cobel", "seasonNumber": 2, "episodeNumber": 1, "airDateUtc": soon, "series": map[string]any{"title": "Severance"}},
	}
	var up upcomingView
	if code := guest.do("GET", "/api/v1/upcoming", nil, &up); code != 200 || len(up.Items) != 2 {
		t.Fatalf("upcoming: %d %+v", code, up)
	}
	if up.Items[0].ShowTitle != "Severance" || *up.Items[0].Season != 2 || up.Items[1].Release != "digital" {
		t.Fatalf("upcoming order: %+v", up.Items)
	}

	// A movie in the library: change its quality, report a problem, replace the file.
	lavfi := []string{"-f", "lavfi", "-i", "testsrc=duration=2:size=160x120:rate=24", "-c:v", "libx264", "-preset", "ultrafast"}
	makeMedia(t, filepath.Join(tmp, "movies", "Arrival (2016)", "Arrival (2016).mp4"), lavfi...)
	admin.do("POST", "/api/v1/libraries", map[string]any{"name": "Movies", "kind": "movies", "paths": []string{filepath.Join(tmp, "movies")}}, nil)
	waitForScan(t, admin)
	var movies []store.Item
	admin.do("GET", "/api/v1/search?q=arrival", nil, &movies)
	if len(movies) != 1 {
		t.Fatalf("movies: %+v", movies)
	}
	item := "/api/v1/items/" + itoa(movies[0].ID)

	var qv qualityView
	if code := admin.do("GET", item+"/quality", nil, &qv); code != 200 || qv.QualityProfileID != 1 || qv.App != arr.Radarr {
		t.Fatalf("quality: %d %+v", code, qv)
	}
	if code := ellen.do("PUT", item+"/quality", map[string]any{"qualityProfileId": 4}, nil); code != http.StatusForbidden {
		t.Fatalf("non-admin changing quality: %d", code)
	}
	if code := admin.do("PUT", item+"/quality", map[string]any{"qualityProfileId": 4}, &qv); code != 200 {
		t.Fatalf("set quality: %d", code)
	}
	if num(radarr.Library[arrivalID]["qualityProfileId"]) != 4 || !slices.Contains(radarr.CommandNames(), "MoviesSearch") {
		t.Fatalf("Radarr after quality change: %+v %v", radarr.Library[arrivalID], radarr.CommandNames())
	}

	if code := guest.do("POST", item+"/issues", map[string]any{"kind": "video"}, nil); code != http.StatusUnauthorized {
		t.Fatalf("guest reporting: %d", code)
	}
	var issue issueView
	if code := ellen.do("POST", item+"/issues", map[string]any{"kind": "audio", "note": "No sound after 20 minutes"}, &issue); code != http.StatusCreated {
		t.Fatalf("report: %d", code)
	}
	var issues []issueView
	ellen.do("GET", "/api/v1/issues", nil, &issues)
	if len(issues) != 1 || issues[0].Replaceable || issues[0].Item == nil {
		t.Fatalf("ellen's issues: %+v", issues)
	}
	admin.do("GET", "/api/v1/issues?all=1", nil, &issues)
	if len(issues) != 1 || !issues[0].Replaceable || issues[0].Username != "ellen" || issues[0].Note != "No sound after 20 minutes" {
		t.Fatalf("all issues: %+v", issues)
	}
	if code := admin.do("POST", "/api/v1/issues/"+issue.ID+"/replace", nil, nil); code != http.StatusNoContent {
		t.Fatalf("replace: %d", code)
	}
	if !slices.Contains(radarr.Deleted, "/moviefile/7") {
		t.Fatalf("file not deleted: %v", radarr.Deleted)
	}
	admin.do("GET", "/api/v1/issues?all=1", nil, &issues)
	if !issues[0].Resolved || issues[0].Resolution != "replaced" {
		t.Fatalf("issue after replace: %+v", issues[0])
	}

	// Disconnecting turns it all off.
	admin.do("DELETE", "/api/v1/integrations/radarr", nil, nil)
	admin.do("DELETE", "/api/v1/integrations/sonarr", nil, nil)
	ellen.do("GET", "/api/v1/requests/options", nil, &opts)
	if opts.Movies || opts.CanRequest {
		t.Fatalf("options after disconnecting: %+v", opts)
	}
}

func findTitle(f *arrtest.Fake, id int) (map[string]any, bool) {
	key := "tmdbId"
	if f.App == arr.Sonarr {
		key = "tvdbId"
	}
	for _, m := range f.Library {
		if num(m[key]) == id {
			return m, true
		}
	}
	return nil, false
}

func num(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case float64:
		return int(n)
	}
	return 0
}
