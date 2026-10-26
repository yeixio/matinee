package scan

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Trailers sit next to the media they belong to, named the way most media
// tools expect: "Heat (1995)-trailer.mkv", "trailer.mp4", or anything in a
// Trailers folder. Scans leave them out of libraries; they're found again
// when someone opens the title.

// trailerRe matches file stems that mark a trailer: "-trailer", ".trailer"
// or "_trailer" at the end, optionally numbered ("-trailer2").
var trailerRe = regexp.MustCompile(`(?i)(?:^|[-._])trailer\d*$`)

// IsTrailerFile reports whether a file name marks a trailer.
func IsTrailerFile(name string) bool {
	return isVideo(name) && trailerRe.MatchString(stem(name))
}

// IsTrailerDir reports whether a folder holds trailers.
func IsTrailerDir(name string) bool { return strings.EqualFold(name, "trailers") }

// MovieTrailers lists the trailers for a movie file. A movie in its own
// folder takes every trailer there; one sharing the library folder with
// others only takes those named after it.
func MovieTrailers(root, path string) []string {
	dir := filepath.Dir(path)
	if filepath.Clean(dir) != filepath.Clean(root) {
		return FolderTrailers(dir)
	}
	prefix := strings.ToLower(stem(path))
	var out []string
	for _, p := range trailerFiles(dir) {
		s := strings.ToLower(stem(p))
		if strings.HasPrefix(s, prefix) && trailerRe.MatchString(s[len(prefix):]) {
			out = append(out, p)
		}
	}
	return out
}

// FolderTrailers lists the trailers in a show's or season's folder.
func FolderTrailers(dir string) []string {
	out := trailerFiles(dir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	for _, e := range entries {
		if !e.IsDir() || !IsTrailerDir(e.Name()) {
			continue
		}
		sub := filepath.Join(dir, e.Name())
		files, _ := os.ReadDir(sub)
		for _, f := range files {
			if !f.IsDir() && !strings.HasPrefix(f.Name(), ".") && isVideo(f.Name()) {
				out = append(out, filepath.Join(sub, f.Name()))
			}
		}
	}
	sort.Strings(out)
	return out
}

func trailerFiles(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() && !strings.HasPrefix(e.Name(), ".") && IsTrailerFile(e.Name()) {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	return out
}

// ShowFolders returns a show's folder and, when episodes are filed by
// season, the season's folder, given one of the episodes under it.
func ShowFolders(root, episodePath string) (show, season string) {
	rel, err := filepath.Rel(root, episodePath)
	if err != nil {
		return "", ""
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) < 2 || parts[0] == ".." {
		return "", ""
	}
	show = filepath.Join(root, parts[0])
	if len(parts) >= 3 {
		season = filepath.Dir(episodePath)
	}
	return show, season
}

// TrailerName is what a trailer file is called on screen.
func TrailerName(path string) string {
	s := stem(path)
	if IsTrailerDir(filepath.Base(filepath.Dir(path))) {
		if name := cleanTitle(s); name != "" {
			return name
		}
	}
	return "Trailer"
}
