package nfo

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestParseMovie(t *testing.T) {
	info := Parse([]byte(`<?xml version="1.0" encoding="UTF-8" standalone="yes" ?>
<movie>
  <title>Harbor Lights</title>
  <year>2019</year>
  <plot>A keeper and a storm.</plot>
  <tagline>Hold the light.</tagline>
  <genre>Drama</genre>
  <genre>Mystery / Thriller</genre>
  <ratings><rating name="imdb" max="10" default="true"><value>7.4</value></rating></ratings>
  <uniqueid type="tmdb">12345</uniqueid>
  <uniqueid type="imdb" default="true">tt0111161</uniqueid>
  <thumb aspect="poster">poster-custom.jpg</thumb>
  <fanart><thumb>https://img.example/fanart.jpg</thumb></fanart>
</movie>`))
	if info.Title != "Harbor Lights" || info.Year != 2019 || info.Plot != "A keeper and a storm." || info.Tagline != "Hold the light." {
		t.Errorf("text: %+v", info)
	}
	if !slices.Equal(info.Genres, []string{"Drama", "Mystery", "Thriller"}) || info.Rating != 7.4 {
		t.Errorf("genres and rating: %+v", info)
	}
	if info.IDs.TMDB != 12345 || info.IDs.IMDB != "tt0111161" {
		t.Errorf("ids: %+v", info.IDs)
	}
	if info.Poster != "poster-custom.jpg" || info.Backdrop != "https://img.example/fanart.jpg" {
		t.Errorf("art: %+v", info)
	}
}

func TestParseOlderAndLinks(t *testing.T) {
	show := Parse([]byte(`<tvshow><title>Crumb Street</title><id>81189</id><premiered>2021-04-02</premiered><rating>8.1</rating></tvshow>`))
	if show.IDs.TVDB != 81189 || show.Year != 2021 || show.Rating != 8.1 {
		t.Errorf("show: %+v", show)
	}
	movie := Parse([]byte(`<movie><id>tt0113277</id><ratings><rating name="x" max="5"><value>4</value></rating></ratings></movie>`))
	if movie.IDs.IMDB != "tt0113277" || movie.Rating != 8 {
		t.Errorf("movie: %+v", movie)
	}
	link := Parse([]byte("https://www.themoviedb.org/movie/949-heat\n"))
	if link.IDs.TMDB != 949 {
		t.Errorf("link: %+v", link)
	}
	if got := Parse([]byte("http://www.imdb.com/title/tt0113277/")); got.IDs.IMDB != "tt0113277" {
		t.Errorf("imdb link: %+v", got)
	}
	ep := Parse([]byte(`<episodedetails><title>Dough Rises</title><season>1</season><episode>2</episode><aired>2021-04-09</aired></episodedetails>`))
	if ep.Title != "Dough Rises" || ep.Season != 1 || ep.Episode != 2 || ep.Aired != "2021-04-09" {
		t.Errorf("episode: %+v", ep)
	}
}

func TestFind(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "movie.nfo"), []byte(`<movie><title>A</title><thumb>poster.jpg</thumb></movie>`), 0o644)
	video := filepath.Join(dir, "a.mkv")
	if info, ok := ForMovie(video, true); !ok || info.Title != "A" || info.Poster != filepath.Join(dir, "poster.jpg") || info.Hash == "" {
		t.Errorf("movie.nfo: %+v %v", info, ok)
	}
	if _, ok := ForMovie(video, false); ok {
		t.Error("movie.nfo only counts in a movie's own folder")
	}
	os.WriteFile(filepath.Join(dir, "a.nfo"), []byte(`<movie><title>B</title></movie>`), 0o644)
	if info, _ := ForMovie(video, true); info.Title != "B" {
		t.Errorf("own name first: %+v", info)
	}
}

func TestParsePeople(t *testing.T) {
	info := Parse([]byte(`<movie>
  <director>Ann Lee / Bo Park</director>
  <credits>Cy Diaz</credits>
  <writer>Cy Diaz</writer>
  <actor><name>Second Star</name><role>Mate</role><order>1</order></actor>
  <actor><name>Lead Star</name><role>Keeper</role><order>0</order><thumb>https://img.example/lead.jpg</thumb><tmdbid>42</tmdbid></actor>
  <actor><name>Guesty</name><role>Visitor</role><type>GuestStar</type></actor>
  <actor><name>Lead Star</name><role>Twice</role></actor>
  <actor><name></name></actor>
</movie>`))
	names := func(ps []Person) []string {
		var out []string
		for _, p := range ps {
			out = append(out, p.Name+":"+p.Role)
		}
		return out
	}
	if got := names(info.Cast); !slices.Equal(got, []string{"Lead Star:Keeper", "Second Star:Mate"}) {
		t.Errorf("cast: %v", got)
	}
	if info.Cast[0].TMDBID != 42 || info.Cast[0].Thumb != "https://img.example/lead.jpg" {
		t.Errorf("lead: %+v", info.Cast[0])
	}
	if got := names(info.Guests); !slices.Equal(got, []string{"Guesty:Visitor"}) {
		t.Errorf("guests: %v", got)
	}
	if got := names(info.Crew); !slices.Equal(got, []string{"Ann Lee:Director", "Bo Park:Director", "Cy Diaz:Writer"}) {
		t.Errorf("crew: %v", got)
	}
	if info.Empty() {
		t.Error("a file that only credits people isn't empty")
	}
}

func TestReadFindsActorPhotos(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".actors"), 0o755)
	os.WriteFile(filepath.Join(dir, ".actors", "Lead_Star.jpg"), []byte("jpg"), 0o644)
	os.WriteFile(filepath.Join(dir, "tvshow.nfo"), []byte(`<tvshow><actor><name>Lead Star</name></actor><actor><name>Other</name><thumb>other.png</thumb></actor></tvshow>`), 0o644)
	info, ok := ForShow(dir)
	if !ok || len(info.Cast) != 2 {
		t.Fatalf("read: %+v %v", info, ok)
	}
	if info.Cast[0].Thumb != filepath.Join(dir, ".actors", "Lead_Star.jpg") || info.Cast[1].Thumb != filepath.Join(dir, "other.png") {
		t.Errorf("photos: %+v", info.Cast)
	}
}
