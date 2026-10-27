package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/yeixio/matinee/i18n"
	"github.com/yeixio/matinee/internal/store"
)

// Titles come in the profile's language, or the device's until it picks one,
// where the online match has a translation.
func TestLanguages(t *testing.T) {
	translated := make(chan struct{}, 4)
	c, _, db := newTestServerStore(t, func(s *Server) {
		s.Translate = func(context.Context) { translated <- struct{}{} }
	})
	ctx := context.Background()
	var login struct{ Token string }
	c.do("POST", "/api/v1/setup", credentials{"admin", "correct horse"}, &login)
	c.token = login.Token

	var langs []i18n.Language
	signedOut := &client{t: t, base: c.base}
	if code := signedOut.do("GET", "/api/v1/languages", nil, &langs); code != 200 || len(langs) < 30 || langs[0].Code != "en" {
		t.Fatalf("languages: %d %v", code, langs)
	}

	tv, _ := db.CreateLibrary(ctx, "TV", "tv", []string{"/t"})
	show, _ := db.EnsureContainer(ctx, tv.ID, nil, "show", "The Bridge", nil, nil)
	db.ApplyMetadata(ctx, show, &store.Metadata{TMDBID: 1, Overview: "Two detectives."})
	db.ApplyDetails(ctx, show, store.DetailsUpdate{Details: store.Details{Tagline: "One body, two countries."}})
	season, _ := db.EnsureContainer(ctx, tv.ID, &show, "season", "Season 1", new(1), nil)
	ep, _ := db.UpsertFile(ctx, store.Item{LibraryID: tv.ID, ParentID: &season, Kind: "episode", Title: "Pilot",
		IndexNumber: new(1), Path: "/t/pilot.mkv"})
	db.SetTranslations(ctx, show, map[string]store.Translation{
		"de": {Title: "Die Brücke", Overview: "Zwei Ermittler.", Tagline: "Eine Leiche, zwei Länder."},
		"fr": {Title: "Bron"},
	})
	db.SetSeasonTranslations(ctx, season, "de", store.Translation{Title: "Staffel 1"}, map[int64]store.Translation{ep: {}})

	type view struct {
		Title, Overview, ShowTitle string
		Details                    *store.Details
	}
	get := func(lang string, id int64) view {
		t.Helper()
		req, _ := http.NewRequest("GET", c.base+"/api/v1/items/"+itoa(id), nil)
		req.Header.Set("Authorization", "Bearer "+c.token)
		req.Header.Set("Accept-Language", lang)
		var v view
		if code := doJSON(t, req, &v); code != 200 {
			t.Fatalf("item %d: %d", id, code)
		}
		return v
	}

	// A German browser gets the German title, description and tagline; one
	// asking for a language with no translation gets the original.
	if v := get("de-AT,de;q=0.9,en;q=0.5", show); v.Title != "Die Brücke" || v.Overview != "Zwei Ermittler." || v.Details == nil || v.Details.Tagline != "Eine Leiche, zwei Länder." {
		t.Errorf("German: %+v", v)
	}
	if v := get("fr-CA", show); v.Title != "Bron" || v.Overview != "Two detectives." {
		t.Errorf("French, title only: %+v", v)
	}
	if v := get("ja", show); v.Title != "The Bridge" {
		t.Errorf("Japanese: %+v", v)
	}
	if v := get("de", ep); v.Title != "Pilot" || v.ShowTitle != "Die Brücke" {
		t.Errorf("German episode: %+v", v)
	}
	if v := get("de", season); v.Title != "Staffel 1" {
		t.Errorf("German season: %+v", v)
	}
	// German and French were new to the server: their seasons get looked up.
	if got, _ := db.MetadataLanguages(ctx); len(got) != 3 {
		t.Errorf("languages in use: %v", got)
	}
	if len(translated) == 0 {
		t.Error("new languages weren't translated")
	}

	// A profile's pick wins over the device's, on every device.
	var mine myLanguage
	if c.do("GET", "/api/v1/me/language", nil, &mine); mine.Language != "" {
		t.Fatalf("no pick yet: %+v", mine)
	}
	var bad struct{ Error, Code string }
	if code := c.do("PUT", "/api/v1/me/language", myLanguage{Language: "xx"}, &bad); code != http.StatusBadRequest || bad.Code != "no_such_language" {
		t.Errorf("unknown language: %d %+v", code, bad)
	}
	c.do("PUT", "/api/v1/me/language", myLanguage{Language: "de"}, nil)
	if c.do("GET", "/api/v1/me/language", nil, &mine); mine.Language != "de" {
		t.Errorf("pick: %+v", mine)
	}
	if v := get("fr", show); v.Title != "Die Brücke" {
		t.Errorf("picked German on a French device: %+v", v)
	}

	// Search finds titles by their translation too.
	var found []store.Item
	c.do("GET", "/api/v1/search?q=br%C3%BCcke", nil, &found)
	if len(found) != 1 || found[0].Title != "Die Brücke" {
		t.Errorf("search: %+v", found)
	}
}

func doJSON(t *testing.T, req *http.Request, out any) int {
	t.Helper()
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if err := json.NewDecoder(res.Body).Decode(out); err != nil {
		t.Fatal(err)
	}
	return res.StatusCode
}

func TestErrorCode(t *testing.T) {
	for msg, want := range map[string]string{
		"sign in required":        "sign_in_required",
		"couldn't reach Radarr":   "couldnt_reach_radarr",
		"a PIN is 4 digits":       "a_pin_is_4_digits",
		"name: too long (max 40)": "name_too_long_max_40",
	} {
		if got := errorCode(msg); got != want {
			t.Errorf("%q: %q, want %q", msg, got, want)
		}
	}
}
