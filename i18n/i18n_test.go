package i18n

import "testing"

func TestMatch(t *testing.T) {
	for tag, want := range map[string]string{
		"de": "de", "de-AT": "de", "pt-BR": "pt-BR", "pt-PT": "pt-BR", "pt": "pt-BR",
		"zh-TW": "zh-Hant", "zh-HK": "zh-Hant", "zh-Hant-TW": "zh-Hant", "zh-CN": "zh-Hans", "zh": "zh-Hans",
		"no": "nb", "nb-NO": "nb", "iw": "he", "en_GB": "en", "xx": "", "": "",
	} {
		if got := Match(tag); got != want {
			t.Errorf("Match(%q) = %q, want %q", tag, got, want)
		}
	}
}

func TestFromAcceptLanguage(t *testing.T) {
	for header, want := range map[string]string{
		"de-AT,de;q=0.9,en;q=0.8": "de",
		"xx,fr;q=0.5":             "fr",
		"en;q=0.2,ja":             "ja",
		"*":                       "",
		"":                        "",
	} {
		if got := FromAcceptLanguage(header); got != want {
			t.Errorf("FromAcceptLanguage(%q) = %q, want %q", header, got, want)
		}
	}
}

func TestFromTMDB(t *testing.T) {
	type in struct{ lang, country string }
	for tr, want := range map[in]struct {
		code  string
		score int
	}{
		{"de", "DE"}: {"de", 2}, {"de", "AT"}: {"de", 1}, {"es", "MX"}: {"es", 1},
		{"zh", "TW"}: {"zh-Hant", 2}, {"zh", "CN"}: {"zh-Hans", 2}, {"pt", "PT"}: {"pt-BR", 1},
		{"da", "DK"}: {"da", 2}, {"en", "US"}: {"", 0}, {"xx", "XX"}: {"", 0},
	} {
		if code, score := FromTMDB(tr.lang, tr.country); code != want.code || score != want.score {
			t.Errorf("FromTMDB(%v) = %q %d, want %v", tr, code, score, want)
		}
	}
}

func TestLanguagesListed(t *testing.T) {
	seen := map[string]bool{}
	for i, l := range Languages() {
		if i == 0 && l.Code != Source {
			t.Errorf("first language %q, want %q", l.Code, Source)
		}
		if seen[l.Code] || l.Name == "" || (l.Dir != "ltr" && l.Dir != "rtl") || Match(l.Code) != l.Code {
			t.Errorf("bad language %+v", l)
		}
		seen[l.Code] = true
	}
}
