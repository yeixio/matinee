package scan

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/yeixio/matinee/internal/mediaid"
)

var (
	videoExts = map[string]bool{".mkv": true, ".mp4": true, ".m4v": true, ".avi": true, ".mov": true, ".webm": true, ".ts": true, ".m2ts": true, ".wmv": true, ".mpg": true, ".mpeg": true}
	audioExts = map[string]bool{".mp3": true, ".flac": true, ".m4a": true, ".aac": true, ".ogg": true, ".opus": true, ".wav": true, ".alac": true, ".wma": true, ".aiff": true}

	// "Show.Name.S01E02", "Show Name - s1e2", "Show_Name_1x02"
	episodeRe = regexp.MustCompile(`(?i)^(.*?)[\s._-]*(?:s(\d{1,3})[\s._-]*e(\d{1,4})|(\d{1,2})x(\d{1,3}))`)
	// The second episode of a file that holds two or more, right after the
	// first: "E02" in S01E01E02 or S01E01-E02, "02" in S01E01-02, "1x02"
	// in 1x01-1x02. A number must end there, so "S01E01-1080p" isn't one.
	endEpisodeRe = regexp.MustCompile(`(?i)^(?:[\s._]*-?[\s._]*e(\d{1,4})|-\d{1,2}x(\d{1,4})|-(\d{1,4}))(?:[^\dpi]|$)`)
	// "Season 2", "S02", and the word for season in a few languages.
	seasonDir = regexp.MustCompile(`(?i)^(?:season|series|staffel|temporada|saison|stagione|seizoen|sezon|säsong|sæson|sesong|kausi|s)[\s._-]*(\d{1,4})$`)
	// A folder of specials, filed as season 0.
	specialsDir = regexp.MustCompile(`(?i)^specials?$`)
	// An air date, for daily shows: 2024-03-15, 2024.03.15 or 2024 03 15.
	airDateRe = regexp.MustCompile(`(?:^|[\s._\-\[(])((?:19|20)\d{2})([\s._-])(0[1-9]|1[0-2])[\s._-](0[1-9]|[12]\d|3[01])(?:$|[\s._\-\])])`)
	// "Episode 4", "Ep 04", "E04" in a file in a season folder.
	episodeWordRe = regexp.MustCompile(`(?i)(?:^|[\s._\-\[(])(?:episode|ep|e)[\s._-]*(\d{1,4})(?:$|[\s._\-\])])`)
	// "04 - Infection", "04", "4." at the start of a file in a season folder.
	leadingNumberRe = regexp.MustCompile(`^(\d{1,3})(?:$|[\s._\-\])])`)
	// A plausible release year; boundaries are checked in parseMovieName.
	yearRe = regexp.MustCompile(`(?:19|20)\d{2}`)
	// Words that mark where the title ends in release-style names.
	junkRe   = regexp.MustCompile(`(?i)[\s._-](?:2160p|1080p|720p|480p|4k|uhd|bluray|blu-ray|bdrip|brrip|web-?dl|webrip|hdtv|dvdrip|x264|x265|h\.?264|h\.?265|hevc|remux|proper|repack|extended|unrated|imax)(?:[\s._-]|$)`)
	trackRe  = regexp.MustCompile(`^(?:(\d)[-.])?(\d{1,3})[\s._-]+(.+)$`)
	spacesRe = regexp.MustCompile(`\s+`)
)

func isVideo(path string) bool { return videoExts[strings.ToLower(filepath.Ext(path))] }
func isAudio(path string) bool { return audioExts[strings.ToLower(filepath.Ext(path))] }

// cleanTitle turns "The.Dark_Knight " into "The Dark Knight".
func cleanTitle(s string) string {
	s = strings.NewReplacer(".", " ", "_", " ").Replace(s)
	s = strings.TrimSpace(s)
	s = strings.TrimRight(s, " -([")
	s = strings.TrimLeft(s, " -)]")
	return spacesRe.ReplaceAllString(s, " ")
}

func isSep(c byte) bool { return strings.IndexByte(" ._-[]()", c) >= 0 }

func stem(path string) string {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// MovieInfo is what a movie's file or folder name says about it.
type MovieInfo struct {
	Title string
	Year  int // 0 when unknown
}

// ParseMovie reads "Title (Year)" or release-style "Title.Year.1080p.BluRay"
// from the file name, falling back to the folder name when the file is generic.
func ParseMovie(path string) MovieInfo {
	name, _ := Part(path) // "Heat (1995) CD1" is Heat
	info := parseMovieName(name)
	if info.Year == 0 {
		// Files like "movie.mkv" in "Heat (1995)/" take the folder's name.
		if dir := parseMovieName(filepath.Base(filepath.Dir(path))); dir.Year != 0 {
			return dir
		}
	}
	return info
}

func parseMovieName(name string) MovieInfo {
	name, _ = splitEdition(mediaid.Strip(name))
	cut := len(name)
	if loc := junkRe.FindStringIndex(name); loc != nil {
		cut = loc[0]
	}
	head := name[:cut]
	var year int
	// Take the last year so "2001 A Space Odyssey (1968)" keeps its title.
	all := yearRe.FindAllStringIndex(head, -1)
	for i := len(all) - 1; i >= 0; i-- {
		start, end := all[i][0], all[i][1]
		if start == 0 || !isSep(head[start-1]) || (end < len(head) && !isSep(head[end])) {
			continue
		}
		if title := cleanTitle(head[:start]); title != "" {
			year, _ = strconv.Atoi(head[start:end])
			head = title
		}
		break
	}
	return MovieInfo{Title: cleanTitle(head), Year: year}
}

// EpisodeInfo is what an episode's path says about it.
type EpisodeInfo struct {
	Show    string
	Season  int
	Episode int
	// EndEpisode is the last episode of a file that holds several, such
	// as 2 in S01E01E02; 0 when it holds one.
	EndEpisode int
	Title      string // episode title when the name carries one
	// ShowYear is the year in the show's folder name, as in
	// "Doctor Who (2005)", which tells same-named shows apart; 0 when the
	// folder names none.
	ShowYear int
	// ShowIDs are the ids in the show's folder name, as in
	// "Breaking Bad {tvdb-81189}".
	ShowIDs mediaid.IDs
	// AirDate is the day a daily show's episode aired, as 2024-03-15, when
	// that's how it's named. Its season is the year, and its episode the
	// month and day as one number (315), so a year's episodes sort by date.
	AirDate string
	OK      bool
}

// DailySeason reports whether a season number is a daily show's year.
func DailySeason(n int) bool { return n >= 1900 }

// DailyEpisode turns a daily show's season and episode back into its air
// date, as 2024-03-15.
func DailyEpisode(season, episode int) string {
	return fmt.Sprintf("%04d-%02d-%02d", season, episode/100, episode%100)
}

// ParseEpisode understands "Show/Season 1/Show - S01E02 - Title.mkv",
// release-style "Show.Name.S01E02.720p.mkv", daily shows named by date
// ("Show/2024-03-15.mkv"), and, inside a season folder, names without a
// season ("Season 2/04 - Title.mkv", "Season 2/Episode 4.mkv"). The show
// name comes from the library folder layout when it's there, since file
// names are often abbreviated.
func ParseEpisode(libraryRoot, path string) EpisodeInfo {
	name := stem(path)
	var info EpisodeInfo
	var rest string
	if m := episodeRe.FindStringSubmatchIndex(name); m != nil {
		info = EpisodeInfo{OK: true, Show: cleanTitle(mediaid.Strip(name[m[2]:m[3]]))}
		if m[4] >= 0 {
			info.Season, _ = strconv.Atoi(name[m[4]:m[5]])
			info.Episode, _ = strconv.Atoi(name[m[6]:m[7]])
		} else {
			info.Season, _ = strconv.Atoi(name[m[8]:m[9]])
			info.Episode, _ = strconv.Atoi(name[m[10]:m[11]])
		}
		rest = name[m[1]:]
		if e := endEpisodeRe.FindStringSubmatchIndex(rest); e != nil {
			for g := 1; g <= 3; g++ {
				if e[2*g] >= 0 {
					end, _ := strconv.Atoi(rest[e[2*g]:e[2*g+1]])
					// Two parts of one release, not a range: S01E05-E03, or a
					// number that can't be an episode this close.
					if end > info.Episode && end-info.Episode < 10 {
						info.EndEpisode = end
						rest = rest[e[2*g+1]:]
					}
					break
				}
			}
		}
	} else if m := airDateRe.FindStringSubmatchIndex(name); m != nil && dateOK(name[m[2]:m[3]], name[m[6]:m[7]], name[m[8]:m[9]]) {
		y, _ := strconv.Atoi(name[m[2]:m[3]])
		mo, _ := strconv.Atoi(name[m[6]:m[7]])
		d, _ := strconv.Atoi(name[m[8]:m[9]])
		info = EpisodeInfo{OK: true, Show: cleanTitle(mediaid.Strip(name[:m[2]])), Season: y, Episode: mo*100 + d,
			AirDate: fmt.Sprintf("%04d-%02d-%02d", y, mo, d)}
		rest = name[m[9]:]
	} else if season, ok := seasonOf(filepath.Base(filepath.Dir(path))); ok {
		if m := episodeWordRe.FindStringSubmatchIndex(name); m != nil {
			info = EpisodeInfo{OK: true, Season: season}
			info.Episode, _ = strconv.Atoi(name[m[2]:m[3]])
			rest = name[m[3]:]
		} else if m := leadingNumberRe.FindStringSubmatchIndex(name); m != nil {
			info = EpisodeInfo{OK: true, Season: season}
			info.Episode, _ = strconv.Atoi(name[m[2]:m[3]])
			rest = name[m[3]:]
		}
		if info.OK && info.Episode == 0 && season != 0 {
			info = EpisodeInfo{}
		}
	}
	if !info.OK {
		return info
	}
	if loc := junkRe.FindStringIndex(rest); loc != nil {
		rest = rest[:loc[0]]
	}
	info.Title = cleanTitle(mediaid.Strip(rest))
	if show, year, ids, ok := showFolder(libraryRoot, path); ok {
		info.Show, info.ShowYear, info.ShowIDs = show, year, ids
	}
	if info.Show == "" {
		info.Show = "Unknown Show"
	}
	return info
}

// seasonOf reads a season folder's number: "Season 2", "S02", "Staffel 2",
// or 0 for "Specials".
func seasonOf(dir string) (int, bool) {
	if specialsDir.MatchString(dir) {
		return 0, true
	}
	if m := seasonDir.FindStringSubmatch(strings.TrimSpace(dir)); m != nil {
		n, _ := strconv.Atoi(m[1])
		return n, true
	}
	return 0, false
}

// dateOK checks a date's day exists in its month.
func dateOK(y, m, d string) bool {
	_, err := time.Parse("2006-01-02", y+"-"+m+"-"+d)
	return err == nil
}

// showFolder reads the show's folder, the one right under the library
// folder, when the file is in one: its name, its year and its ids.
func showFolder(libraryRoot, path string) (show string, year int, ids mediaid.IDs, ok bool) {
	rel, err := filepath.Rel(libraryRoot, path)
	if err != nil {
		return "", 0, ids, false
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) < 2 || parts[0] == ".." {
		return "", 0, ids, false
	}
	if _, isSeason := seasonOf(parts[0]); isSeason {
		return "", 0, ids, false
	}
	mi := parseMovieName(parts[0])
	return mi.Title, mi.Year, mediaid.Parse(parts[0]), mi.Title != ""
}

// ShowOf names the show a file in a TV library belongs to, from its
// folder, for files that don't name an episode, such as a special.
func ShowOf(libraryRoot, path string) EpisodeInfo {
	info := EpisodeInfo{Show: cleanTitle(filepath.Base(filepath.Dir(path)))}
	if show, year, ids, ok := showFolder(libraryRoot, path); ok {
		info.Show, info.ShowYear, info.ShowIDs = show, year, ids
	}
	return info
}

// TrackInfo is a music file's place in the library, from tags or folders.
type TrackInfo struct {
	Artist string // the album's artist, which the album is filed under
	// TrackArtist is the track's own artist from its tags, when they name one.
	TrackArtist string
	Album       string
	Title       string
	Track       int
	Disc        int
	Year        int
}

// ParseTrackPath reads "Artist/Album/01 - Title.flac" for files without tags.
func ParseTrackPath(libraryRoot, path string) TrackInfo {
	info := TrackInfo{Title: cleanTitle(stem(path))}
	if m := trackRe.FindStringSubmatch(stem(path)); m != nil {
		info.Disc, _ = strconv.Atoi(m[1])
		info.Track, _ = strconv.Atoi(m[2])
		info.Title = cleanTitle(strings.TrimLeft(m[3], " -."))
	}
	if rel, err := filepath.Rel(libraryRoot, path); err == nil {
		parts := strings.Split(filepath.ToSlash(rel), "/")
		// Artist/Album/CD 1/track: the disc folder isn't the album.
		if n := len(parts); n >= 3 {
			if m := discDirRe.FindStringSubmatch(parts[n-2]); m != nil {
				if info.Disc == 0 {
					info.Disc, _ = strconv.Atoi(m[1])
				}
				parts = append(parts[:n-2], parts[n-1])
			}
		}
		if len(parts) >= 3 {
			info.Artist = parts[len(parts)-3]
			info.Album = parts[len(parts)-2]
		} else if len(parts) == 2 {
			info.Album = parts[0]
		}
	}
	if mi := parseMovieName(info.Album); mi.Year != 0 {
		info.Album, info.Year = mi.Title, mi.Year
	}
	return info
}
