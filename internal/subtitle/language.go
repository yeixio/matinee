package subtitle

import "strings"

// languages maps ISO 639-1 codes to their English names and the ISO 639-2
// codes (bibliographic and terminology) that video files are tagged with.
var languages = []struct{ code, name, b, t string }{
	{"ar", "Arabic", "ara", ""}, {"bg", "Bulgarian", "bul", ""}, {"bn", "Bengali", "ben", ""},
	{"ca", "Catalan", "cat", ""}, {"cs", "Czech", "cze", "ces"}, {"da", "Danish", "dan", ""},
	{"de", "German", "ger", "deu"}, {"el", "Greek", "gre", "ell"}, {"en", "English", "eng", ""},
	{"es", "Spanish", "spa", ""}, {"et", "Estonian", "est", ""}, {"eu", "Basque", "baq", "eus"},
	{"fa", "Persian", "per", "fas"}, {"fi", "Finnish", "fin", ""}, {"fil", "Filipino", "fil", "tgl"},
	{"fr", "French", "fre", "fra"}, {"ga", "Irish", "gle", ""}, {"gl", "Galician", "glg", ""},
	{"he", "Hebrew", "heb", ""}, {"hi", "Hindi", "hin", ""}, {"hr", "Croatian", "hrv", ""},
	{"hu", "Hungarian", "hun", ""}, {"id", "Indonesian", "ind", ""}, {"is", "Icelandic", "ice", "isl"},
	{"it", "Italian", "ita", ""}, {"ja", "Japanese", "jpn", ""}, {"ko", "Korean", "kor", ""},
	{"lt", "Lithuanian", "lit", ""}, {"lv", "Latvian", "lav", ""}, {"mk", "Macedonian", "mac", "mkd"},
	{"ms", "Malay", "may", "msa"}, {"nb", "Norwegian Bokmål", "nob", ""}, {"nl", "Dutch", "dut", "nld"},
	{"no", "Norwegian", "nor", ""}, {"pl", "Polish", "pol", ""}, {"pt", "Portuguese", "por", ""},
	{"ro", "Romanian", "rum", "ron"}, {"ru", "Russian", "rus", ""}, {"sk", "Slovak", "slo", "slk"},
	{"sl", "Slovenian", "slv", ""}, {"sr", "Serbian", "srp", ""}, {"sv", "Swedish", "swe", ""},
	{"ta", "Tamil", "tam", ""}, {"te", "Telugu", "tel", ""}, {"th", "Thai", "tha", ""},
	{"tr", "Turkish", "tur", ""}, {"uk", "Ukrainian", "ukr", ""}, {"ur", "Urdu", "urd", ""},
	{"vi", "Vietnamese", "vie", ""}, {"zh", "Chinese", "chi", "zho"},
}

// Region and script variants seen in file names, e.g. "Movie.pt-BR.srt".
var variants = map[string][2]string{
	"pt-br": {"pt", "Portuguese (Brazil)"}, "pob": {"pt", "Portuguese (Brazil)"},
	"es-419": {"es", "Spanish (Latin America)"}, "es-la": {"es", "Spanish (Latin America)"},
	"zh-hans": {"zh", "Chinese (Simplified)"}, "zh-cn": {"zh", "Chinese (Simplified)"}, "chs": {"zh", "Chinese (Simplified)"},
	"zh-hant": {"zh", "Chinese (Traditional)"}, "zh-tw": {"zh", "Chinese (Traditional)"}, "cht": {"zh", "Chinese (Traditional)"},
}

var languageIndex = func() map[string]int {
	m := map[string]int{}
	for i, l := range languages {
		for _, k := range []string{l.code, l.b, l.t, strings.ToLower(l.name)} {
			if k != "" {
				m[k] = i
			}
		}
	}
	return m
}()

// lookupLanguage turns a tag or file name part ("eng", "en", "English",
// "pt-BR") into a short code and a display name. Unknown values give "", "".
func lookupLanguage(s string) (code, name string) {
	k := strings.ToLower(strings.TrimSpace(s))
	if k == "" || k == "und" || k == "unk" || k == "zxx" || k == "mis" {
		return "", ""
	}
	k = strings.ReplaceAll(k, "_", "-")
	if v, ok := variants[k]; ok {
		return v[0], v[1]
	}
	if i, ok := languageIndex[k]; ok {
		return languages[i].code, languages[i].name
	}
	return "", ""
}
