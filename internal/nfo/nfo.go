// Package nfo reads the .nfo files many media managers, ripping tools and
// people write next to their movies and shows: small XML files that say
// what a title is (its ids) and describe it.
package nfo

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/yeixio/matinee/internal/mediaid"
)

// Info is what an .nfo file says about a movie, show or episode. Zero
// values are things it doesn't say.
type Info struct {
	Title    string
	Year     int
	Plot     string
	Tagline  string
	Genres   []string
	Rating   float64 // out of 10
	IDs      mediaid.IDs
	Poster   string // a local image the file names, or a web address
	Backdrop string
	// Season and Episode, for an episode's file.
	Season, Episode int
	Aired           string // YYYY-MM-DD
	// Hash identifies the file's contents, to tell when it changed.
	Hash string
}

// Empty reports whether the file said nothing Matinee uses.
func (i Info) Empty() bool {
	return i.Title == "" && i.Year == 0 && i.Plot == "" && i.Tagline == "" && len(i.Genres) == 0 &&
		i.Rating == 0 && !i.IDs.Any() && i.Poster == "" && i.Backdrop == ""
}

type uniqueID struct {
	Type    string `xml:"type,attr"`
	Default string `xml:"default,attr"`
	Value   string `xml:",chardata"`
}

type thumb struct {
	Aspect string `xml:"aspect,attr"`
	Value  string `xml:",chardata"`
}

// doc is the common shape of <movie>, <tvshow> and <episodedetails>.
type doc struct {
	XMLName   xml.Name
	Title     string   `xml:"title"`
	Year      string   `xml:"year"`
	Premiered string   `xml:"premiered"`
	Aired     string   `xml:"aired"`
	Plot      string   `xml:"plot"`
	Outline   string   `xml:"outline"`
	Tagline   string   `xml:"tagline"`
	Genres    []string `xml:"genre"`
	Rating    string   `xml:"rating"`
	Ratings   []struct {
		Name    string `xml:"name,attr"`
		Default string `xml:"default,attr"`
		Max     string `xml:"max,attr"`
		Value   string `xml:"value"`
	} `xml:"ratings>rating"`
	UniqueIDs []uniqueID `xml:"uniqueid"`
	ID        string     `xml:"id"`
	IMDbID    string     `xml:"imdbid"`
	IMDbID2   string     `xml:"imdb_id"`
	TMDbID    string     `xml:"tmdbid"`
	TVDbID    string     `xml:"tvdbid"`
	Thumbs    []thumb    `xml:"thumb"`
	Fanart    []thumb    `xml:"fanart>thumb"`
	Season    string     `xml:"season"`
	Episode   string     `xml:"episode"`
}

// Read reads an .nfo file. Files that are only a link to a web page, as
// some tools write, give their ids. ok is false when there's no such file.
func Read(path string) (info Info, ok bool) {
	b, err := os.ReadFile(path)
	if err != nil || len(b) > 4<<20 {
		return Info{}, false
	}
	sum := sha256.Sum256(b)
	info = Parse(b)
	info.Hash = hex.EncodeToString(sum[:12])
	// Images named relative to the file are next to it.
	for _, p := range []*string{&info.Poster, &info.Backdrop} {
		if *p != "" && !strings.Contains(*p, "://") && !filepath.IsAbs(*p) {
			*p = filepath.Join(filepath.Dir(path), *p)
		}
	}
	return info, true
}

// Parse reads an .nfo file's contents.
func Parse(b []byte) Info {
	var info Info
	var d doc
	dec := xml.NewDecoder(bytes.NewReader(b))
	dec.Strict = false
	dec.CharsetReader = func(_ string, r io.Reader) (io.Reader, error) { return r, nil }
	if err := dec.Decode(&d); err != nil {
		// Not XML, or XML followed by a link: read the ids in any links.
		info.IDs = linkIDs(string(b))
		return info
	}
	info.Title = strings.TrimSpace(d.Title)
	info.Year = year(d.Year)
	if info.Year == 0 {
		info.Year = year(d.Premiered)
	}
	info.Plot = strings.TrimSpace(d.Plot)
	if info.Plot == "" {
		info.Plot = strings.TrimSpace(d.Outline)
	}
	info.Tagline = strings.TrimSpace(d.Tagline)
	for _, g := range d.Genres {
		for _, part := range strings.Split(g, "/") {
			if part = strings.TrimSpace(part); part != "" {
				info.Genres = append(info.Genres, part)
			}
		}
	}
	info.Rating = rating(d)
	info.IDs = ids(d, d.XMLName.Local)
	if more := linkIDs(string(b)); !info.IDs.Any() {
		info.IDs = more
	}
	for _, t := range d.Thumbs {
		v := strings.TrimSpace(t.Value)
		switch {
		case v == "":
		case info.Poster == "" && (t.Aspect == "" || t.Aspect == "poster"):
			info.Poster = v
		case info.Backdrop == "" && (t.Aspect == "fanart" || t.Aspect == "landscape"):
			info.Backdrop = v
		}
	}
	for _, t := range d.Fanart {
		if v := strings.TrimSpace(t.Value); v != "" && info.Backdrop == "" {
			info.Backdrop = v
		}
	}
	info.Season, _ = strconv.Atoi(strings.TrimSpace(d.Season))
	info.Episode, _ = strconv.Atoi(strings.TrimSpace(d.Episode))
	if a := strings.TrimSpace(d.Aired); len(a) >= 10 {
		info.Aired = a[:10]
	}
	return info
}

func year(s string) int {
	s = strings.TrimSpace(s)
	if len(s) >= 4 {
		if n, err := strconv.Atoi(s[:4]); err == nil && n > 1800 && n < 3000 {
			return n
		}
	}
	return 0
}

func rating(d doc) float64 {
	pick := func(v, max string) float64 {
		f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if err != nil || f <= 0 {
			return 0
		}
		if m, err := strconv.ParseFloat(strings.TrimSpace(max), 64); err == nil && m > 0 && m != 10 {
			f = f * 10 / m
		}
		return min(f, 10)
	}
	for _, r := range d.Ratings {
		if r.Default == "true" {
			if f := pick(r.Value, r.Max); f > 0 {
				return f
			}
		}
	}
	for _, r := range d.Ratings {
		if f := pick(r.Value, r.Max); f > 0 {
			return f
		}
	}
	return pick(d.Rating, "")
}

func ids(d doc, root string) mediaid.IDs {
	var out mediaid.IDs
	set := func(kind, v string) {
		v = strings.TrimSpace(v)
		if v == "" {
			return
		}
		switch kind {
		case "tmdb", "themoviedb":
			if n, err := strconv.ParseInt(v, 10, 64); err == nil && out.TMDB == 0 {
				out.TMDB = n
			}
		case "tvdb", "thetvdb":
			if n, err := strconv.ParseInt(v, 10, 64); err == nil && out.TVDB == 0 {
				out.TVDB = n
			}
		case "imdb":
			if strings.HasPrefix(v, "tt") && out.IMDB == "" {
				out.IMDB = v
			}
		}
	}
	for _, u := range d.UniqueIDs {
		set(strings.ToLower(u.Type), u.Value)
	}
	set("tmdb", d.TMDbID)
	set("tvdb", d.TVDbID)
	set("imdb", d.IMDbID)
	set("imdb", d.IMDbID2)
	// <id> is IMDb's for movies, and TheTVDB's for shows, in older files.
	if strings.HasPrefix(strings.TrimSpace(d.ID), "tt") {
		set("imdb", d.ID)
	} else if root == "tvshow" {
		set("tvdb", d.ID)
	}
	return out
}

// linkIDs reads ids from web addresses in a file, such as
// https://www.themoviedb.org/movie/949 or https://www.imdb.com/title/tt0113277/.
func linkIDs(s string) mediaid.IDs {
	var out mediaid.IDs
	if i := strings.Index(s, "themoviedb.org/"); i >= 0 {
		rest := s[i+len("themoviedb.org/"):]
		for _, kind := range []string{"movie/", "tv/"} {
			if strings.HasPrefix(rest, kind) {
				n := 0
				for _, c := range rest[len(kind):] {
					if c < '0' || c > '9' {
						break
					}
					n++
				}
				out.TMDB, _ = strconv.ParseInt(rest[len(kind):len(kind)+n], 10, 64)
			}
		}
	}
	if i := strings.Index(s, "imdb.com/title/tt"); i >= 0 {
		rest := s[i+len("imdb.com/title/"):]
		n := 2
		for n < len(rest) && rest[n] >= '0' && rest[n] <= '9' {
			n++
		}
		if n > 2 {
			out.IMDB = rest[:n]
		}
	}
	return out
}

// ForMovie finds a movie file's .nfo: one named like the file, or
// movie.nfo when the movie has a folder of its own (inFolder).
func ForMovie(path string, inFolder bool) (Info, bool) {
	if info, ok := Read(strings.TrimSuffix(path, filepath.Ext(path)) + ".nfo"); ok {
		return info, true
	}
	if inFolder {
		return Read(filepath.Join(filepath.Dir(path), "movie.nfo"))
	}
	return Info{}, false
}

// ForEpisode finds an episode file's .nfo, named like the file.
func ForEpisode(path string) (Info, bool) {
	return Read(strings.TrimSuffix(path, filepath.Ext(path)) + ".nfo")
}

// ForShow finds a show's tvshow.nfo in its folder.
func ForShow(showDir string) (Info, bool) {
	return Read(filepath.Join(showDir, "tvshow.nfo"))
}
