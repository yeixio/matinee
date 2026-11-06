package api

import (
	"context"
	"net/http"
	"testing"

	"github.com/yeixio/matinee/internal/arr/arrtest"
	"github.com/yeixio/matinee/internal/store"
)

// With a Box Office that takes requests, asking for a title files it on
// Box Office's Requests page under the person's Matinee account, and
// Matinee follows what Box Office decides. Box Office fills Discover even
// with Overseerr connected.
func TestRequestsWithBoxOffice(t *testing.T) {
	var srv *Server
	admin, _, db := newTestServerStore(t, func(s *Server) { srv = s })
	ctx := context.Background()
	var login struct {
		Token string
		User  store.User
	}
	admin.do("POST", "/api/v1/setup", credentials{"admin", "correct horse"}, &login)
	admin.token = login.Token
	admin.do("POST", "/api/v1/users", map[string]any{"username": "Ellen", "password": "password123"}, nil)
	ellen := &client{t: t, base: admin.base}
	admin.do("POST", "/api/v1/auth/login", credentials{"Ellen", "password123"}, &login)
	ellen.token = login.Token
	ellenID := login.User.ID

	// An Overseerr that's gone away, as on a server that moved to Box Office.
	db.SetSetting(ctx, settingOverseerr, `{"url":"http://127.0.0.1:1","apiKey":"gone"}`)

	bo := arrtest.NewBoxOffice(t)
	discover := map[string]any{"": map[string]any{"rows": []any{
		map[string]any{"id": "trending", "title": "Trending", "items": []any{map[string]any{"kind": "movie", "tmdbId": 329865, "title": "Arrival", "year": 2016}}},
	}}}
	bo.Radarr.Discover, bo.Sonarr.Discover = discover, discover
	bo.Radarr.TakesRequests, bo.Sonarr.TakesRequests = true, true
	bo.Radarr.Catalog = []map[string]any{{"tmdbId": 329865, "title": "Arrival", "year": 2016}, {"tmdbId": 603, "title": "The Matrix", "year": 1999}}
	bo.Sonarr.Catalog = []map[string]any{{"tmdbId": 60059, "tvdbId": 273181, "title": "Better Call Saul", "year": 2015}}
	if code := admin.do("PUT", "/api/v1/integrations/boxoffice", boxOfficeConfig{URL: bo.URL, APIKey: bo.Key}, nil); code != 200 {
		t.Fatalf("connect: %d", code)
	}
	srv.ReadBoxOffice(ctx)

	var view discoverView
	if code := ellen.do("GET", "/api/v1/discover", nil, &view); code != 200 || len(view.Rows) != 1 {
		t.Fatalf("discover with Overseerr gone: %d %+v", code, view)
	}

	// Ellen's request goes to Box Office, waiting there.
	var asked discoverTitle
	if code := ellen.do("POST", "/api/v1/discover/requests", map[string]any{"kind": "show", "tmdbId": 60059}, &asked); code != http.StatusCreated || asked.Status != titleRequested {
		t.Fatalf("ellen asks: %d %+v", code, asked)
	}
	if len(bo.Sonarr.Requests) != 1 || bo.Sonarr.Requests[100]["user"] != "Ellen" || bo.Sonarr.Requests[100]["approved"] != false || bo.Sonarr.Requests[100]["tmdbId"] != float64(60059) {
		t.Fatalf("box office got: %+v", bo.Sonarr.Requests)
	}
	if len(bo.Sonarr.Library) != 0 {
		t.Fatalf("added without approval: %+v", bo.Sonarr.Library)
	}
	req, err := db.OpenRequest(ctx, "show", 273181)
	if err != nil || req.State != store.RequestPending || req.BoxOfficeID != 100 || req.UserID != ellenID {
		t.Fatalf("matinee's copy: %+v %v", req, err)
	}

	// The admin's own request is added straight away on Box Office.
	if code := admin.do("POST", "/api/v1/requests", map[string]any{"kind": "movie", "tmdbId": 329865}, nil); code != http.StatusCreated {
		t.Fatalf("admin asks: %d", code)
	}
	if r := bo.Radarr.Requests[100]; r["approved"] != true || r["user"] != "admin" {
		t.Fatalf("admin's: %+v", bo.Radarr.Requests)
	}
	if r, _ := db.OpenRequest(ctx, "movie", 329865); r.State != store.RequestApproved {
		t.Fatalf("admin's copy: %+v", r)
	}

	// Box Office's rules come back in its words.
	bo.Radarr.RequestRule = "you can ask for 1 movies a week, and you've reached that; try again in a few days"
	var e struct{ Error string }
	if code := ellen.do("POST", "/api/v1/requests", map[string]any{"kind": "movie", "tmdbId": 603}, &e); code != http.StatusBadRequest || e.Error != bo.Radarr.RequestRule {
		t.Fatalf("over the limit: %d %+v", code, e)
	}
	bo.Radarr.RequestRule = ""

	// An admin approves Ellen's on Box Office; Matinee catches up and tells her.
	bo.Sonarr.Requests[100]["status"] = "approved"
	srv.ReadBoxOffice(ctx)
	if r, _ := db.Request(ctx, req.ID); r.State != store.RequestApproved {
		t.Fatalf("after approving on Box Office: %+v", r)
	}
	notes, _, err := db.Notifications(ctx, []int64{ellenID}, 10)
	if err != nil || len(notes) != 1 || notes[0].Event != "request.approved" || notes[0].Title != "Your request for Better Call Saul was approved" {
		t.Fatalf("ellen told: %+v %v", notes, err)
	}

	// Decided here, it's passed on: declined, and approved with a profile.
	var m1, m2 requestView
	ellen.do("POST", "/api/v1/requests", map[string]any{"kind": "movie", "tmdbId": 603}, &m1)
	if code := admin.do("POST", "/api/v1/requests/"+m1.ID+"/decline", nil, nil); code != 200 || bo.Radarr.Requests[m1.BoxOfficeID]["status"] != "declined" {
		t.Fatalf("decline: %d %+v", code, bo.Radarr.Requests[m1.BoxOfficeID])
	}
	admin.do("DELETE", "/api/v1/requests/"+m1.ID, nil, nil)
	ellen.do("POST", "/api/v1/requests", map[string]any{"kind": "movie", "tmdbId": 603}, &m2)
	if code := admin.do("POST", "/api/v1/requests/"+m2.ID+"/approve", map[string]int{"qualityProfileId": 4}, nil); code != 200 {
		t.Fatalf("approve: %d", code)
	}
	if r := bo.Radarr.Requests[m2.BoxOfficeID]; r["status"] != "approved" || r["approvedWith"] != float64(4) {
		t.Fatalf("approved on box office: %+v", r)
	}

	// Deleting one that waits takes it off Box Office's page too.
	var m3 requestView
	bo.Radarr.Catalog = append(bo.Radarr.Catalog, map[string]any{"tmdbId": 11, "title": "Star Wars", "year": 1977})
	ellen.do("POST", "/api/v1/requests", map[string]any{"kind": "movie", "tmdbId": 11}, &m3)
	if code := ellen.do("DELETE", "/api/v1/requests/"+m3.ID, nil, nil); code != http.StatusNoContent {
		t.Fatalf("delete: %d", code)
	}
	if _, ok := bo.Radarr.Requests[m3.BoxOfficeID]; ok || m3.BoxOfficeID == 0 {
		t.Fatalf("still on box office: %d %+v", m3.BoxOfficeID, bo.Radarr.Requests)
	}
}
