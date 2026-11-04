package scan

import (
	"path/filepath"
	"regexp"
	"strings"
)

// A library can hold several files of one movie: a 4K copy for the TV and
// a 1080p one for phones, or the theatrical cut next to the extended one.
// They show as one title with a version for each file (see
// store.GroupVersions), and a version's edition, read from its file name
// here, tells the cuts apart.

var (
	// "{edition-Director's Cut}", the way many naming tools mark it.
	editionTagRe = regexp.MustCompile(`(?i)\{edition-([^}]+)\}`)
	// Other "{tmdb-603}"-style tags say nothing about the title.
	braceTagRe = regexp.MustCompile(`\{[^}]*\}`)
)

// editionNames are the cuts file names commonly carry, with how they're
// shown. Each pattern matches the words with any separators between them.
var editionNames = []struct {
	re    *regexp.Regexp
	label string
}{
	{editionRe(`director'?s cut`), "Director's Cut"},
	{editionRe(`director'?s edition`), "Director's Edition"},
	{editionRe(`extended (?:edition|cut|version)`), "Extended Edition"},
	{editionRe(`theatrical (?:cut|edition|version)`), "Theatrical"},
	{editionRe(`unrated (?:cut|edition|version)`), "Unrated"},
	{editionRe(`ultimate (?:cut|edition)`), "Ultimate Edition"},
	{editionRe(`special edition`), "Special Edition"},
	{editionRe(`collector'?s edition`), "Collector's Edition"},
	{editionRe(`anniversary edition`), "Anniversary Edition"},
	{editionRe(`imax(?: edition)?`), "IMAX"},
	{editionRe(`final cut`), "Final Cut"},
	{editionRe(`open matte`), "Open Matte"},
	{editionRe(`remastered`), "Remastered"},
	{editionRe(`redux`), "Redux"},
	{editionRe(`uncut`), "Uncut"},
	{editionRe(`extended`), "Extended Edition"},
	{editionRe(`theatrical`), "Theatrical"},
	{editionRe(`unrated`), "Unrated"},
}

// editionRe matches words like "director's cut" as a whole, with spaces,
// dots, underscores or dashes between them.
func editionRe(words string) *regexp.Regexp {
	words = strings.ReplaceAll(words, " ", `[\s._-]+`)
	return regexp.MustCompile(`(?i)(?:^|[\s._\-\[\](){}])(` + words + `)(?:$|[\s._\-\[\](){}])`)
}

// splitEdition takes a file or folder name apart into the rest of the name
// and the edition it names, if any. An edition only counts where it can't
// be part of the title: in a {edition-…} tag, in brackets, after " - ", or
// after the year in a release-style name. So "Uncut Gems (2019)" keeps its
// title.
func splitEdition(name string) (rest, edition string) {
	if m := editionTagRe.FindStringSubmatchIndex(name); m != nil {
		edition = strings.TrimSpace(name[m[2]:m[3]])
		name = name[:m[0]] + " " + name[m[1]:]
	}
	name = braceTagRe.ReplaceAllString(name, " ")
	for _, e := range editionNames {
		for _, loc := range e.re.FindAllStringSubmatchIndex(name, -1) {
			start, end := loc[2], loc[3]
			if !editionPlace(name, start, end) {
				continue
			}
			if edition == "" {
				edition = e.label
			}
			name = name[:start] + strings.Repeat(" ", end-start) + name[end:]
		}
	}
	return strings.TrimSpace(spacesRe.ReplaceAllString(name, " ")), edition
}

// editionPlace says whether name[start:end] stands where an edition goes
// rather than inside the title.
func editionPlace(name string, start, end int) bool {
	before := strings.TrimRight(name[:start], " ._")
	after := strings.TrimLeft(name[end:], " ._")
	switch {
	case before == "":
		return false // it starts the name, so it's the title
	case strings.HasSuffix(before, "(") && strings.HasPrefix(after, ")"),
		strings.HasSuffix(before, "[") && strings.HasPrefix(after, "]"):
		return true
	case strings.HasSuffix(strings.TrimRight(name[:start], " "), " -"):
		return true // "Heat (1995) - Director's Cut", but not "Spider-Man"
	}
	// After a year: "Heat.1995.Directors.Cut.1080p".
	locs := yearRe.FindAllStringIndex(before, -1)
	for _, l := range locs {
		if l[0] > 0 && isSep(before[l[0]-1]) && (l[1] == len(before) || isSep(before[l[1]])) {
			return true
		}
	}
	return false
}

// Edition is the edition a video file's name says it is, such as
// "Director's Cut", or "" when it names none. A file named only
// "movie.mkv" takes its folder's.
func Edition(path string) string {
	if _, e := splitEdition(stem(path)); e != "" {
		return e
	}
	_, e := splitEdition(filepath.Base(filepath.Dir(path)))
	return e
}
