package importer_test

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"testing"

	"github.com/yeixio/matinee/internal/importer"
	"github.com/yeixio/matinee/internal/importer/importertest"
	"github.com/yeixio/matinee/internal/store"
	"github.com/yeixio/matinee/internal/store/storetest"
)

func pngBytes() []byte {
	var b bytes.Buffer
	png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 4, 4)))
	return b.Bytes()
}

type library struct {
	movie, other, ep1, ep2, show, season, track, book, part1, part2 int64
}

// matinee has a movie library under /mnt/media and a show, an album and a book.
func matinee(t *testing.T, st *store.Store) library {
	t.Helper()
	ctx := context.Background()
	var l library
	movies, _ := st.CreateLibrary(ctx, "Movies", "movies", []string{"/mnt/media/movies"})
	tv, _ := st.CreateLibrary(ctx, "TV", "tv", []string{"/mnt/media/tv"})
	music, _ := st.CreateLibrary(ctx, "Music", "music", []string{"/mnt/media/music"})
	books, _ := st.CreateLibrary(ctx, "Books", "audiobooks", []string{"/mnt/media/books"})
	file := func(lib int64, parent *int64, kind, title, path string) int64 {
		dur := int64(600000)
		id, err := st.UpsertFile(ctx, store.Item{LibraryID: lib, ParentID: parent, Kind: kind, Title: title, Path: path,
			DurationMs: &dur, AudioCodec: "aac", VideoCodec: "h264", Container: "mkv"})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	l.movie = file(movies.ID, nil, "movie", "Heat", "/mnt/media/movies/Heat (1995)/Heat (1995).mkv")
	l.other = file(movies.ID, nil, "movie", "Ronin", "/mnt/media/movies/Ronin (1998)/Ronin (1998).mkv")
	one, two := 1, 2
	l.show, _ = st.EnsureContainer(ctx, tv.ID, nil, "show", "Severance", nil, nil)
	l.season, _ = st.EnsureContainer(ctx, tv.ID, &l.show, "season", "Season 1", &one, nil)
	l.ep1 = file(tv.ID, &l.season, "episode", "Good News", "/mnt/media/tv/Severance/Season 1/S01E01.mkv")
	l.ep2 = file(tv.ID, &l.season, "episode", "Half Loop", "/mnt/media/tv/Severance/Season 1/S01E02.mkv")
	artist, _ := st.EnsureContainer(ctx, music.ID, nil, "artist", "Air", nil, nil)
	album, _ := st.EnsureContainer(ctx, music.ID, &artist, "album", "Moon Safari", nil, nil)
	l.track = file(music.ID, &album, "track", "La femme d'argent", "/mnt/media/music/Air/Moon Safari/01.flac")
	author, _ := st.EnsureContainer(ctx, books.ID, nil, "author", "Le Guin", nil, nil)
	l.book, _ = st.EnsureContainer(ctx, books.ID, &author, "book", "Earthsea", nil, nil)
	l.part1 = file(books.ID, &l.book, "part", "Part 1", "/mnt/media/books/Le Guin/Earthsea/1.m4b")
	l.part2 = file(books.ID, &l.book, "part", "Part 2", "/mnt/media/books/Le Guin/Earthsea/2.m4b")
	_ = two
	return l
}

func TestImport(t *testing.T) {
	ctx := context.Background()
	st := storetest.Open(t)
	l := matinee(t, st)
	mike, _ := st.CreateUser(ctx, "mike", "x", true)
	ellen, _ := st.CreateUser(ctx, "ellen", "x", false)

	src := importertest.New(t, t.TempDir())
	src.Account(1, "mikej")
	src.Account(5001, "Ellen")
	heat := src.Item(1, 0, "Heat", "movie-heat", "/data/movies/Heat (1995)/Heat (1995).mkv")
	ronin := src.Item(1, 0, "Ronin", "movie-ronin", "/data/movies/Ronin (1998)/Ronin (1998).mkv")
	src.Item(1, 0, "Gone", "movie-gone", "/data/movies/Gone/Gone.mkv")
	show := src.Item(2, 0, "Severance", "show-sev")
	season := src.Item(3, show, "Season 1", "season-sev1")
	src.Item(4, season, "Good News", "ep-1", "/data/tv/Severance/Season 1/S01E01.mkv")
	src.Item(4, season, "Half Loop", "ep-2", "/data/tv/Severance/Season 1/S01E02.mkv")
	artist := src.Item(8, 0, "Air", "artist-air")
	album := src.Item(9, artist, "Moon Safari", "album-ms")
	src.Item(10, album, "La femme d'argent", "track-1", "/data/music/Air/Moon Safari/01.flac")
	bookAlbum := src.Item(9, 0, "Earthsea", "book-es")
	src.Item(10, bookAlbum, "Part 1", "part-1", "/data/books/Le Guin/Earthsea/1.m4b")
	src.Item(10, bookAlbum, "Part 2", "part-2", "/data/books/Le Guin/Earthsea/2.m4b")

	src.View(1, "movie-heat", 0, 2, 1700000000) // watched
	src.View(1, "ep-1", 0, 1, 1700000000)       // watched
	src.View(1, "ep-2", 125000, 0, 1700000500)  // half way
	src.View(1, "movie-gone", 0, 1, 1700000000) // a file Matinee doesn't have
	src.View(1, "track-1", 0, 4, 1700000000)    // played song
	src.View(1, "part-1", 0, 1, 1700000000)     // finished part 1 of the book…
	src.View(1, "part-2", 60000, 0, 1700001000) // …and a minute into part 2
	src.View(5001, "movie-ronin", 300000, 0, 1) // Ellen's place, older than Matinee's
	src.View(5001, "movie-heat", 0, 1, 1700000000)
	src.Rate(1, "movie-heat", 8, 1700000000)     // four stars, on a watched movie
	src.Rate(1, "show-sev", 9, 1700000000)       // a show
	src.Rate(1, "album-ms", 7.0, 1700000000)     // an album
	src.Rate(1, "part-1", 6, 1700000000)         // a book's part: not rated alone
	src.Rate(5001, "movie-ronin", 2, 1700000000) // older than Ellen's rating here
	src.Rate(5001, "movie-gone", 10, 1700000000) // a file Matinee doesn't have
	src.Collection("Heists", heat, ronin, show)
	src.Poster(heat, "movie-heat", pngBytes())
	src.Poster(season, "season-sev1", pngBytes())
	src.Close()

	// Ellen already started Ronin on Matinee, more recently than there.
	if err := st.SetProgress(ctx, ellen.ID, l.other, 42000, false); err != nil {
		t.Fatal(err)
	}
	if err := st.SetRating(ctx, ellen.ID, l.other, 10); err != nil {
		t.Fatal(err)
	}

	loc, err := importer.Locate(src.Dir)
	if err != nil || loc.DataDir != src.Dir {
		t.Fatalf("locate: %+v %v", loc, err)
	}
	snap, err := importer.Read(ctx, loc)
	if err != nil {
		t.Fatal(err)
	}
	files, _ := st.FileRefs(ctx)

	plan := importer.NewPlan(snap, files, nil)
	if plan.Files != 8 || plan.Matched != 0 {
		t.Fatalf("before path changes: %d of %d", plan.Matched, plan.Files)
	}
	if len(plan.Suggested) != 1 || plan.Suggested[0].From != "/data" || plan.Suggested[0].To != "/mnt/media" || plan.Suggested[0].Files != 7 {
		t.Fatalf("suggested: %+v", plan.Suggested)
	}

	plan = importer.NewPlan(snap, files, []importer.PathChange{{From: "/data/", To: "/mnt/media"}})
	if plan.Files != 8 || plan.Matched != 7 || len(plan.Unmatched) != 1 || len(plan.Suggested) != 0 {
		t.Fatalf("after: %d of %d, %v %v", plan.Matched, plan.Files, plan.Unmatched, plan.Suggested)
	}
	accounts := plan.Accounts()
	if len(accounts) != 2 || accounts[0].Watched != 5 || accounts[0].InProgress != 2 || accounts[0].Found != 6 || accounts[1].Found != 2 ||
		accounts[0].Rated != 4 || accounts[1].Rated != 2 || accounts[1].Watched != 1 {
		t.Fatalf("accounts: %+v", accounts)
	}
	if plan.Collections() != 1 || plan.Posters() != 2 {
		t.Fatalf("collections %d posters %d", plan.Collections(), plan.Posters())
	}

	res, err := plan.Apply(ctx, st, importer.Options{
		Accounts: map[int64]int64{1: mike.ID, 5001: ellen.ID},
		Watched:  true, Collections: true, Posters: true, Ratings: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Mike: Heat, episode 1, the song, the book's progress; Ellen: Heat.
	// In progress: episode 2. Kept: Ellen's Ronin.
	if res.Watched != 4 || res.InProgress != 2 || res.Kept != 1 || res.Collections != 1 || res.CollectionItems != 3 || res.Posters != 2 || res.Ratings != 3 {
		t.Fatalf("result: %+v", res)
	}
	item := func(user, id int64) store.Item {
		it, err := st.Item(ctx, user, id)
		if err != nil {
			t.Fatal(err)
		}
		return it
	}
	if !item(mike.ID, l.movie).Played || !item(mike.ID, l.ep1).Played || !item(mike.ID, l.track).Played {
		t.Fatal("watched marks missing")
	}
	if ep := item(mike.ID, l.ep2); ep.Played || ep.PositionMs != 125000 {
		t.Fatalf("episode 2: %+v", ep)
	}
	if b := item(mike.ID, l.book); b.Played || b.PositionMs != 600000+60000 {
		t.Fatalf("book place: %+v", b.PositionMs)
	}
	if r := item(ellen.ID, l.other); r.PositionMs != 42000 {
		t.Fatalf("Ellen's newer place was replaced: %+v", r.PositionMs)
	}
	for id, want := range map[int64]int{l.movie: 8, l.show: 9} {
		if r := item(mike.ID, id).MyRating; r == nil || *r != want {
			t.Errorf("Mike's rating of %d: %v, want %d", id, r, want)
		}
	}
	albumItem, _ := st.Item(ctx, mike.ID, *item(0, l.track).ParentID)
	if albumItem.MyRating == nil || *albumItem.MyRating != 7 {
		t.Errorf("album rating: %v", albumItem.MyRating)
	}
	if b := item(mike.ID, l.book); b.MyRating != nil {
		t.Errorf("a part's rating went to the book: %v", *b.MyRating)
	}
	if r := item(ellen.ID, l.other).MyRating; r == nil || *r != 10 {
		t.Errorf("Ellen's newer rating was replaced: %v", r)
	}
	if item(ellen.ID, l.ep1).Played {
		t.Fatal("Mike's history went to Ellen")
	}
	cols, _ := st.Collections(ctx, mike.ID)
	var heists *store.SavedCollection
	for i := range cols {
		if cols[i].Name == "Heists" {
			heists = &cols[i]
		}
	}
	if heists == nil || !heists.Shared || heists.Kind != store.CollectionManual {
		t.Fatalf("collection: %+v", cols)
	}
	in, _ := st.ManualItems(ctx, mike.ID, heists.ID)
	if len(in) != 3 || in[0].ID != l.movie || in[1].ID != l.other || in[2].ID != l.show {
		t.Fatalf("collection items: %+v", in)
	}
	if item(mike.ID, l.movie).PosterTag == "" || item(mike.ID, l.season).PosterTag == "" {
		t.Fatal("posters not brought over")
	}

	// A rescan of the movie keeps the poster brought over.
	if _, err := st.UpsertFile(ctx, store.Item{LibraryID: item(0, l.movie).LibraryID, Kind: "movie", Title: "Heat",
		Path: "/mnt/media/movies/Heat (1995)/Heat (1995).mkv", Poster: "/mnt/media/movies/Heat (1995)/poster.jpg"}); err != nil {
		t.Fatal(err)
	}
	if item(mike.ID, l.movie).PosterTag == "" {
		t.Fatal("a rescan replaced the poster brought over")
	}

	// Running it again adds nothing twice.
	res, err = plan.Apply(ctx, st, importer.Options{Accounts: map[int64]int64{1: mike.ID}, Watched: true, Collections: true})
	if err != nil {
		t.Fatal(err)
	}
	in, _ = st.ManualItems(ctx, mike.ID, heists.ID)
	cols2, _ := st.Collections(ctx, mike.ID)
	if len(in) != 3 || len(cols2) != len(cols) {
		t.Fatalf("again: %d items, %d collections", len(in), len(cols2))
	}
}

func TestMapPath(t *testing.T) {
	changes := []importer.PathChange{
		{From: "/data", To: "/mnt/media"},
		{From: "/data/tv", To: "/srv/tv/"},
		{From: `D:\Media`, To: "/mnt/media"},
	}
	for in, want := range map[string]string{
		"/data/movies/a.mkv":   "/mnt/media/movies/a.mkv",
		"/data/tv/b.mkv":       "/srv/tv/b.mkv",
		"/database/c.mkv":      "/database/c.mkv",
		`D:\Media\Films\d.mkv`: "/mnt/media/Films/d.mkv",
		`d:\media\e.mkv`:       "/mnt/media/e.mkv",
	} {
		if got := importer.MapPath(in, changes); got != want {
			t.Errorf("%s: got %s, want %s", in, got, want)
		}
	}
	if got := importer.MapPath("/movies/f.mkv", []importer.PathChange{{From: "/movies", To: `M:\Films`}}); got != `M:\Films\f.mkv` {
		t.Errorf("to Windows: %s", got)
	}
}

func TestLocate(t *testing.T) {
	root := t.TempDir()
	src := importertest.New(t, root+"/backup/Media Server")
	src.Close()
	for _, p := range []string{root, root + "/backup", src.Dir, src.Dir + "/Plug-in Support/Databases/com.plexapp.plugins.library.db"} {
		loc, err := importer.Locate(p)
		if err != nil || loc.DataDir != src.Dir {
			t.Errorf("%s: %+v %v", p, loc, err)
		}
	}
	if _, err := importer.Locate(t.TempDir()); err != importer.ErrNoDatabase {
		t.Errorf("empty folder: %v", err)
	}
}
