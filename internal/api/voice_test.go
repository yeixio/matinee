package api

import (
	"context"
	"net/url"
	"testing"

	"github.com/yeixio/matinee/internal/store"
)

// A voice request plays the one thing that fits best: of the kind asked
// for, by whoever was named, closest title first, and for a plain "play X"
// an artist, then an album, a book, a song.
func TestSearchPlay(t *testing.T) {
	c, _, db := newTestServerStore(t)
	ctx := context.Background()
	var login struct{ Token string }
	c.do("POST", "/api/v1/setup", credentials{"admin", "correct horse"}, &login)
	c.token = login.Token

	music, _ := db.CreateLibrary(ctx, "Music", "music", []string{"/music"})
	books, _ := db.CreateLibrary(ctx, "Books", "audiobooks", []string{"/books"})
	movies, _ := db.CreateLibrary(ctx, "Movies", "movies", []string{"/movies"})
	ids := map[string]int64{}
	artist := func(name string) int64 {
		id, _ := db.EnsureContainer(ctx, music.ID, nil, "artist", name, nil, nil)
		ids[name] = id
		return id
	}
	album := func(by int64, title string) int64 {
		id, _ := db.EnsureContainer(ctx, music.ID, &by, "album", title, nil, nil)
		ids["album:"+title] = id
		return id
	}
	n := 0
	track := func(album int64, title string) {
		n++
		id, err := db.UpsertFile(ctx, store.Item{LibraryID: music.ID, ParentID: &album, Kind: "track", Title: title,
			IndexNumber: new(n), Path: "/music/" + itoa(int64(n)) + ".flac"})
		if err != nil {
			t.Fatal(err)
		}
		ids["track:"+title+":"+itoa(album)] = id
	}
	beatles, queen, mj := artist("The Beatles"), artist("Queen"), artist("Michael Jackson")
	abbey := album(beatles, "Abbey Road")
	help := album(beatles, "Help!")
	track(abbey, "Something")
	track(help, "Yesterday")
	track(help, "Help!")
	opera := album(queen, "A Night at the Opera")
	track(opera, "Bohemian Rhapsody")
	track(opera, "Yesterday's Hero")
	thriller := album(mj, "Thriller")
	track(thriller, "Thriller")
	track(thriller, "Beat It")
	ben := artist("Ben E. King")
	track(album(ben, "Don't Play That Song"), "Stand by Me")
	hobbit, err := db.UpsertFile(ctx, store.Item{LibraryID: books.ID, Kind: "book", Title: "The Hobbit", Path: "/books/hobbit.m4b"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.UpsertFile(ctx, store.Item{LibraryID: movies.ID, Kind: "movie", Title: "Something Wild", Path: "/movies/sw.mkv"}); err != nil {
		t.Fatal(err)
	}

	ask := func(params url.Values) (voiceMatch, int) {
		t.Helper()
		var m voiceMatch
		code := c.do("GET", "/api/v1/search/play?"+params.Encode(), nil, &m)
		return m, code
	}
	for _, tc := range []struct {
		params url.Values
		kind   string
		id     int64
	}{
		{url.Values{"q": {"abbey road"}}, "album", abbey},
		{url.Values{"q": {"Beatles"}}, "artist", beatles},
		// An album over the song it's named after.
		{url.Values{"q": {"Thriller"}}, "album", thriller},
		{url.Values{"q": {"the song Thriller"}}, "track", ids["track:Thriller:"+itoa(thriller)]},
		{url.Values{"q": {"Thriller"}, "kind": {"track"}}, "track", ids["track:Thriller:"+itoa(thriller)]},
		// Closest title first: Yesterday, not Yesterday's Hero.
		{url.Values{"q": {"yesterday"}}, "track", ids["track:Yesterday:"+itoa(help)]},
		{url.Values{"q": {"Yesterday by Queen"}}, "track", ids["track:Yesterday's Hero:"+itoa(opera)]},
		{url.Values{"q": {"yesterday"}, "artist": {"Beatles"}}, "track", ids["track:Yesterday:"+itoa(help)]},
		// "by" in the title itself.
		{url.Values{"q": {"Stand by Me"}}, "track", ids["track:Stand by Me:"+itoa(ids["album:Don't Play That Song"])]},
		{url.Values{"q": {"songs by Queen"}}, "artist", queen},
		{url.Values{"artist": {"Queen"}, "kind": {"artist"}}, "artist", queen},
		{url.Values{"album": {"A Night at the Opera"}, "kind": {"album"}}, "album", opera},
		{url.Values{"q": {"The Hobbit audiobook"}}, "book", hobbit},
		{url.Values{"q": {"hobbit"}}, "book", hobbit},
		// Help! the album over Help! the song; the name has no "!" when said.
		{url.Values{"q": {"help"}}, "album", help},
		// Someone named who has nothing by that title: the title alone.
		{url.Values{"q": {"Bohemian Rhapsody"}, "artist": {"Freddie"}}, "track", ids["track:Bohemian Rhapsody:"+itoa(opera)]},
		// Movies aren't played by voice.
		{url.Values{"q": {"something"}}, "track", ids["track:Something:"+itoa(abbey)]},
	} {
		m, code := ask(tc.params)
		if code != 200 || m.Kind != tc.kind || m.Item.ID != tc.id || m.Item.Kind != tc.kind {
			t.Errorf("%v: %d %s %d %q, want %s %d", tc.params, code, m.Kind, m.Item.ID, m.Item.Title, tc.kind, tc.id)
		}
	}
	for _, params := range []url.Values{{"q": {"nothing like it"}}, {"q": {"Something Wild"}}, {"q": {"Hobbit"}, "kind": {"album"}}, {"q": {"x"}, "kind": {"movie"}}} {
		if _, code := ask(params); code != 404 {
			t.Errorf("%v: %d, want 404", params, code)
		}
	}
	for _, params := range []url.Values{{}, {"kind": {"album"}}} {
		if _, code := ask(params); code != 400 {
			t.Errorf("%v: %d, want 400", params, code)
		}
	}
}

func TestParseVoice(t *testing.T) {
	for q, want := range map[string]voiceAsk{
		"the album Thriller":     {Words: "Thriller", Kind: "album"},
		"Abbey Road album":       {Words: "Abbey Road", Kind: "album"},
		"The Hobbit audio book":  {Words: "The Hobbit", Kind: "book"},
		"music by Queen":         {Words: "Queen", Kind: "artist"},
		"Album":                  {Words: "Album"},
		"  Bohemian Rhapsody   ": {Words: "Bohemian Rhapsody"},
	} {
		if got := parseVoice(q, "", "", ""); got != want {
			t.Errorf("%q: %+v, want %+v", q, got, want)
		}
	}
	if got := parseVoice("the album Thriller", "track", "", ""); got.Kind != "track" {
		t.Errorf("the phone's kind should win: %+v", got)
	}
}
