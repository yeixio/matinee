package mediaid

import "testing"

func TestParse(t *testing.T) {
	cases := []struct {
		name string
		want IDs
	}{
		{"Heat (1995) {tmdb-949}", IDs{TMDB: 949}},
		{"Heat (1995) [tmdbid-949]", IDs{TMDB: 949}},
		{"Heat (1995) [tmdbid=949]", IDs{TMDB: 949}},
		{"The Shawshank Redemption (1994) {imdb-tt0111161}", IDs{IMDB: "tt0111161"}},
		{"Breaking Bad (2008) [tvdbid-81189]", IDs{TVDB: 81189}},
		{"Show {tvdb-81189} {imdb-tt0903747}", IDs{TVDB: 81189, IMDB: "tt0903747"}},
		{"Heat (1995)", IDs{}},
		{"Heat {edition-Director's Cut}", IDs{}},
	}
	for _, c := range cases {
		if got := Parse(c.name); got != c.want {
			t.Errorf("Parse(%q) = %+v, want %+v", c.name, got, c.want)
		}
	}
	if got := Movie("/m/Heat (1995) {tmdb-949}/Heat (1995).mkv"); got.TMDB != 949 {
		t.Errorf("folder id: %+v", got)
	}
	if got := Strip("Heat (1995) [tmdbid-949].mkv"); got != "Heat (1995)  .mkv" {
		t.Errorf("Strip = %q", got)
	}
}
