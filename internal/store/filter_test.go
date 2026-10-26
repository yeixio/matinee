package store_test

import (
	"context"
	"encoding/json"
	"net/url"
	"reflect"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/yeixio/matinee/internal/store"
	"github.com/yeixio/matinee/internal/store/storetest"
)

// A filter reads the same from URL parameters as from JSON, and back.
func TestItemFilterRoundTrip(t *testing.T) {
	q, _ := url.ParseQuery("genre=Comedy&genre=Drama&notGenre=Horror&yearMin=1980&yearMax=1989&releasedWithin=90" +
		"&addedAfter=2026-01-01&ratingMin=7.5&runtimeMax=95&watched=unwatched&tag=4k&tag=hdr&person=1,2&director=3" +
		"&studio=A24&country=Japan&language=Japanese&certification=PG-13&collection=8091&watchList=true&sort=year&order=asc&limit=50&q=star&kind=movie&library=4&other=x")
	f, err := store.ParseItemFilter(q)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(f.Person, []int64{1, 2}) || f.RatingMin != 7.5 || !f.WatchList || f.Sort != "year" {
		t.Fatalf("parsed %+v", f)
	}
	back, err := store.ParseItemFilter(f.Values())
	if err != nil || !reflect.DeepEqual(back, f) {
		t.Fatalf("round trip: %+v, %v", back, err)
	}
	js, _ := json.Marshal(f)
	var fromJSON store.ItemFilter
	json.Unmarshal(js, &fromJSON)
	if !reflect.DeepEqual(fromJSON, f) {
		t.Fatalf("json: %s", js)
	}
	// JSON names are the parameter names.
	var m map[string]any
	json.Unmarshal(js, &m)
	for name := range m {
		if !q.Has(name) {
			t.Errorf("json field %q is not a query parameter", name)
		}
	}

	for _, bad := range []string{"yearMin=x", "watched=maybe", "sort=colour", "order=up", "addedAfter=yesterday", "person=bob", "kind=film", "ratingMin=-1"} {
		q, _ := url.ParseQuery(bad)
		if _, err := store.ParseItemFilter(q); err == nil {
			t.Errorf("%s: no error", bad)
		}
	}
}

func TestFilterItems(t *testing.T) {
	db := storetest.Open(t)
	ctx := context.Background()
	user, err := db.CreateUser(ctx, "mike", "hash", true)
	if err != nil {
		t.Fatal(err)
	}
	other, _ := db.CreateUser(ctx, "ellen", "hash", false)
	movies, _ := db.CreateLibrary(ctx, "Movies", "movies", []string{"/m"})
	shows, _ := db.CreateLibrary(ctx, "Shows", "shows", []string{"/s"})
	day := int64(86400)
	now := time.Now().Unix()

	ms := func(min int64) *int64 { v := min * 60000; return &v }
	ids := map[string]int64{}
	movie := func(title string, year int, rating float64, minutes int64, addedDaysAgo int64, genres []string, tags []string, d store.DetailsUpdate) {
		id, err := db.UpsertFile(ctx, store.Item{LibraryID: movies.ID, Kind: "movie", Title: title, Year: &year, Path: "/m/" + title + ".mkv", DurationMs: ms(minutes)})
		if err != nil {
			t.Fatal(err)
		}
		db.ApplyMetadata(ctx, id, &store.Metadata{TMDBID: id, Rating: rating, Genres: genres})
		if err := db.ApplyDetails(ctx, id, d); err != nil {
			t.Fatal(err)
		}
		if tags != nil {
			db.SetMediaInfo(ctx, id, 0, 0, "{}", tags, "k")
		}
		db.SetAddedAt(ctx, id, now-addedDaysAgo*day)
		ids[title] = id
	}
	scott := store.Credit{PersonID: 578, Name: "Ridley Scott", Role: "Director", Department: "Directing"}
	weaver := store.Credit{PersonID: 10205, Name: "Sigourney Weaver", Role: "Ripley"}
	movie("Alien", 1979, 8.2, 117, 400, []string{"Horror", "Science Fiction"}, []string{"4k", "hdr"}, store.DetailsUpdate{
		Details: store.Details{ReleaseDate: "1979-05-25", Studios: []string{"20th Century Fox"}, Countries: []string{"United Kingdom"}, OriginalLanguage: "English", Certification: "R", Collection: &store.Collection{ID: 8091, Name: "Alien Collection"}},
		Cast:    []store.Credit{weaver}, Crew: []store.Credit{scott},
		Terms: []store.Term{{Kind: "collection", Term: "8091"}},
	})
	movie("Aliens", 1986, 7.9, 137, 10, []string{"Action", "Science Fiction"}, []string{"1080p"}, store.DetailsUpdate{
		Details: store.Details{ReleaseDate: "1986-07-18", Studios: []string{"20th Century Fox"}, Certification: "R", Collection: &store.Collection{ID: 8091, Name: "Alien Collection"}},
		Cast:    []store.Credit{weaver},
		Terms:   []store.Term{{Kind: "collection", Term: "8091"}},
	})
	movie("Spirited Away", 2001, 8.5, 125, 3, []string{"Animation", "Family"}, []string{"4k"}, store.DetailsUpdate{
		Details: store.Details{ReleaseDate: "2001-07-20", Studios: []string{"Studio Ghibli"}, Countries: []string{"Japan"}, OriginalLanguage: "Japanese", Certification: "PG"},
	})
	movie("Short Film", 2026, 0, 12, 1, nil, nil, store.DetailsUpdate{
		Details: store.Details{ReleaseDate: time.Now().AddDate(0, 0, -20).Format(time.DateOnly)},
	})

	// A show with two seasons; its added date is its newest episode's.
	y := 2008
	show, _ := db.EnsureContainer(ctx, shows.ID, nil, "show", "Breaking Bad", nil, &y)
	db.ApplyMetadata(ctx, show, &store.Metadata{TMDBID: 1396, Rating: 8.9, Genres: []string{"Drama", "Crime"}})
	db.ApplyDetails(ctx, show, store.DetailsUpdate{
		Details: store.Details{ReleaseDate: "2008-01-20", Networks: []string{"AMC"}, Certification: "TV-MA"},
		Crew:    []store.Credit{{PersonID: 66633, Name: "Vince Gilligan", Role: "Creator"}},
	})
	var eps []int64
	for s := 1; s <= 2; s++ {
		season, _ := db.EnsureContainer(ctx, shows.ID, &show, "season", "Season", &s, nil)
		for e := 1; e <= 2; e++ {
			id, _ := db.UpsertFile(ctx, store.Item{LibraryID: shows.ID, ParentID: &season, Kind: "episode", Title: "Episode", IndexNumber: &e,
				Path: "/s/bb/" + string(rune('0'+s)) + string(rune('0'+e)) + ".mkv", DurationMs: ms(47)})
			db.SetAddedAt(ctx, id, now-200*day)
			eps = append(eps, id)
		}
	}
	db.SetAddedAt(ctx, show, now-900*day)
	db.SetMediaInfo(ctx, eps[3], 0, 0, "{}", []string{"4k"}, "k")
	// A guest director on one episode counts toward the show.
	db.ApplyEpisodeDetails(ctx, eps[2], store.DetailsUpdate{Crew: []store.Credit{{PersonID: 1, Name: "Rian Johnson", Role: "Director"}}})

	// Mike watched Alien and half of Breaking Bad; started Aliens.
	db.SetProgress(ctx, user.ID, ids["Alien"], 0, true)
	db.SetProgress(ctx, user.ID, ids["Aliens"], 60000, false)
	db.SetProgress(ctx, user.ID, eps[0], 0, true)
	db.SetProgress(ctx, user.ID, eps[1], 0, true)
	db.AddToWatchList(ctx, user.ID, ids["Spirited Away"])

	run := func(userID int64, query string) []string {
		t.Helper()
		q, _ := url.ParseQuery(query)
		f, err := store.ParseItemFilter(q)
		if err != nil {
			t.Fatalf("%s: %v", query, err)
		}
		items, err := db.FilterItems(ctx, userID, f)
		if err != nil {
			t.Fatalf("%s: %v", query, err)
		}
		titles := []string{}
		for _, it := range items {
			titles = append(titles, it.Title)
		}
		return titles
	}
	lib := "library=" + itoa(movies.ID) + "&"
	tv := "library=" + itoa(shows.ID) + "&"
	for _, c := range []struct {
		user  int64
		query string
		want  []string
	}{
		{user.ID, lib, []string{"Alien", "Aliens", "Short Film", "Spirited Away"}},
		{user.ID, lib + "genre=Science Fiction&genre=Animation", []string{"Alien", "Aliens", "Spirited Away"}},
		{user.ID, lib + "genre=Science Fiction&notGenre=Horror", []string{"Aliens"}},
		{user.ID, lib + "yearMin=1980&yearMax=2001", []string{"Aliens", "Spirited Away"}},
		{user.ID, lib + "releasedAfter=1986-07-01&releasedBefore=2001-07-20", []string{"Aliens", "Spirited Away"}},
		{user.ID, lib + "releasedWithin=30", []string{"Short Film"}},
		{user.ID, lib + "addedWithin=7", []string{"Short Film", "Spirited Away"}},
		{user.ID, lib + "addedAfter=" + time.Unix(now-11*day, 0).Format(time.DateOnly), []string{"Aliens", "Short Film", "Spirited Away"}},
		{user.ID, lib + "ratingMin=8", []string{"Alien", "Spirited Away"}},
		{user.ID, lib + "runtimeMin=120&runtimeMax=130", []string{"Spirited Away"}},
		{user.ID, lib + "tag=4k", []string{"Alien", "Spirited Away"}},
		{user.ID, lib + "tag=4k&tag=hdr", []string{"Alien"}},
		{user.ID, lib + "notTag=4k", []string{"Aliens", "Short Film"}},
		{user.ID, lib + "watched=watched", []string{"Alien"}},
		{user.ID, lib + "watched=unwatched", []string{"Aliens", "Short Film", "Spirited Away"}},
		{user.ID, lib + "watched=inProgress", []string{"Aliens"}},
		{user.ID, lib + "watched=notStarted", []string{"Short Film", "Spirited Away"}},
		{other.ID, lib + "watched=watched", []string{}},
		{user.ID, lib + "person=10205", []string{"Alien", "Aliens"}},
		{user.ID, lib + "person=10205&person=578", []string{"Alien"}},
		{user.ID, lib + "director=578", []string{"Alien"}},
		{user.ID, lib + "studio=20th Century Fox", []string{"Alien", "Aliens"}},
		{user.ID, lib + "country=Japan", []string{"Spirited Away"}},
		{user.ID, lib + "language=Japanese&language=English", []string{"Alien", "Spirited Away"}},
		{user.ID, lib + "certification=PG&certification=PG-13", []string{"Spirited Away"}},
		{user.ID, lib + "collection=8091", []string{"Alien", "Aliens"}},
		{user.ID, lib + "watchList=true", []string{"Spirited Away"}},
		{other.ID, lib + "watchList=true", []string{}},
		{user.ID, lib + "q=ALI", []string{"Alien", "Aliens"}},
		{user.ID, lib + "q=100%25", []string{}},

		{user.ID, lib + "sort=year", []string{"Short Film", "Spirited Away", "Aliens", "Alien"}},
		{user.ID, lib + "sort=year&order=asc", []string{"Alien", "Aliens", "Spirited Away", "Short Film"}},
		{user.ID, lib + "sort=released&limit=2", []string{"Short Film", "Spirited Away"}},
		{user.ID, lib + "sort=added", []string{"Short Film", "Spirited Away", "Aliens", "Alien"}},
		{user.ID, lib + "sort=rating", []string{"Spirited Away", "Alien", "Aliens", "Short Film"}},
		{user.ID, lib + "sort=runtime", []string{"Short Film", "Alien", "Spirited Away", "Aliens"}},
		{user.ID, lib + "sort=lastPlayed&limit=2", []string{"Alien", "Aliens"}},

		// Shows count what's in their episodes.
		{user.ID, tv + "watched=inProgress", []string{"Breaking Bad"}},
		{user.ID, tv + "watched=watched", []string{}},
		{other.ID, tv + "watched=notStarted", []string{"Breaking Bad"}},
		{user.ID, tv + "tag=4k", []string{"Breaking Bad"}},
		{user.ID, tv + "addedWithin=300", []string{"Breaking Bad"}},
		{user.ID, tv + "runtimeMin=45&runtimeMax=50", []string{"Breaking Bad"}},
		{user.ID, tv + "director=66633", []string{"Breaking Bad"}},
		{user.ID, tv + "director=1", []string{"Breaking Bad"}},
		{user.ID, tv + "studio=AMC&certification=TV-MA&genre=Crime", []string{"Breaking Bad"}},
		{user.ID, tv + "releasedBefore=2008-01-19", []string{}},

		// Across libraries, and by kind.
		{user.ID, "genre=Drama&genre=Family", []string{"Breaking Bad", "Spirited Away"}},
		{user.ID, "kind=episode&watched=watched", []string{"Episode", "Episode"}},
	} {
		got := run(c.user, c.query)
		if !slices.Equal(got, c.want) {
			t.Errorf("%s: got %q, want %q", c.query, got, c.want)
		}
	}

	o, err := db.FilterOptions(ctx, movies.ID)
	if err != nil {
		t.Fatal(err)
	}
	if o.YearMin != 1979 || o.YearMax != 2026 || !o.HasRatings || !o.HasReleased ||
		len(o.Genres) != 5 || o.Genres[0] != (store.FilterValue{Value: "Science Fiction", Count: 2}) ||
		o.Tags[0] != (store.FilterValue{Value: "4k", Count: 2}) ||
		len(o.Studios) != 2 || o.Studios[0].Value != "20th Century Fox" ||
		len(o.Collections) != 1 || o.Collections[0] != (store.FilterValue{Value: "8091", Label: "Alien Collection", Count: 2}) ||
		len(o.Directors) != 1 || o.Directors[0].Name != "Ridley Scott" || len(o.Cast) != 1 || o.Cast[0].Count != 2 {
		t.Fatalf("options: %+v", o)
	}
	people, err := db.FilterPeople(ctx, shows.ID, "director", "joh", 10)
	if err != nil || len(people) != 1 || people[0].Name != "Rian Johnson" || people[0].Count != 1 {
		t.Fatalf("people: %+v %v", people, err)
	}
	if people, _ := db.FilterPeople(ctx, movies.ID, "", "SIG", 10); len(people) != 1 {
		t.Fatalf("people by first name: %+v", people)
	}

	// Details stored before Matinee kept filter terms get them on start.
	if err := db.ForgetFilterBackfill(ctx); err != nil {
		t.Fatal(err)
	}
	if got := run(user.ID, lib+"studio=Studio Ghibli"); len(got) != 0 {
		t.Fatalf("before backfill: %q", got)
	}
	if err := db.BackfillFilters(ctx); err != nil {
		t.Fatal(err)
	}
	if got := run(user.ID, lib+"studio=Studio Ghibli&releasedAfter=2001-07-01"); !slices.Equal(got, []string{"Spirited Away"}) {
		t.Fatalf("after backfill: %q", got)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
