package scan

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var (
	videoExts = map[string]bool{".mkv": true, ".mp4": true, ".m4v": true, ".avi": true, ".mov": true, ".webm": true, ".ts": true, ".m2ts": true, ".wmv": true, ".mpg": true, ".mpeg": true}
	audioExts = map[string]bool{".mp3": true, ".flac": true, ".m4a": true, ".aac": true, ".ogg": true, ".opus": true, ".wav": true, ".alac": true, ".wma": true, ".aiff": true}

	// "Show.Name.S01E02", "Show Name - s1e2", "Show_Name_1x02"
	episodeRe = regexp.MustCompile(`(?i)^(.*?)[\s._-]*(?:s(\d{1,3})[\s._-]*e(\d{1,4})|(\d{1,2})x(\d{1,3}))`)
	seasonDir = regexp.MustCompile(`(?i)^(?:season|series|s)[\s._-]*(\d{1,3})$`)
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
	name := stem(path)
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
	Title   string // episode title when the name carries one
	OK      bool
}

// ParseEpisode understands "Show/Season 1/Show - S01E02 - Title.mkv" and
// release-style "Show.Name.S01E02.720p.mkv". The show name comes from the
// library folder layout when it's there, since file names are often abbreviated.
func ParseEpisode(libraryRoot, path string) EpisodeInfo {
	m := episodeRe.FindStringSubmatch(stem(path))
	if m == nil {
		return EpisodeInfo{}
	}
	info := EpisodeInfo{OK: true, Show: cleanTitle(m[1])}
	if m[2] != "" {
		info.Season, _ = strconv.Atoi(m[2])
		info.Episode, _ = strconv.Atoi(m[3])
	} else {
		info.Season, _ = strconv.Atoi(m[4])
		info.Episode, _ = strconv.Atoi(m[5])
	}
	rest := stem(path)[len(m[0]):]
	if loc := junkRe.FindStringIndex(rest); loc != nil {
		rest = rest[:loc[0]]
	}
	info.Title = cleanTitle(rest)

	// Prefer the top folder under the library root as the show name.
	if rel, err := filepath.Rel(libraryRoot, path); err == nil {
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if len(parts) >= 2 && !seasonDir.MatchString(parts[0]) {
			info.Show = cleanTitle(stripYear(parts[0]))
		}
	}
	if info.Show == "" {
		info.Show = "Unknown Show"
	}
	return info
}

func stripYear(s string) string {
	if mi := parseMovieName(s); mi.Year != 0 {
		return mi.Title
	}
	return s
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
