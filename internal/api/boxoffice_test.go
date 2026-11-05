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
