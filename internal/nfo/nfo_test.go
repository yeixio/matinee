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
