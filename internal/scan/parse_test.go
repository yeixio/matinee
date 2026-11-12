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
		{"/m/Heat (1995) {tmdb-949}/Heat (1995) {tmdb-949}.mkv", "Heat", 1995},
		{"/m/Heat [imdbid-tt0113277].mkv", "Heat", 0},
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
	if got := ParseEpisode("/tv", "/tv/Doctor Who (2005) {tvdb-78804}/Season 1/Doctor.Who.S01E01.mkv"); got.Show != "Doctor Who" || got.ShowYear != 2005 || got.ShowIDs.TVDB != 78804 {
		t.Errorf("show folder year and id: %+v", got)
	}
	if got := ParseEpisode("/tv", "/tv/Breaking Bad [tmdbid-1396]/Season 1/x.S01E01.mkv"); got.Show != "Breaking Bad" || got.ShowIDs.TMDB != 1396 {
		t.Errorf("show folder tmdb id: %+v", got)
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

// Artist/Album/CD 1/track files belong to the album, on disc 1.
func TestParseTrackPathDiscFolder(t *testing.T) {
	got := ParseTrackPath("/music", "/music/Pink Floyd/The Wall/CD 2/03 Hey You.flac")
	want := TrackInfo{Artist: "Pink Floyd", Album: "The Wall", Title: "Hey You", Track: 3, Disc: 2}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

// A compilation without an album artist files under Various Artists
// rather than splitting into one album per track artist.
func TestTrackFromTagsCompilation(t *testing.T) {
	path := TrackInfo{Artist: "Mixes", Album: "Summer"}
	if got := trackFromTags(map[string]string{"artist": "Someone", "album": "Summer Hits", "compilation": "1"}, path); got.Artist != "Various Artists" {
		t.Errorf("compilation: %+v", got)
	}
	if got := trackFromTags(map[string]string{"artist": "Someone", "album_artist": "DJ Mix", "compilation": "1"}, path); got.Artist != "DJ Mix" {
		t.Errorf("album artist wins: %+v", got)
	}
	if got := trackFromTags(map[string]string{"artist": "Someone"}, path); got.Artist != "Someone" || got.TrackArtist != "Someone" {
		t.Errorf("track artist: %+v", got)
	}
	if got := trackFromTags(map[string]string{"artist": "Someone", "album artist": "DJ Mix"}, path); got.Artist != "DJ Mix" || got.TrackArtist != "Someone" {
		t.Errorf("album artist spelled with a space: %+v", got)
	}
	if got := trackFromTags(map[string]string{"artist": "Someone", "itunescompilation": "1"}, path); got.Artist != "Various Artists" {
		t.Errorf("iTunes compilation flag: %+v", got)
	}
	if got := trackFromTags(map[string]string{}, path); got.Artist != "Mixes" || got.TrackArtist != "" {
		t.Errorf("no tags: %+v", got)
	}
}

func TestEdition(t *testing.T) {
	cases := []struct {
		path, title, edition string
		year                 int
	}{
		{"/m/Blade Runner (1982)/Blade Runner (1982) {edition-The Final Cut}.mkv", "Blade Runner", "The Final Cut", 1982},
		{"/m/Heat (1995)/Heat (1995) - Director's Cut.mkv", "Heat", "Director's Cut", 1995},
		{"/m/Aliens (1986) [Special Edition].mkv", "Aliens", "Special Edition", 1986},
		{"/m/Apocalypse.Now.1979.Redux.1080p.BluRay.mkv", "Apocalypse Now", "Redux", 1979},
		{"/m/The.Lord.of.the.Rings.2001.EXTENDED.2160p.mkv", "The Lord of the Rings", "Extended Edition", 2001},
		{"/m/Amadeus - Director's Cut.mkv", "Amadeus", "Director's Cut", 0},
		{"/m/Uncut Gems (2019).mkv", "Uncut Gems", "", 2019},
		{"/m/Spider-Man (2002).mkv", "Spider-Man", "", 2002},
		{"/m/Heat (1995) {tmdb-949}.mkv", "Heat", "", 1995},
		{"/m/Dune (2021) {edition-IMAX}/movie.mkv", "Dune", "IMAX", 2021},
	}
	for _, c := range cases {
		got := ParseMovie(c.path)
		if got.Title != c.title || got.Year != c.year {
			t.Errorf("ParseMovie(%q) = %q (%d), want %q (%d)", c.path, got.Title, got.Year, c.title, c.year)
		}
		if e := Edition(c.path); e != c.edition {
			t.Errorf("Edition(%q) = %q, want %q", c.path, e, c.edition)
		}
	}
}

func TestParseMultiEpisode(t *testing.T) {
	cases := []struct {
		name    string
		ep, end int
		title   string
	}{
		{"Show - S01E01E02 - Pilot.mkv", 1, 2, "Pilot"},
		{"Show.S02E05-E06.720p.mkv", 5, 6, ""},
		{"Show - S01E01-02 - Pilot.mkv", 1, 2, "Pilot"},
		{"Show - S01E01-E03.mkv", 1, 3, ""},
		{"Show 1x01-1x02.avi", 1, 2, ""},
		{"Show 1x01-02.avi", 1, 2, ""},
		{"Show.S01E01.1080p.mkv", 1, 0, ""},
		{"Show.S01E01-1080p.mkv", 1, 0, ""},
		{"Show - S01E05 - Part 2.mkv", 5, 0, "Part 2"},
	}
	for _, c := range cases {
		got := ParseEpisode("/tv", "/tv/Show/Season 1/"+c.name)
		if !got.OK || got.Episode != c.ep || got.EndEpisode != c.end || got.Title != c.title {
			t.Errorf("ParseEpisode(%q) = %+v, want E%d-E%d %q", c.name, got, c.ep, c.end, c.title)
		}
	}
}

func TestParseEpisodeWithoutCode(t *testing.T) {
	cases := []struct {
		path       string
		show       string
		season, ep int
		title      string
		date       string
	}{
		{"/tv/Crumb Street/Season 2/04 - Infection.mkv", "Crumb Street", 2, 4, "Infection", ""},
		{"/tv/Crumb Street/Season 2/04.mkv", "Crumb Street", 2, 4, "", ""},
		{"/tv/Crumb Street/Season 2/Episode 4.mkv", "Crumb Street", 2, 4, "", ""},
		{"/tv/Crumb Street/Season 2/Crumb Street Ep 04 - Infection.mkv", "Crumb Street", 2, 4, "Infection", ""},
		{"/tv/Crumb Street/S02/E04.720p.mkv", "Crumb Street", 2, 4, "", ""},
		{"/tv/Crumb Street/Staffel 2/04 - Infektion.mkv", "Crumb Street", 2, 4, "Infektion", ""},
		{"/tv/Crumb Street/Temporada 2/04.mkv", "Crumb Street", 2, 4, "", ""},
		{"/tv/Crumb Street/Saison 2/04.mkv", "Crumb Street", 2, 4, "", ""},
		{"/tv/Crumb Street/Specials/01 - Christmas.mkv", "Crumb Street", 0, 1, "Christmas", ""},
		// Daily shows, by air date.
		{"/tv/The Nightly Desk/2024-03-15.mkv", "The Nightly Desk", 2024, 315, "", "2024-03-15"},
		{"/tv/The.Nightly.Desk.2024.03.15.Guest.Name.720p.mkv", "The Nightly Desk", 2024, 315, "Guest Name", "2024-03-15"},
		{"/tv/The Nightly Desk/Season 2024/The Nightly Desk 2024 03 15 - Guest.mkv", "The Nightly Desk", 2024, 315, "Guest", "2024-03-15"},
		// A show's year is not a date, and codes still win.
		{"/tv/Show (2019)/Season 1/Show - S01E01.mkv", "Show", 1, 1, "", ""},
	}
	for _, c := range cases {
		got := ParseEpisode("/tv", c.path)
		if !got.OK || got.Show != c.show || got.Season != c.season || got.Episode != c.ep || got.Title != c.title || got.AirDate != c.date {
			t.Errorf("ParseEpisode(%q) = %+v, want %s S%dE%d %q %q", c.path, got, c.show, c.season, c.ep, c.title, c.date)
		}
	}
	for _, p := range []string{
		"/tv/Crumb Street/Behind the scenes.mkv",
		"/tv/Crumb Street/Season 2/Making of.mkv",
		"/tv/Crumb Street/2024-02-30.mkv", // no such day
	} {
		if got := ParseEpisode("/tv", p); got.OK {
			t.Errorf("ParseEpisode(%q) = %+v, want no episode", p, got)
		}
	}
	if got := ShowOf("/tv", "/tv/Crumb Street (2021)/Season 1/Making of.mkv"); got.Show != "Crumb Street" || got.ShowYear != 2021 {
		t.Errorf("ShowOf: %+v", got)
	}
	if DailyEpisode(2024, 315) != "2024-03-15" {
		t.Error("DailyEpisode")
	}
}

func TestParseAbsoluteEpisode(t *testing.T) {
	cases := []struct {
		path     string
		show     string
		absolute int
		compact  bool
		title    string
	}{
		{"/tv/Frieren/[SubsPlease] Sousou no Frieren - 01 (1080p) [A1B2C3D4].mkv", "Frieren", 1, false, ""},
		{"/tv/One Piece/One Piece - 0123 [1080p].mkv", "One Piece", 123, false, ""},
		{"/tv/One Piece/One Piece - 1071v2 - Gear 5.mkv", "One Piece", 1071, false, "Gear 5"},
		{"/tv/Crumb Street/Crumb Street - 104.mkv", "Crumb Street", 104, true, ""},
		{"/tv/[Grp] Naruto - 104 [720p].mkv", "Naruto", 104, false, ""},
		{"/tv/Bleach/Bleach Episode 300.mkv", "Bleach", 300, false, ""},
	}
	for _, c := range cases {
		got := ParseEpisode("/tv", c.path)
		if !got.OK || got.Show != c.show || got.Absolute != c.absolute || got.Season != 1 || got.Episode != c.absolute || got.Compact != c.compact || got.Title != c.title {
			t.Errorf("ParseEpisode(%q) = %+v, want %s #%d compact=%v %q", c.path, got, c.show, c.absolute, c.compact, c.title)
		}
	}
	for _, p := range []string{"/tv/Show/Show - 2019.mkv", "/tv/Show/Show - 1080p.mkv", "/tv/Show/Show - Special.mkv"} {
		if got := ParseEpisode("/tv", p); got.OK {
			t.Errorf("ParseEpisode(%q) = %+v, want no episode", p, got)
		}
	}
	// In a season folder, the number is the season's episode.
	if got := ParseEpisode("/tv", "/tv/Crumb Street/Season 2/Crumb Street - 05.mkv"); !got.OK || got.Season != 2 || got.Episode != 5 || got.Absolute != 0 {
		t.Errorf("season folder: %+v", got)
	}
}
