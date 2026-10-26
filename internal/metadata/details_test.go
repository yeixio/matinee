package metadata

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yeixio/matinee/internal/rating"
	"github.com/yeixio/matinee/internal/store"
	"github.com/yeixio/matinee/internal/store/storetest"
)

func person(id int, name, character string, order int) map[string]any {
	return map[string]any{"id": id, "name": name, "character": character, "order": order, "profile_path": "/" + name + ".jpg"}
}

func crewMember(id int, name, job, dept string) map[string]any {
	return map[string]any{"id": id, "name": name, "job": job, "department": dept}
}

func movie(collection map[string]any, cast []map[string]any, crew []map[string]any, recs ...int) map[string]any {
	var rs []map[string]any
	for _, r := range recs {
		rs = append(rs, map[string]any{"id": r})
	}
	return map[string]any{
		"tagline": "A tagline.", "original_language": "en", "status": "Released", "release_date": "1995-12-15",
		"genres":                []map[string]any{{"id": 28, "name": "Action"}, {"id": 80, "name": "Crime"}},
		"production_companies":  []map[string]any{{"id": 1, "name": "Studio"}},
		"production_countries":  []map[string]any{{"iso_3166_1": "US", "name": "United States of America"}},
		"spoken_languages":      []map[string]any{{"iso_639_1": "en", "english_name": "English", "name": "English"}},
		"budget":                60000000,
		"revenue":               187436818,
		"belongs_to_collection": collection,
		"imdb_id":               "tt0113277",
		"external_ids":          map[string]any{"wikidata_id": "Q1"},
		"credits":               map[string]any{"cast": cast, "crew": crew},
		"release_dates": map[string]any{"results": []map[string]any{
			{"iso_3166_1": "GB", "release_dates": []map[string]any{{"certification": "15", "type": 3}}},
			{"iso_3166_1": "US", "release_dates": []map[string]any{{"certification": "", "type": 1}, {"certification": "R", "type": 3}}},
		}},
		"keywords":        map[string]any{"keywords": []map[string]any{{"id": 10051, "name": "heist"}}},
		"recommendations": map[string]any{"results": rs},
	}
}

func fakeDetails(t *testing.T) (*TMDB, *Wikidata) {
	mann := crewMember(1, "Michael Mann", "Director", "Directing")
	alienSeries := map[string]any{"id": 8091, "name": "Alien Collection"}
	movies := map[string]map[string]any{
		"949": movie(nil, []map[string]any{person(1158, "Al Pacino", "Vincent Hanna", 0), person(380, "Robert De Niro", "Neil McCauley", 1)},
			[]map[string]any{mann, crewMember(1, "Michael Mann", "Screenplay", "Writing"), crewMember(2, "Elliot Goldenthal", "Original Music Composer", "Sound"),
				crewMember(3, "Grip", "Key Grip", "Crew")}),
		"1538": movie(nil, []map[string]any{person(500, "Tom Cruise", "Vincent", 0)}, []map[string]any{mann}),
		"348":  movie(alienSeries, []map[string]any{person(10205, "Sigourney Weaver", "Ripley", 0)}, nil, 679),
		"679":  movie(alienSeries, []map[string]any{person(10205, "Sigourney Weaver", "Ripley", 0)}, nil),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /movie/{id}", func(w http.ResponseWriter, r *http.Request) {
		m, ok := movies[r.PathValue("id")]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if !strings.Contains(r.URL.Query().Get("append_to_response"), "credits") {
			t.Errorf("details asked without credits: %s", r.URL)
		}
		json.NewEncoder(w).Encode(m)
	})
	mux.HandleFunc("GET /tv/1396", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"name": "Breaking Bad", "status": "Ended", "first_air_date": "2008-01-20", "last_air_date": "2013-09-29",
			"number_of_seasons": 5, "number_of_episodes": 62,
			"networks":   []map[string]any{{"id": 174, "name": "AMC"}},
			"created_by": []map[string]any{{"id": 66633, "name": "Vince Gilligan"}},
			"aggregate_credits": map[string]any{"cast": []map[string]any{{
				"id": 17419, "name": "Bryan Cranston", "total_episode_count": 62,
				"roles": []map[string]any{{"character": "Walter White", "episode_count": 62}},
			}}},
			"content_ratings": map[string]any{"results": []map[string]any{{"iso_3166_1": "US", "rating": "TV-MA"}}},
		})
	})
	mux.HandleFunc("GET /tv/1396/season/1", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"air_date": "2008-01-20",
			"episodes": []map[string]any{{
				"episode_number": 1, "air_date": "2008-01-20",
				"crew":        []map[string]any{crewMember(66633, "Vince Gilligan", "Director", "Directing")},
				"guest_stars": []map[string]any{person(1000, "Guest Person", "Krazy-8", 0)},
			}},
			"aggregate_credits": map[string]any{"cast": []map[string]any{{
				"id": 17419, "name": "Bryan Cranston", "total_episode_count": 7,
				"roles": []map[string]any{{"character": "Walter White", "episode_count": 7}},
			}}},
		})
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	tm := NewTMDB("k")
	tm.BaseURL, tm.ImageURL = ts.URL, "https://img/"

	wd := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		b := func(award, label, won, year, who string) map[string]any {
			row := map[string]any{"award": map[string]string{"value": award}, "awardLabel": map[string]string{"value": label},
				"won": map[string]string{"value": won}}
			if year != "" {
				row["year"] = map[string]string{"value": year}
			}
			if who != "" {
				row["whoLabel"] = map[string]string{"value": who}
			}
			return row
		}
		json.NewEncoder(w).Encode(map[string]any{"results": map[string]any{"bindings": []map[string]any{
			b("http://www.wikidata.org/entity/Q102427", "Academy Award for Best Picture", "false", "1996", ""),
			b("http://www.wikidata.org/entity/Q103916", "Academy Award for Best Actor", "true", "1996", "Al Pacino"),
			b("http://www.wikidata.org/entity/Q103916", "Academy Award for Best Actor", "false", "1996", "Al Pacino"),
			b("http://www.wikidata.org/entity/Q5", "Saturn Award for Best Action Film", "true", "1996", ""),
			b("http://www.wikidata.org/entity/Q6", "Q6", "true", "", ""),
		}}})
	}))
	t.Cleanup(wd.Close)
	w := NewWikidata("test")
	w.Endpoint = wd.URL
	return tm, w
}

func TestFetchDetails(t *testing.T) {
	st := storetest.Open(t)
	ctx := context.Background()
	tm, wd := fakeDetails(t)
	f := &Fetcher{Store: st, Providers: []Provider{tm}, Awards: wd, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}

	lib, _ := st.CreateLibrary(ctx, "Movies", "movies", []string{"/m"})
	ids := map[string]int64{}
	for _, m := range []struct {
		title string
		tmdb  int64
	}{{"Heat", 949}, {"Collateral", 1538}, {"Alien", 348}, {"Aliens", 679}} {
		id, err := st.UpsertFile(ctx, store.Item{LibraryID: lib.ID, Kind: "movie", Title: m.title, Path: "/m/" + m.title + ".mkv"})
		if err != nil {
			t.Fatal(err)
		}
		st.ApplyMetadata(ctx, id, &store.Metadata{TMDBID: m.tmdb})
		ids[m.title] = id
	}
	f.FetchDetails(ctx, lib)

	d, err := st.ItemDetails(ctx, ids["Heat"])
	if err != nil || d == nil {
		t.Fatalf("details: %v %v", d, err)
	}
	if d.Tagline != "A tagline." || d.Certification != "R" || d.OriginalLanguage != "English" || d.Budget != 60000000 ||
		len(d.Genres) != 2 || d.Studios[0] != "Studio" || d.OriginalTitle != "" || d.IMDbID != "tt0113277" {
		t.Fatalf("details: %+v", d)
	}
	// The win isn't also listed as a nomination, and unlabelled awards are left out.
	if len(d.Awards) != 3 || !d.Awards[0].Won || d.Awards[0].Names[0] != "Al Pacino" {
		t.Fatalf("awards: %+v", d.Awards)
	}
	if d.AwardSummary != "Won 1 Academy Award · 2 wins and 1 nomination" {
		t.Fatalf("award summary: %q", d.AwardSummary)
	}
	c, _ := st.ItemCredits(ctx, ids["Heat"])
	if len(c.Cast) != 2 || c.Cast[0].Name != "Al Pacino" || c.Cast[0].Role != "Vincent Hanna" || !c.Cast[0].HasPhoto {
		t.Fatalf("cast: %+v", c.Cast)
	}
	// Key jobs only, most important first; one person can hold two.
	var jobs []string
	for _, cr := range c.Crew {
		jobs = append(jobs, cr.Name+": "+cr.Role)
	}
	if strings.Join(jobs, ", ") != "Michael Mann: Director, Michael Mann: Screenplay, Elliot Goldenthal: Original Music Composer" {
		t.Fatalf("crew: %v", jobs)
	}

	// Same director: related. Same collection: in the collection, not related.
	heat, _ := st.Item(ctx, 0, ids["Heat"])
	rel, _ := st.Related(ctx, 0, heat)
	if len(rel) != 1 || rel[0].Title != "Collateral" {
		t.Fatalf("related to Heat: %+v", rel)
	}
	alien, _ := st.Item(ctx, 0, ids["Alien"])
	if rel, _ := st.Related(ctx, 0, alien); len(rel) != 0 {
		t.Fatalf("collection members shouldn't be related too: %+v", rel)
	}
	coll, _ := st.CollectionItems(ctx, 0, 8091)
	if len(coll) != 2 {
		t.Fatalf("collection: %+v", coll)
	}

	titles, _ := st.PersonTitles(ctx, 0, 1)
	if len(titles) != 2 || strings.Join(titles[0].Roles, ",")+"|"+strings.Join(titles[1].Roles, ",") == "" {
		t.Fatalf("Michael Mann's titles: %+v", titles)
	}
	for _, pt := range titles {
		if pt.Item.Title == "Heat" && strings.Join(pt.Roles, ", ") != "Director, Screenplay" {
			t.Fatalf("roles in Heat: %v", pt.Roles)
		}
	}

	// Nothing left until the next refresh.
	if left, _ := st.NeedsDetails(ctx, lib.ID, 1); len(left) != 0 {
		t.Fatalf("still needing details: %+v", left)
	}
}

func TestFetchShowDetails(t *testing.T) {
	st := storetest.Open(t)
	ctx := context.Background()
	tm, _ := fakeDetails(t)
	f := &Fetcher{Store: st, Providers: []Provider{tm}, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}

	lib, _ := st.CreateLibrary(ctx, "TV", "tv", []string{"/tv"})
	show, _ := st.EnsureContainer(ctx, lib.ID, nil, "show", "Breaking Bad", nil, nil)
	one := 1
	season, _ := st.EnsureContainer(ctx, lib.ID, &show, "season", "Season 1", &one, nil)
	ep, _ := st.UpsertFile(ctx, store.Item{LibraryID: lib.ID, ParentID: &season, Kind: "episode", Title: "Pilot", IndexNumber: &one, Path: "/tv/1.mkv"})
	st.ApplyMetadata(ctx, show, &store.Metadata{TMDBID: 1396})
	st.ApplyMetadata(ctx, season, &store.Metadata{TMDBID: 3572})
	f.FetchDetails(ctx, lib)

	d, _ := st.ItemDetails(ctx, show)
	if d == nil || d.Certification != "TV-MA" || d.EndDate != "2013-09-29" || d.Seasons != 5 || d.Networks[0] != "AMC" {
		t.Fatalf("show details: %+v", d)
	}
	// TV-MA is for adults, so a teens profile sees neither the show nor its episodes.
	acct, _ := st.CreateUser(ctx, "parent", "hash", false)
	teens := rating.Teens
	kid, err := st.CreateProfile(ctx, acct.ID, store.ProfileChanges{KidsLevel: &teens, Libraries: map[int64]string{lib.ID: store.LibraryRated}})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{show, season, ep} {
		if _, err := st.Item(ctx, kid.ID, id); err != store.ErrNotFound {
			t.Errorf("teens profile opening %d: %v", id, err)
		}
		if _, err := st.Item(ctx, acct.ID, id); err != nil {
			t.Errorf("account opening %d: %v", id, err)
		}
	}
	c, _ := st.ItemCredits(ctx, show)
	if len(c.Cast) != 1 || c.Cast[0].Role != "Walter White" || c.Cast[0].Episodes != 62 || c.Crew[0].Role != "Creator" {
		t.Fatalf("show credits: %+v", c)
	}
	ec, _ := st.ItemCredits(ctx, ep)
	if len(ec.Guests) != 1 || ec.Guests[0].Role != "Krazy-8" || len(ec.Crew) != 1 || ec.Crew[0].Role != "Director" {
		t.Fatalf("episode credits: %+v", ec)
	}
	// Vince Gilligan created the show and directed an episode: one entry.
	titles, _ := st.PersonTitles(ctx, 0, 66633)
	if len(titles) != 1 || titles[0].Item.ID != show || strings.Join(titles[0].Roles, ", ") != "Creator, Director · 1 episode" {
		t.Fatalf("Vince Gilligan's titles: %+v", titles)
	}
}

func TestAwardSummary(t *testing.T) {
	for _, tc := range []struct {
		awards []store.Award
		want   string
	}{
		{nil, ""},
		{[]store.Award{{Name: "Primetime Emmy Award for Outstanding Drama Series", Won: true}, {Name: "Peabody Award", Won: true}}, "Won 1 Primetime Emmy · 2 wins"},
		{[]store.Award{{Name: "Academy Award for Best Picture"}, {Name: "Academy Award for Best Director"}}, "Nominated for 2 Academy Awards · 2 nominations"},
		{[]store.Award{{Name: "Saturn Award", Won: true}}, "1 win"},
	} {
		if got := AwardSummary(tc.awards); got != tc.want {
			t.Errorf("AwardSummary(%v) = %q, want %q", tc.awards, got, tc.want)
		}
	}
}
