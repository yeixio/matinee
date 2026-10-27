// Package i18n holds Matinee's text in every language (locales/, read by the
// web player and copied into the apps) and the list of languages people can
// pick (languages.json). The server uses it to pick the language a request
// gets titles and descriptions in. See README.md.
package i18n

import (
	_ "embed"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
)

// Source is the language the catalog is written in, and the last fallback.
const Source = "en"

// Language is one of the languages people can pick.
type Language struct {
	Code   string `json:"code"` // BCP 47, such as de, pt-BR or zh-Hant
	Name   string `json:"name"` // in the language itself, such as Deutsch
	Dir    string `json:"dir"`  // ltr or rtl
	Status string `json:"status"`
}

//go:embed languages.json
var languagesJSON []byte

var languages = func() []Language {
	var out []Language
	if err := json.Unmarshal(languagesJSON, &out); err != nil {
		panic("i18n: languages.json: " + err.Error())
	}
	return out
}()

// Languages lists the languages people can pick, English first.
func Languages() []Language { return languages }

// Has reports whether code is one of the languages, exactly as listed.
func Has(code string) bool {
	for _, l := range languages {
		if l.Code == code {
			return true
		}
	}
	return false
}

// Old or other names for listed languages: Norwegian is written in Bokmål,
// and iw and in are Hebrew's and Indonesian's former codes.
var aliases = map[string]string{"no": "nb", "nn": "nb", "iw": "he", "in": "id"}

// Chinese regions and the script each writes, for tags such as zh-TW that
// don't name one.
var chineseScripts = map[string]string{"tw": "Hant", "hk": "Hant", "mo": "Hant", "cn": "Hans", "sg": "Hans", "my": "Hans"}

// scriptOf returns a tag's script, such as Hant for zh-Hant-TW or zh-TW, or
// "" when the tag doesn't say.
func scriptOf(parts []string) string {
	for _, p := range parts[1:] {
		if len(p) == 4 {
			return strings.ToUpper(p[:1]) + strings.ToLower(p[1:])
		}
	}
	if parts[0] != "zh" {
		return ""
	}
	for _, p := range parts[1:] {
		if s, ok := chineseScripts[strings.ToLower(p)]; ok {
			return s
		}
	}
	return "Hans"
}

// Match finds the listed language for a tag, as the web player and the apps
// do: exactly (pt-BR), by base language (de-AT → de), by script (zh-TW →
// zh-Hant), or by another region of the language (pt-PT → pt-BR, never
// Simplified for Traditional Chinese). It returns "" for none.
func Match(tag string) string {
	parts := strings.Split(strings.ReplaceAll(strings.TrimSpace(tag), "_", "-"), "-")
	if parts[0] == "" {
		return ""
	}
	parts[0] = strings.ToLower(parts[0])
	if a, ok := aliases[parts[0]]; ok {
		parts[0] = a
	}
	whole := strings.ToLower(strings.Join(parts, "-"))
	for _, l := range languages {
		if strings.ToLower(l.Code) == whole {
			return l.Code
		}
	}
	for _, l := range languages {
		if l.Code == parts[0] {
			return l.Code
		}
	}
	script := scriptOf(parts)
	for _, l := range languages {
		lp := strings.Split(l.Code, "-")
		if lp[0] == parts[0] && scriptOf(lp) == script {
			return l.Code
		}
	}
	return ""
}

// FromAcceptLanguage picks the listed language an Accept-Language header asks
// for, most preferred first, or "" when it names none of them.
func FromAcceptLanguage(header string) string {
	type pref struct {
		tag string
		q   float64
	}
	var prefs []pref
	for _, part := range strings.Split(header, ",") {
		fields := strings.Split(strings.TrimSpace(part), ";")
		p := pref{tag: strings.TrimSpace(fields[0]), q: 1}
		for _, f := range fields[1:] {
			if v, ok := strings.CutPrefix(strings.TrimSpace(f), "q="); ok {
				p.q, _ = strconv.ParseFloat(v, 64)
			}
		}
		if p.tag != "" && p.tag != "*" && p.q > 0 {
			prefs = append(prefs, p)
		}
	}
	sort.SliceStable(prefs, func(i, j int) bool { return prefs[i].q > prefs[j].q })
	for _, p := range prefs {
		if code := Match(p.tag); code != "" {
			return code
		}
	}
	return ""
}

// TMDB asks for a language with ISO 639-1 and, for some, a country.
var tmdbCodes = map[string]string{"zh-Hans": "zh-CN", "zh-Hant": "zh-TW", "pt-BR": "pt-BR"}

// TMDBLanguage is how TMDB's language parameter names a listed language.
func TMDBLanguage(code string) string {
	if c, ok := tmdbCodes[code]; ok {
		return c
	}
	return strings.Split(code, "-")[0]
}

// FromTMDB finds the listed language of one of TMDB's translations, which
// name a language (ISO 639-1) and a country (ISO 3166-1), and how well the
// country fits it: 2 for the language's own (de-DE, pt-BR, zh-TW), 1 for
// another country, 0 for no listed language.
func FromTMDB(language, country string) (string, int) {
	language, country = strings.ToLower(language), strings.ToUpper(country)
	switch language {
	case "zh":
		switch country {
		case "TW":
			return "zh-Hant", 2
		case "HK", "MO":
			return "zh-Hant", 1
		case "CN":
			return "zh-Hans", 2
		}
		return "zh-Hans", 1
	case "pt":
		if country == "BR" {
			return "pt-BR", 2
		}
		return "pt-BR", 1
	}
	code := Match(language)
	if code == "" || code == Source {
		return "", 0
	}
	if country == homeCountry(code) {
		return code, 2
	}
	return code, 1
}

// homeCountries are the countries whose translations languages prefer on
// TMDB where the country code isn't the language code: Danish is Denmark's.
// The others go by their own code (de-DE, fr-FR).
var homeCountries = map[string]string{
	"ar": "SA", "cs": "CZ", "da": "DK", "el": "GR", "fa": "IR", "he": "IL", "hi": "IN", "ja": "JP",
	"ko": "KR", "ms": "MY", "nb": "NO", "sv": "SE", "uk": "UA", "ur": "PK", "vi": "VN",
}

func homeCountry(code string) string {
	if c, ok := homeCountries[code]; ok {
		return c
	}
	return strings.ToUpper(code)
}
