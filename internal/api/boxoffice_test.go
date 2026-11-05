package api

import (
	"strings"
	"testing"

	"github.com/yeixio/matinee/internal/arr"
	"github.com/yeixio/matinee/internal/arr/arrtest"
)

// Box Office connects in one go, as both Radarr and Sonarr, and Matinee
// finds it on its own.
func TestBoxOfficeConnection(t *testing.T) {
	bo := arrtest.NewBoxOffice(t)
	saved := boxOfficeGuesses
	// It's found once, at the first address that reaches it.
	boxOfficeGuesses = []string{"http://127.0.0.1:1", bo.URL, strings.Replace(bo.URL, "127.0.0.1", "localhost", 1)}
	t.Cleanup(func() { boxOfficeGuesses = saved })

	admin, _ := newTestServer(t)
	var login struct{ Token string }
	admin.do("POST", "/api/v1/setup", credentials{"admin", "correct horse"}, &login)
	admin.token = login.Token

	var view boxOfficeView
	if code := admin.do("GET", "/api/v1/integrations/boxoffice", nil, &view); code != 200 || view.Connected || view.Movies || view.URL != "" {
		t.Fatalf("before: %d %+v", code, view)
	}
	var found struct{ Found []foundBoxOffice }
	if code := admin.do("POST", "/api/v1/integrations/boxoffice/find", nil, &found); code != 200 || len(found.Found) != 1 || found.Found[0].URL != bo.URL || found.Found[0].Version != "1.2.3" {
		t.Fatalf("find: %d %+v", code, found)
	}

	// Without the key, or with the wrong one, nothing's saved.
	if code := admin.do("PUT", "/api/v1/integrations/boxoffice", boxOfficeConfig{URL: bo.URL}, nil); code != 400 {
		t.Fatalf("no key: %d", code)
	}
	if code := admin.do("PUT", "/api/v1/integrations/boxoffice", boxOfficeConfig{URL: bo.URL, APIKey: "nope"}, nil); code != 502 {
		t.Fatalf("wrong key: %d", code)
	}
	// Plain Radarr isn't Box Office.
	radarr := arrtest.New(t, arr.Radarr)
	if code := admin.do("POST", "/api/v1/integrations/boxoffice/test", boxOfficeConfig{URL: radarr.URL, APIKey: radarr.Key}, nil); code != 400 && code != 502 {
		t.Fatalf("plain radarr: %d", code)
	}

	var chk boxOfficeCheck
	if code := admin.do("POST", "/api/v1/integrations/boxoffice/test", boxOfficeConfig{URL: bo.URL + "/radarr/", APIKey: bo.Key}, &chk); code != 200 ||
		chk.URL != bo.URL || chk.ID != "bo-1" || len(chk.Movies.RootFolders) != 1 || len(chk.Shows.QualityProfiles) != 2 {
		t.Fatalf("test: %d %+v", code, chk)
	}
	if code := admin.do("PUT", "/api/v1/integrations/boxoffice", boxOfficeConfig{URL: bo.URL, APIKey: bo.Key}, &view); code != 200 || !view.Connected || view.URL != bo.URL || !view.HasAPIKey {
		t.Fatalf("connect: %d %+v", code, view)
	}
	var ints map[arr.App]integrationView
	admin.do("GET", "/api/v1/integrations", nil, &ints)
	if r, s := ints[arr.Radarr], ints[arr.Sonarr]; r.URL != bo.URL+"/radarr" || s.URL != bo.URL+"/sonarr" || r.RootFolder != "/media/movie" || s.QualityProfileID != 1 {
		t.Fatalf("radarr and sonarr: %+v", ints)
	}
	// Testing again needs no key; people can now ask for titles.
	if code := admin.do("POST", "/api/v1/integrations/boxoffice/test", nil, &chk); code != 200 {
		t.Fatalf("retest: %d", code)
	}
	var opts requestOptions
	if admin.do("GET", "/api/v1/requests/options", nil, &opts); !opts.Movies || !opts.Shows {
		t.Fatalf("options: %+v", opts)
	}

	// A quality picked under Radarr stays when Box Office is saved again.
	admin.do("PUT", "/api/v1/integrations/radarr", arr.Config{URL: bo.URL + "/radarr", QualityProfileID: 4, RootFolder: "/media/movie"}, nil)
	admin.do("PUT", "/api/v1/integrations/boxoffice", boxOfficeConfig{URL: bo.URL}, &view)
	admin.do("GET", "/api/v1/integrations", nil, &ints)
	if ints[arr.Radarr].QualityProfileID != 4 || !view.Connected {
		t.Fatalf("kept quality: %+v %+v", ints, view)
	}

	if code := admin.do("DELETE", "/api/v1/integrations/boxoffice", nil, nil); code != 204 {
		t.Fatalf("disconnect: %d", code)
	}
	admin.do("GET", "/api/v1/integrations/boxoffice", nil, &view)
	admin.do("GET", "/api/v1/integrations", nil, &ints)
	if view.Movies || view.Shows || ints[arr.Radarr].Connected || ints[arr.Sonarr].Connected {
		t.Fatalf("after: %+v %+v", view, ints)
	}
}

// Radarr and Sonarr behind a proxy at /radarr and /sonarr aren't Box Office,
// even once they're shut down, and they can still be disconnected.
func TestRealArrBehindProxyIsntBoxOffice(t *testing.T) {
	var srv *Server
	admin, _, db := newTestServerStore(t, func(s *Server) { srv = s })
	var login struct{ Token string }
	admin.do("POST", "/api/v1/setup", credentials{"admin", "correct horse"}, &login)
	admin.token = login.Token

	// Nothing answers at these addresses, like apps that were turned off.
	ctx := t.Context()
	for app, c := range map[arr.App]string{
		arr.Radarr: `{"url":"http://127.0.0.1:1/radarr","apiKey":"radarr-key","qualityProfileId":1,"rootFolder":"/movies"}`,
		arr.Sonarr: `{"url":"http://127.0.0.1:2/sonarr","apiKey":"sonarr-key","qualityProfileId":1,"rootFolder":"/tv"}`,
	} {
		if err := db.SetSetting(ctx, arr.SettingKey(app), c); err != nil {
			t.Fatal(err)
		}
	}
	var view boxOfficeView
	admin.do("GET", "/api/v1/integrations/boxoffice", nil, &view)
	if view.Movies || view.Shows {
		t.Fatalf("before a check: %+v", view)
	}
	srv.ReadBoxOffice(ctx)
	admin.do("GET", "/api/v1/integrations/boxoffice", nil, &view)
	if view.Movies || view.Shows {
		t.Fatalf("after a failed check: %+v", view)
	}

	for _, app := range []string{"radarr", "sonarr"} {
		if code := admin.do("DELETE", "/api/v1/integrations/"+app, nil, nil); code != 204 {
			t.Fatalf("disconnect %s: %d", app, code)
		}
	}
	srv.ReadBoxOffice(ctx)
	var ints map[arr.App]integrationView
	admin.do("GET", "/api/v1/integrations", nil, &ints)
	if ints[arr.Radarr].Connected || ints[arr.Sonarr].Connected {
		t.Fatalf("after disconnecting: %+v", ints)
	}
}

// A Box Office that stops answering is still Box Office.
func TestBoxOfficeDownStaysBoxOffice(t *testing.T) {
	bo := arrtest.NewBoxOffice(t)
	var srv *Server
	admin, _ := newTestServer(t, func(s *Server) { srv = s })
	var login struct{ Token string }
	admin.do("POST", "/api/v1/setup", credentials{"admin", "correct horse"}, &login)
	admin.token = login.Token
	var view boxOfficeView
	if code := admin.do("PUT", "/api/v1/integrations/boxoffice", boxOfficeConfig{URL: bo.URL, APIKey: bo.Key}, &view); code != 200 || !view.Connected {
		t.Fatalf("connect: %d %+v", code, view)
	}
	bo.Close()
	srv.ReadBoxOffice(t.Context())
	admin.do("GET", "/api/v1/integrations/boxoffice", nil, &view)
	if !view.Connected || view.Error == "" {
		t.Fatalf("down: %+v", view)
	}
}
