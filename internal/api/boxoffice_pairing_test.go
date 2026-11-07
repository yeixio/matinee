package api

import (
	"context"
	"slices"
	"strconv"
	"testing"

	"github.com/yeixio/matinee/internal/arr"
	"github.com/yeixio/matinee/internal/arr/arrtest"
	"github.com/yeixio/matinee/internal/store"
)

type pairingEnv struct {
	srv          *Server
	db           *store.Store
	admin, ellen *client
	ellenID      int64
	movie        int64 // Blade Runner, in a library
}

func pairingSetup(t *testing.T) pairingEnv {
	t.Helper()
	var e pairingEnv
	admin, _, db := newTestServerStore(t, func(s *Server) { e.srv = s })
	e.db, e.admin = db, admin
	var login struct {
		Token string
		User  store.User
	}
	admin.do("POST", "/api/v1/setup", credentials{"admin", "correct horse"}, &login)
	admin.token = login.Token
	admin.do("POST", "/api/v1/users", map[string]any{"username": "ellen", "password": "password123"}, nil)
	e.ellen = &client{t: t, base: admin.base}
	admin.do("POST", "/api/v1/auth/login", credentials{"ellen", "password123"}, &login)
	e.ellen.token, e.ellenID = login.Token, login.User.ID

	ctx := context.Background()
	lib, _ := db.CreateLibrary(ctx, "Movies", "movies", []string{"/m"})
	year := 1982
	id, err := db.UpsertFile(ctx, store.Item{LibraryID: lib.ID, Kind: "movie", Title: "Blade Runner", Year: &year, Path: "/m/Blade Runner.mkv"})
	if err != nil {
		t.Fatal(err)
	}
	db.ApplyMetadata(ctx, id, &store.Metadata{TMDBID: 78})
	e.movie = id
	return e
}

// Radarr and Sonarr that aren't Box Office get none of it: Matinee works as
// it always has.
func TestPairingNotOfferedWithoutBoxOffice(t *testing.T) {
	e := pairingSetup(t)
	radarr := arrtest.New(t, arr.Radarr)
	radarr.Add(map[string]any{"tmdbId": 78, "title": "Blade Runner", "year": 1982, "hasFile": true})
	if code := e.admin.do("PUT", "/api/v1/integrations/radarr", map[string]any{"url": radarr.URL, "apiKey": radarr.Key, "qualityProfileId": 1, "rootFolder": "/media/movie"}, nil); code != 200 {
		t.Fatalf("connect radarr: %d", code)
	}
	e.srv.ReadBoxOffice(t.Context())
	var opts requestOptions
	e.ellen.do("GET", "/api/v1/requests/options", nil, &opts)
	if opts.BoxOffice != nil || !opts.Movies {
		t.Fatalf("options: %+v", opts)
	}
	var soon comingSoonView
	if code := e.ellen.do("GET", "/api/v1/boxoffice/coming-soon", nil, &soon); code != 200 || soon.Available || len(soon.Titles) != 0 {
		t.Fatalf("coming soon: %d %+v", code, soon)
	}
	path := "/api/v1/items/" + strconv.FormatInt(e.movie, 10)
	if code := e.admin.do("GET", path+"/editions", nil, nil); code != 404 {
		t.Fatalf("editions: %d", code)
	}
	if code := e.admin.do("POST", path+"/copy", map[string]any{"want": "better"}, nil); code != 404 {
		t.Fatalf("copy: %d", code)
	}
}

func connectPairedBoxOffice(t *testing.T, e pairingEnv) *arrtest.BoxOffice {
	t.Helper()
	bo := arrtest.NewBoxOffice(t)
	bo.Radarr.Pairing, bo.Sonarr.Pairing = true, true
	bo.Radarr.Add(map[string]any{"tmdbId": 78, "title": "Blade Runner", "year": 1982, "hasFile": true})
	if code := e.admin.do("PUT", "/api/v1/integrations/boxoffice", boxOfficeConfig{URL: bo.URL, APIKey: bo.Key}, nil); code != 200 {
		t.Fatalf("connect: %d", code)
	}
	e.srv.ReadBoxOffice(t.Context())
	return bo
}

func TestComingSoonFromBoxOffice(t *testing.T) {
	e := pairingSetup(t)
	bo := connectPairedBoxOffice(t, e)
	activity := map[string]any{"boxOfficeId": "bo-1", "speed": 1000, "items": []any{
		map[string]any{"id": 1, "kind": "movie", "tmdbId": 329865, "title": "Arrival", "year": 2016, "poster": "https://img/arrival.jpg", "stage": "downloading", "percent": 40, "etaSeconds": 600},
		map[string]any{"id": 2, "kind": "series", "tvdbId": 273181, "tmdbId": 60059, "title": "Better Call Saul", "stage": "queued", "episodes": []any{map[string]any{"seasonNumber": 1, "episodeNumber": 2}}},
		map[string]any{"id": 3, "kind": "series", "tvdbId": 273181, "tmdbId": 60059, "title": "Better Call Saul", "stage": "unpacking", "percent": 50, "episodes": []any{map[string]any{"seasonNumber": 1, "episodeNumber": 1}}},
		map[string]any{"id": 4, "kind": "movie", "tmdbId": 78, "title": "Blade Runner", "stage": "failed", "message": "par2 couldn't repair it", "edition": "Director's Cut", "mode": "alongside"},
	}}
	bo.Radarr.Activity, bo.Sonarr.Activity = activity, activity
	// Ellen's request that Box Office hasn't found yet is being looked for.
	ctx := t.Context()
	req, _ := e.db.CreateRequest(ctx, store.Request{UserID: e.ellenID, Kind: "movie", TMDBID: 603, Title: "The Matrix", Year: 1999, Poster: "https://img/matrix.jpg"})
	e.db.UpdateRequest(ctx, req.ID, store.RequestApproved, "", 0)

	var opts requestOptions
	e.ellen.do("GET", "/api/v1/requests/options", nil, &opts)
	if opts.BoxOffice == nil || !opts.BoxOffice.Progress || !opts.BoxOffice.Editions || !opts.BoxOffice.UpgradeShows || !opts.BoxOffice.AsksNeedApproval {
		t.Fatalf("options: %+v", opts.BoxOffice)
	}

	var soon comingSoonView
	if code := e.ellen.do("GET", "/api/v1/boxoffice/coming-soon", nil, &soon); code != 200 || !soon.Available {
		t.Fatalf("coming soon: %d %+v", code, soon)
	}
	titles := map[string]comingSoonTitle{}
	order := []string{}
	for _, x := range soon.Titles {
		titles[x.Title], order = x, append(order, x.Title)
	}
	if !slices.Equal(order, []string{"Better Call Saul", "Arrival", "The Matrix", "Blade Runner"}) {
		t.Fatalf("order: %v", order)
	}
	// A show's episodes are one title, showing the one furthest along.
	if s := titles["Better Call Saul"]; s.Stage != "unpacking" || s.Downloads != 2 || s.Episode != 1 {
		t.Errorf("show: %+v", s)
	}
	if a := titles["Arrival"]; a.Percent != 40 || a.ETA != 600 || a.Poster == "" || a.Requested {
		t.Errorf("arrival: %+v", a)
	}
	if m := titles["The Matrix"]; m.Stage != "searching" || !m.Requested || m.Poster != "https://img/matrix.jpg" {
		t.Errorf("matrix: %+v", m)
	}
	// Another edition of a title in the library points at it; only admins
	// see why it failed.
	if b := titles["Blade Runner"]; b.ItemID != e.movie || b.Message != "" || b.Edition != "Director's Cut" {
		t.Errorf("blade runner: %+v", b)
	}
	e.admin.do("GET", "/api/v1/boxoffice/coming-soon", nil, &soon)
	if soon.Titles[3].Message == "" {
		t.Errorf("admin doesn't see the failure: %+v", soon.Titles[3])
	}

	// Notify me: Ellen is told when Arrival arrives, like a requester.
	if code := e.ellen.do("POST", "/api/v1/boxoffice/notify-me", map[string]any{"kind": "movie", "tmdbId": 329865}, nil); code != 200 {
		t.Fatalf("notify me: %d", code)
	}
	if ids, _ := e.db.Requesters(ctx, "movie", 329865, 0); !slices.Contains(ids, e.ellenID) {
		t.Fatalf("requesters: %v", ids)
	}
	e.srv.arrCache.clear()
	e.ellen.do("GET", "/api/v1/boxoffice/coming-soon", nil, &soon)
	if !soon.Titles[1].NotifyMe {
		t.Errorf("notify me not shown: %+v", soon.Titles[1])
	}
	e.ellen.do("POST", "/api/v1/boxoffice/notify-me", map[string]any{"kind": "movie", "tmdbId": 329865, "on": false}, nil)
	if ids, _ := e.db.Requesters(ctx, "movie", 329865, 0); slices.Contains(ids, e.ellenID) {
		t.Fatalf("still told: %v", ids)
	}
}

func TestEditionsAndCopiesFromBoxOffice(t *testing.T) {
	e := pairingSetup(t)
	bo := connectPairedBoxOffice(t, e)
	bo.Radarr.Editions = map[string]any{"movieId": 1, "hasFile": true, "editions": []any{map[string]any{"edition": "Director's Cut", "releases": 2, "quality": "Blu-ray 1080p"}}}
	path := "/api/v1/items/" + strconv.FormatInt(e.movie, 10)

	var eds arr.BoxOfficeEditions
	if code := e.ellen.do("GET", path+"/editions", nil, &eds); code != 200 || len(eds.Editions) != 1 || eds.Editions[0].Edition != "Director's Cut" {
		t.Fatalf("editions: %d %+v", code, eds)
	}

	// Requests wait for an admin (the default), and so do editions.
	var res askResult
	if code := e.ellen.do("POST", path+"/editions", map[string]any{"edition": "Director's Cut", "mode": "alongside"}, &res); code != 201 || res.State != store.RequestPending {
		t.Fatalf("ask: %d %+v", code, res)
	}
	if len(bo.Radarr.Asked) != 0 {
		t.Fatalf("sent before approval: %+v", bo.Radarr.Asked)
	}
	// It doesn't make the movie look requested.
	if _, err := e.db.OpenRequest(t.Context(), "movie", 78); err == nil {
		t.Fatal("an edition request counts as asking for the movie")
	}
	var approved requestView
	if code := e.admin.do("POST", "/api/v1/requests/"+res.Request.ID+"/approve", nil, &approved); code != 200 || approved.State != store.RequestApproved {
		t.Fatalf("approve: %d %+v", code, approved)
	}
	if len(bo.Radarr.Asked) != 1 || bo.Radarr.Asked[0]["path"] != "/boxoffice/editions" || bo.Radarr.Asked[0]["edition"] != "Director's Cut" ||
		bo.Radarr.Asked[0]["mode"] != "alongside" || bo.Radarr.Asked[0]["tmdbId"] != float64(78) {
		t.Fatalf("asked: %+v", bo.Radarr.Asked)
	}

	// With requests added right away, they go straight to Box Office...
	e.admin.do("PUT", "/api/v1/settings", map[string]any{"requests": "auto"}, nil)
	if code := e.ellen.do("POST", path+"/copy", map[string]any{"want": "replace", "reason": "out of sync"}, &res); code != 201 || res.State != store.RequestApproved {
		t.Fatalf("copy: %d %+v", code, res)
	}
	if last := bo.Radarr.Asked[len(bo.Radarr.Asked)-1]; last["path"] != "/boxoffice/upgrade" || last["want"] != "replace" || last["reason"] != "out of sync" {
		t.Fatalf("asked: %+v", last)
	}
	// ...unless Settings says editions and copies always wait.
	e.admin.do("PUT", "/api/v1/settings", map[string]any{"boxOfficeAsksNeedApproval": true}, nil)
	var set serverSettings
	e.admin.do("GET", "/api/v1/settings", nil, &set)
	if !set.BoxOfficeAsksNeedApproval {
		t.Fatal("setting not saved")
	}
	if code := e.ellen.do("POST", path+"/copy", map[string]any{"want": "better"}, &res); code != 201 || res.State != store.RequestPending {
		t.Fatalf("copy with approval: %d %+v", code, res)
	}
	// Admins' own go straight through.
	if code := e.admin.do("POST", path+"/copy", map[string]any{"want": "better"}, &res); code != 201 || res.State != store.RequestApproved {
		t.Fatalf("admin copy: %d %+v", code, res)
	}
	if code := e.ellen.do("POST", path+"/copy", map[string]any{"want": "nope"}, nil); code != 400 {
		t.Fatalf("bad want: %d", code)
	}
}

// A problem someone reports goes to a Box Office that fixes problems, which
// gets another copy; once another file is in, the report closes and the
// person who made it is told.
func TestReportedProblemsFixedByBoxOffice(t *testing.T) {
	e := pairingSetup(t)
	bo := arrtest.NewBoxOffice(t)
	bo.Radarr.Pairing, bo.Radarr.Problems = true, true
	bo.Radarr.Add(map[string]any{"tmdbId": 78, "title": "Blade Runner", "year": 1982, "hasFile": true})
	if code := e.admin.do("PUT", "/api/v1/integrations/boxoffice", boxOfficeConfig{URL: bo.URL, APIKey: bo.Key}, nil); code != 200 {
		t.Fatalf("connect: %d", code)
	}
	e.srv.ReadBoxOffice(t.Context())
	ctx := t.Context()
	path := "/api/v1/items/" + strconv.FormatInt(e.movie, 10) + "/issues"

	var opts requestOptions
	e.ellen.do("GET", "/api/v1/requests/options", nil, &opts)
	if opts.BoxOffice == nil || !opts.BoxOffice.FixMovies || opts.BoxOffice.FixShows || opts.BoxOffice.ReportsNeedApproval {
		t.Fatalf("options: %+v", opts.BoxOffice)
	}

	// The wrong language needs the language it should be in.
	if code := e.ellen.do("POST", path, map[string]any{"kind": "language"}, nil); code != 400 {
		t.Fatalf("no language: %d", code)
	}
	// Even with requests waiting for an admin (the default), Box Office
	// is asked straight away.
	var v issueView
	if code := e.ellen.do("POST", path, map[string]any{"kind": "language", "language": "en", "note": "German dub"}, &v); code != 201 || v.Fix == nil || v.Fix.State != "fixing" {
		t.Fatalf("report: %d %+v", code, v)
	}
	last := bo.Radarr.Asked[len(bo.Radarr.Asked)-1]
	if last["path"] != "/boxoffice/upgrade" || last["want"] != "replace" || last["problem"] != "language" || last["language"] != "en" ||
		last["reportedBy"] != "ellen" || last["reason"] != "German dub" || last["tmdbId"] != float64(78) || last["fileName"] != "Blade Runner.mkv" {
		t.Fatalf("asked: %+v", last)
	}
	// It isn't waiting on an admin.
	if _, issues, _ := e.db.Waiting(ctx); issues != 0 {
		t.Fatalf("waiting: %d", issues)
	}
	// Subtitles aren't Box Office's to fix: they wait for an admin.
	var subs issueView
	e.ellen.do("POST", path, map[string]any{"kind": "subtitles"}, &subs)
	if subs.Fix != nil || len(bo.Radarr.Asked) != 1 {
		t.Fatalf("subtitles: %+v %d", subs, len(bo.Radarr.Asked))
	}
	if _, issues, _ := e.db.Waiting(ctx); issues != 1 {
		t.Fatalf("waiting: %d", issues)
	}

	// Nothing changed yet: it stays open.
	e.srv.settleIssues(ctx)
	if x, _ := e.db.Issue(ctx, v.ID); x.Resolved {
		t.Fatal("closed before a new copy is in")
	}
	// Another copy took its place.
	year := 1982
	e.db.UpsertFile(ctx, store.Item{LibraryID: v.Item.LibraryID, Kind: "movie", Title: "Blade Runner", Year: &year, Path: "/m/Blade Runner.mkv", Size: 5 << 30, Mtime: 1700000000})
	e.srv.settleIssues(ctx)
	x, _ := e.db.Issue(ctx, v.ID)
	if !x.Resolved || x.Resolution != store.IssueReplaced {
		t.Fatalf("not closed: %+v", x)
	}
	notes, _, _ := e.db.Notifications(ctx, []int64{e.ellenID}, 10)
	if len(notes) == 0 || notes[0].Event != "issue.fixed" || notes[0].Title != "Another copy of Blade Runner is in" {
		t.Fatalf("notifications: %+v", notes)
	}
	var list []issueView
	e.ellen.do("GET", "/api/v1/issues", nil, &list)
	for _, l := range list {
		if l.ID == v.ID && (l.Fix == nil || l.Fix.State != "fixed") {
			t.Fatalf("listed: %+v", l)
		}
	}

	// With Settings making Box Office asks wait, Ellen's waits for an
	// admin, and approving it sends it.
	e.admin.do("PUT", "/api/v1/settings", map[string]any{"boxOfficeAsksNeedApproval": true}, nil)
	v = issueView{}
	if code := e.ellen.do("POST", path, map[string]any{"kind": "wrong"}, &v); code != 201 || v.Fix == nil || v.Fix.State != "waiting" {
		t.Fatalf("report waiting: %d %+v", code, v)
	}
	if len(bo.Radarr.Asked) != 1 {
		t.Fatalf("sent before approval: %+v", bo.Radarr.Asked)
	}
	if code := e.admin.do("POST", "/api/v1/requests/"+v.RequestID+"/approve", nil, nil); code != 200 {
		t.Fatalf("approve: %d", code)
	}
	if last := bo.Radarr.Asked[len(bo.Radarr.Asked)-1]; last["problem"] != "wrong" || last["want"] != "replace" {
		t.Fatalf("asked: %+v", last)
	}

	// A Box Office without "problems" gets nothing: the report waits for
	// an admin, as before.
	bo.Radarr.Problems = false
	e.srv.ReadBoxOffice(ctx)
	n := len(bo.Radarr.Asked)
	var plain issueView
	if e.ellen.do("POST", path, map[string]any{"kind": "video"}, &plain); plain.Fix != nil || len(bo.Radarr.Asked) != n {
		t.Fatalf("without problems: %+v", plain)
	}
}
