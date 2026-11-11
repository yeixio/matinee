package scan

import "testing"

// File names written with split accents (as a Mac writes them) or garbled
// give the title as it's meant to be, so it matches online.
func TestParseRepairsNames(t *testing.T) {
	for path, want := range map[string]string{
		"/m/Amélie (2001)/Amélie (2001) Bluray-1080p Proper.mkv":                      "Amélie",
		"/m/Ã\u0086on Flux (2005)/Ã\u0086on Flux (2005) WEBDL-1080p.mkv":              "Æon Flux",
		"/m/Café Society (2016)/Café Society (2016) Bluray-1080p.mkv":                 "Café Society",
		"/m/When Björk Met Attenborough (2013)/When Björk Met Attenborough (2013).mkv": "When Björk Met Attenborough",
	} {
		if got := ParseMovie(path).Title; got != want {
			t.Errorf("ParseMovie(%q) = %q, want %q", path, got, want)
		}
	}
	ep := ParseEpisode("/tv", "/tv/Saturday Night Live/Season 50/Saturday Night Live - S50E12 - TimothÃ©e Chalamet.mkv")
	if ep.Title != "Timothée Chalamet" {
		t.Errorf("episode title = %q", ep.Title)
	}
}
