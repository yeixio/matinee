package scan

import "testing"

func TestParseMovie(t *testing.T) {
	cases := []struct {
		path  string
		title string
		year  int
	}{
		{"/m/The Matrix (1999)/The Matrix (1999).mkv", "The Matrix", 1999},
		{"/m/The.Dark.Knight.2008.1080p.BluRay.x264.mkv", "The Dark Knight", 2008},
		{"/m/Heat (1995)/movie.mkv", "Heat", 1995},
		{"/m/2001 A Space Odyssey (1968).mp4", "2001 A Space Odyssey", 1968},
		{"/m/Blade_Runner_2049_2017_2160p_UHD.mkv", "Blade Runner 2049", 2017},
		{"/m/Amelie.avi", "Amelie", 0},
	}
	for _, c := range cases {
		got := ParseMovie(c.path)
		if got.Title != c.title || got.Year != c.year {
			t.Errorf("ParseMovie(%q) = %q (%d), want %q (%d)", c.path, got.Title, got.Year, c.title, c.year)
		}
	}
}

func TestParseEpisode(t *testing.T) {
	cases := []struct {
		path       string
		show       string
		season, ep int
		title      string
	}{
		{"/tv/Breaking Bad/Season 1/Breaking Bad - S01E02 - Cat's in the Bag.mkv", "Breaking Bad", 1, 2, "Cat's in the Bag"},
		{"/tv/The Office (US)/S03/The.Office.US.S03E10.720p.HDTV.mkv", "The Office (US)", 3, 10, ""},
		{"/tv/Firefly (2002)/Firefly 1x05.avi", "Firefly", 1, 5, ""},
		{"/tv/Severance.S02E01.Hello.Ms.Cobel.2160p.WEB-DL.mkv", "Severance", 2, 1, "Hello Ms Cobel"},
	}
	for _, c := range cases {
		got := ParseEpisode("/tv", c.path)
		if !got.OK || got.Show != c.show || got.Season != c.season || got.Episode != c.ep || got.Title != c.title {
			t.Errorf("ParseEpisode(%q) = %+v, want %s S%dE%d %q", c.path, got, c.show, c.season, c.ep, c.title)
		}
	}
	if ParseEpisode("/tv", "/tv/Show/extras/behind the scenes.mkv").OK {
		t.Error("a file without an episode number should not parse")
	}
}

func TestParseTrackPath(t *testing.T) {
	got := ParseTrackPath("/music", "/music/Radiohead/OK Computer (1997)/02 - Paranoid Android.flac")
	want := TrackInfo{Artist: "Radiohead", Album: "OK Computer", Title: "Paranoid Android", Track: 2, Year: 1997}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
	got = ParseTrackPath("/music", "/music/Artist/Album/1-03 Song.mp3")
	if got.Disc != 1 || got.Track != 3 || got.Title != "Song" {
		t.Errorf("disc-track prefix: got %+v", got)
	}
}
