package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/yeixio/matinee/internal/arr"
	"github.com/yeixio/matinee/internal/arr/arrtest"
	"github.com/yeixio/matinee/internal/live"
	"github.com/yeixio/matinee/internal/store"
)

// Box Office's notifications reach the person who asked for the title, on
// their open players and in their list, and admins hear about problems.
func TestBoxOfficeNotifications(t *testing.T) {
	var srv *Server
	admin, _, db := newTestServerStore(t, func(s *Server) { srv = s })
	ctx := context.Background()
	var login struct {
		Token string
		User  store.User
	}
	admin.do("POST", "/api/v1/setup", credentials{"admin", "correct horse"}, &login)
	admin.token = login.Token
	admin.do("POST", "/api/v1/users", map[string]any{"username": "ellen", "password": "password123"}, nil)
	ellen := &client{t: t, base: admin.base}
	admin.do("POST", "/api/v1/auth/login", credentials{"ellen", "password123"}, &login)
	ellen.token, _ = login.Token, login.User

	// Box Office answers as both Radarr and Sonarr.
	radarr := arrtest.New(t, arr.Radarr)
	radarr.BoxOfficeID = "bo-1"
	sonarr := arrtest.New(t, arr.Sonarr)
	sonarr.BoxOfficeID = "bo-1"
	for app, f := range map[string]*arrtest.Fake{"radarr": radarr, "sonarr": sonarr} {
		if code := admin.do("PUT", "/api/v1/integrations/"+app, arr.Config{URL: f.URL, APIKey: f.Key}, nil); code != 200 {
			t.Fatalf("connect %s: %d", app, code)
		}
	}
	ellenUser, err := db.UserByName(ctx, "ellen")
	if err != nil {
		t.Fatal(err)
	}
	req, _ := db.CreateRequest(ctx, store.Request{Kind: "movie", TMDBID: 693134, Title: "Dune: Part Two", Year: 2024, UserID: ellenUser.ID})
	db.UpdateRequest(ctx, req.ID, store.RequestApproved, "", 0)

	// What happened before Matinee first looked isn't news.
	radarr.Notify(arr.BoxOfficeNotification{Event: "download.imported", Audience: "everyone", Title: "Old news",
		Media: &arr.BoxOfficeMedia{Kind: "movie", TMDBID: 693134}, At: time.Now()})
	srv.ReadBoxOffice(ctx)

	// Ellen's phone is open and shows notifications.
	hello := map[string]any{"name": "Ellen's phone", "platform": "ios", "commands": []string{"playItem"}, "notifications": true}
	if code := ellen.do("PUT", "/api/v1/clients/phone-1234567", hello, nil); code != 200 {
		t.Fatalf("hello: %d", code)
	}

	dune := &arr.BoxOfficeMedia{Kind: "movie", Title: "Dune: Part Two", Year: 2024, TMDBID: 693134}
	radarr.Notify(arr.BoxOfficeNotification{Event: "download.grabbed", Level: "info", Audience: "everyone", Title: "Downloading Dune: Part Two (2024)", Media: dune, At: time.Now()})
	radarr.Notify(arr.BoxOfficeNotification{Event: "download.failed", Level: "warning", Audience: "admins", Title: "Dune: Part Two (2024) didn't download", Media: dune, At: time.Now()})
	radarr.Notify(arr.BoxOfficeNotification{Event: "download.imported", Level: "info", Audience: "everyone", Title: "Dune: Part Two (2024) is ready", Media: dune, At: time.Now()})
	radarr.Notify(arr.BoxOfficeNotification{Event: "download.grabbed", Level: "info", Audience: "everyone", Title: "Downloading Arrival (2016)",
		Media: &arr.BoxOfficeMedia{Kind: "movie", TMDBID: 329865}, At: time.Now()})
	srv.ReadBoxOffice(ctx)
	// Reading again (or from a second Matinee pod) tells nobody twice.
	db.SetSetting(ctx, settingFeedPrefix+"bo-1", "1")
	srv.ReadBoxOffice(ctx)

	var list struct {
		Notifications []store.Notification
		Unread        int
	}
	if code := ellen.do("GET", "/api/v1/notifications", nil, &list); code != 200 || list.Unread != 2 || len(list.Notifications) != 2 {
		t.Fatalf("ellen's: %d %+v", code, list)
	}
	for _, n := range list.Notifications {
		if n.Kind != "movie" || n.TMDBID != 693134 || n.Read {
			t.Fatalf("ellen's: %+v", n)
		}
	}
	var got struct{ Commands []live.Command }
	if code := ellen.do("GET", "/api/v1/clients/phone-1234567/commands?wait=1", nil, &got); code != 200 || len(got.Commands) != 2 || got.Commands[0].Type != live.CommandNotify {
		t.Fatalf("pushed: %d %+v", code, got)
	}

	// Admins hear about problems only, by default.
	admin.do("GET", "/api/v1/notifications", nil, &list)
	if len(list.Notifications) != 1 || list.Notifications[0].Event != "download.failed" || list.Notifications[0].Level != "warning" {
		t.Fatalf("admin's: %+v", list)
	}
	var settings notificationSettings
	if code := admin.do("GET", "/api/v1/admin/notifications", nil, &settings); code != 200 || settings.Admins != "problems" ||
		len(settings.BoxOffice) != 2 || settings.BoxOffice[0].ID != "bo-1" || settings.BoxOffice[1].ID != "bo-1" {
		t.Fatalf("settings: %d %+v", code, settings)
	}
	if code := ellen.do("PUT", "/api/v1/admin/notifications", notificationSettings{Admins: "all"}, nil); code != http.StatusForbidden {
		t.Fatalf("ellen changed admin settings: %d", code)
	}
	if code := admin.do("PUT", "/api/v1/admin/notifications", notificationSettings{Admins: "loud"}, nil); code != http.StatusBadRequest {
		t.Fatalf("bad mode: %d", code)
	}
	if code := admin.do("PUT", "/api/v1/admin/notifications", notificationSettings{Admins: "all"}, nil); code != 200 {
		t.Fatalf("all: %d", code)
	}
	radarr.Notify(arr.BoxOfficeNotification{Event: "download.grabbed", Audience: "everyone", Title: "Downloading Arrival again",
		Media: &arr.BoxOfficeMedia{Kind: "movie", TMDBID: 329865}, At: time.Now()})
	srv.ReadBoxOffice(ctx)
	admin.do("GET", "/api/v1/notifications", nil, &list)
	if len(list.Notifications) != 2 || list.Unread != 2 {
		t.Fatalf("admin's, all: %+v", list)
	}

	// Read one, then clear them all; each person's own only.
	ellen.do("GET", "/api/v1/notifications", nil, &list)
	if code := ellen.do("POST", "/api/v1/notifications/read", notificationIDs{IDs: []string{list.Notifications[0].ID}}, nil); code != http.StatusNoContent {
		t.Fatalf("read: %d", code)
	}
	ellen.do("GET", "/api/v1/notifications", nil, &list)
	if list.Unread != 1 {
		t.Fatalf("after reading one: %+v", list)
	}
	ellen.do("POST", "/api/v1/notifications/clear", notificationIDs{}, nil)
	ellen.do("GET", "/api/v1/notifications", nil, &list)
	if len(list.Notifications) != 0 {
		t.Fatalf("after clearing: %+v", list)
	}
	admin.do("GET", "/api/v1/notifications", nil, &list)
	if len(list.Notifications) != 2 {
		t.Fatalf("ellen cleared the admin's: %+v", list)
	}
}

// Real Radarr has no feed, and is left alone.
func TestNotificationsIgnoreRadarr(t *testing.T) {
	var srv *Server
	admin, _ := newTestServer(t, func(s *Server) { srv = s })
	var login struct{ Token string }
	admin.do("POST", "/api/v1/setup", credentials{"admin", "correct horse"}, &login)
	admin.token = login.Token
	radarr := arrtest.New(t, arr.Radarr)
	admin.do("PUT", "/api/v1/integrations/radarr", arr.Config{URL: radarr.URL, APIKey: radarr.Key}, nil)
	srv.ReadBoxOffice(context.Background())
	var settings notificationSettings
	admin.do("GET", "/api/v1/admin/notifications", nil, &settings)
	if len(settings.BoxOffice) != 1 || settings.BoxOffice[0].ID != "" || settings.BoxOffice[0].Error != "" {
		t.Fatalf("settings: %+v", settings)
	}
}

// The same notification again, such as a disk that stays nearly full,
// counts on the one already there in place of stacking up.
func TestRepeatedNotifications(t *testing.T) {
	var srv *Server
	admin, _, _ := newTestServerStore(t, func(s *Server) { srv = s })
	ctx := context.Background()
	var login struct{ Token string }
	admin.do("POST", "/api/v1/setup", credentials{"admin", "correct horse"}, &login)
	admin.token = login.Token
	radarr := arrtest.New(t, arr.Radarr)
	radarr.BoxOfficeID = "bo-1"
	if code := admin.do("PUT", "/api/v1/integrations/radarr", arr.Config{URL: radarr.URL, APIKey: radarr.Key}, nil); code != 200 {
		t.Fatalf("connect: %d", code)
	}
	srv.ReadBoxOffice(ctx) // starts from now
	hello := map[string]any{"name": "Browser", "platform": "web", "commands": []string{"playItem"}, "notifications": true}
	if code := admin.do("PUT", "/api/v1/clients/web-1234567", hello, nil); code != 200 {
		t.Fatalf("hello: %d", code)
	}
	disk := func(pct int, level string) {
		radarr.Notify(arr.BoxOfficeNotification{Event: "system.disk", Level: level, Audience: "admins", Title: "Disk space is low",
			Body: fmt.Sprintf("/downloads is %d%% full: %d GB free.", pct, 100-pct), At: time.Now()})
	}
	for i := range 10 {
		disk(90+i%3, "warning")
		srv.ReadBoxOffice(ctx)
	}
	// Another disk is another notification.
	radarr.Notify(arr.BoxOfficeNotification{Event: "system.disk", Level: "warning", Audience: "admins", Title: "Disk space is low",
		Body: "/movies is 95% full: 5 GB free.", At: time.Now()})
	srv.ReadBoxOffice(ctx)

	var list struct {
		Notifications []store.Notification
		Unread        int
	}
	list.Notifications = nil
	admin.do("GET", "/api/v1/notifications", nil, &list)
	if len(list.Notifications) != 2 || list.Unread != 2 {
		t.Fatalf("listed: %+v", list)
	}
	movies, downloads := list.Notifications[0], list.Notifications[1]
	if strings.HasPrefix(movies.Body, "/downloads") {
		movies, downloads = downloads, movies
	}
	if movies.Repeats != 0 || downloads.Repeats != 10 || downloads.Body != "/downloads is 90% full: 10 GB free." || downloads.FirstAt == 0 {
		t.Fatalf("repeats: %+v", list.Notifications)
	}
	var got struct{ Commands []live.Command }
	if admin.do("GET", "/api/v1/clients/web-1234567/commands?wait=1", nil, &got); len(got.Commands) != 2 {
		t.Fatalf("popped up %d times, want once each", len(got.Commands))
	}

	// Read, it comes again: it's unread again, at the top, and doesn't pop up...
	admin.do("POST", "/api/v1/notifications/read", notificationIDs{}, nil)
	disk(92, "warning")
	srv.ReadBoxOffice(ctx)
	list.Notifications = nil
	admin.do("GET", "/api/v1/notifications", nil, &list)
	if n := find(list.Notifications, downloads.ID); list.Unread != 1 || n.Repeats != 11 || n.Read {
		t.Fatalf("again: %+v", list)
	}
	if admin.do("GET", "/api/v1/clients/web-1234567/commands?wait=1", nil, &got); len(got.Commands) != 0 {
		t.Fatalf("a repeat popped up: %+v", got)
	}
	// ...unless it's got more serious.
	disk(98, "critical")
	srv.ReadBoxOffice(ctx)
	list.Notifications = nil
	admin.do("GET", "/api/v1/notifications", nil, &list)
	if n := find(list.Notifications, downloads.ID); n.Level != "critical" || n.Repeats != 12 {
		t.Fatalf("critical: %+v", list)
	}
	if admin.do("GET", "/api/v1/clients/web-1234567/commands?wait=1", nil, &got); len(got.Commands) != 1 {
		t.Fatalf("critical didn't pop up: %+v", got)
	}

	// Cleared, the next one starts over.
	if code := admin.do("POST", "/api/v1/notifications/clear", notificationIDs{IDs: []string{downloads.ID}}, nil); code != http.StatusNoContent {
		t.Fatalf("clear: %d", code)
	}
	disk(93, "warning")
	srv.ReadBoxOffice(ctx)
	list.Notifications = nil
	admin.do("GET", "/api/v1/notifications", nil, &list)
	if n := find(list.Notifications, downloads.ID); n.ID != "" || len(list.Notifications) != 2 || list.Notifications[0].Repeats+list.Notifications[1].Repeats != 0 {
		t.Fatalf("after clearing: %+v", list)
	}
}

func find(list []store.Notification, id string) store.Notification {
	for _, n := range list {
		if n.ID == id {
			return n
		}
	}
	return store.Notification{}
}
