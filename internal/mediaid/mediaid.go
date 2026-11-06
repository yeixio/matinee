// Package mediaid reads the database ids that naming tools put in folder
// and file names, such as "Heat (1995) {tmdb-949}" or
// "Breaking Bad [tvdbid-81189]", so a title can be matched by its id
// instead of by a search.
package mediaid

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// IDs are the ids a name carries; zero values are ids it doesn't carry.
type IDs struct {
	TMDB int64
	IMDB string // with its "tt"
	TVDB int64
}

// Any reports whether there's an id at all.
func (i IDs) Any() bool { return i.TMDB != 0 || i.IMDB != "" || i.TVDB != 0 }

// tagRe finds "{tmdb-949}", "[tmdbid-949]", "{imdb-tt0113277}",
// "[imdbid=tt0113277]" and "{tvdb-81189}", in either kind of bracket.
var tagRe = regexp.MustCompile(`(?i)[\[{]\s*(tmdb|imdb|tvdb)(?:id)?\s*[-=:]\s*([a-z]*\d+)\s*[\]}]`)

// Parse reads the ids in one name. When a name carries the same kind of
// id twice, the first wins.
func Parse(name string) IDs {
	var ids IDs
	for _, m := range tagRe.FindAllStringSubmatch(name, -1) {
		v := strings.ToLower(m[2])
		switch strings.ToLower(m[1]) {
		case "tmdb":
			if n, err := strconv.ParseInt(v, 10, 64); err == nil && ids.TMDB == 0 {
				ids.TMDB = n
			}
		case "tvdb":
			if n, err := strconv.ParseInt(v, 10, 64); err == nil && ids.TVDB == 0 {
				ids.TVDB = n
			}
		case "imdb":
			if !strings.HasPrefix(v, "tt") {
				v = "tt" + strings.TrimLeft(v, "abcdefghijklmnopqrstuvwxyz")
			}
			if ids.IMDB == "" {
				ids.IMDB = v
			}
		}
	}
	return ids
}

// Strip takes the id tags out of a name, leaving the rest as it was.
func Strip(name string) string { return tagRe.ReplaceAllString(name, " ") }

// Movie reads a movie's ids from its file name, then its folder's.
func Movie(path string) IDs {
	if ids := Parse(filepath.Base(path)); ids.Any() {
		return ids
	}
	return Parse(filepath.Base(filepath.Dir(path)))
}
