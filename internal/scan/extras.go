package scan

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Extras are the special features that sit next to the media they belong
// to: trailers, behind the scenes, deleted scenes, featurettes, interviews,
// scenes, shorts and anything else. They're named the way most media tools
// expect, either with a suffix ("Heat (1995)-featurette.mkv", "trailer.mp4")
// or by being in a folder for their kind ("Deleted Scenes/Bank job.mkv").
// Scans leave them out of libraries; they're found again when someone opens
// the title.

// The kinds of extra, as the API names them.
const (
	ExtraTrailer         = "trailer"
	ExtraBehindTheScenes = "behindTheScenes"
	ExtraDeleted         = "deleted"
	ExtraFeaturette      = "featurette"
	ExtraInterview       = "interview"
	ExtraScene           = "scene"
	ExtraShort           = "short"
	ExtraOther           = "other"
)

// ExtraKinds is the order extras are listed in.
var ExtraKinds = []string{ExtraTrailer, ExtraBehindTheScenes, ExtraDeleted, ExtraFeaturette, ExtraInterview, ExtraScene, ExtraShort, ExtraOther}

// Extra is one extra's file and kind.
type Extra struct {
	Path string
	Kind string
}

// extraDirs are the folder names that hold extras, written lower case with
// single spaces.
var extraDirs = map[string]string{
	"trailers":          ExtraTrailer,
	"behind the scenes": ExtraBehindTheScenes,
	"behindthescenes":   ExtraBehindTheScenes,
	"making of":         ExtraBehindTheScenes,
	"deleted scenes":    ExtraDeleted,
	"deleted":           ExtraDeleted,
	"featurettes":       ExtraFeaturette,
	"interviews":        ExtraInterview,
	"scenes":            ExtraScene,
	"shorts":            ExtraShort,
	"other":             ExtraOther,
	"extras":            ExtraOther,
	"special features":  ExtraOther,
	"bonus features":    ExtraOther,
}

// trailerRe matches file stems that mark a trailer: "-trailer", ".trailer"
// or "_trailer" at the end, optionally numbered ("-trailer2"), or a file
// called just "trailer".
var trailerRe = regexp.MustCompile(`(?i)(?:^|[-._])trailer\d*$`)

// extraRe matches the other kinds' suffixes. These take a hyphen only, so
// titles such as "The.Big.Short.mkv" stay movies.
var extraRe = regexp.MustCompile(`(?i)-(behindthescenes|deleted|deletedscene|featurette|interview|scene|short|other|extra)\d*$`)

var extraSuffixes = map[string]string{
	"behindthescenes": ExtraBehindTheScenes,
	"deleted":         ExtraDeleted,
	"deletedscene":    ExtraDeleted,
	"featurette":      ExtraFeaturette,
	"interview":       ExtraInterview,
	"scene":           ExtraScene,
	"short":           ExtraShort,
	"other":           ExtraOther,
	"extra":           ExtraOther,
}

// ExtraFileKind says which kind of extra a file's name marks it as, if any.
func ExtraFileKind(name string) (string, bool) {
	if !isVideo(name) {
		return "", false
	}
	s := stem(name)
	if trailerRe.MatchString(s) {
		return ExtraTrailer, true
	}
	if m := extraRe.FindStringSubmatch(s); m != nil {
		return extraSuffixes[strings.ToLower(m[1])], true
	}
	return "", false
}

// IsExtraFile reports whether a file name marks an extra.
func IsExtraFile(name string) bool {
	_, ok := ExtraFileKind(name)
	return ok
}

// IsTrailerFile reports whether a file name marks a trailer.
func IsTrailerFile(name string) bool {
	k, ok := ExtraFileKind(name)
	return ok && k == ExtraTrailer
}

// ExtraDirKind says which kind of extra a folder holds, if any.
func ExtraDirKind(name string) (string, bool) {
	n := strings.ToLower(spacesRe.ReplaceAllString(strings.NewReplacer(".", " ", "_", " ", "-", " ").Replace(strings.TrimSpace(name)), " "))
	k, ok := extraDirs[n]
	return k, ok
}

// IsExtraDir reports whether a folder holds extras. Only a Trailers folder
// counts right in a library's folder: there, "Shorts" or "Other" is more
// likely a group of titles than a title's extras. nested is whether the
// folder is inside a title's folder.
func IsExtraDir(name string, nested bool) bool {
	k, ok := ExtraDirKind(name)
	return ok && (nested || k == ExtraTrailer)
}

// MovieExtras lists a movie's extras. A movie in its own folder takes every
// extra there; one sharing the library folder with others only takes those
// named after it.
func MovieExtras(root, path string) []Extra {
	dir := filepath.Dir(path)
	if filepath.Clean(dir) != filepath.Clean(root) {
		return FolderExtras(dir)
	}
	prefix := strings.ToLower(stem(path))
	var out []Extra
	for _, e := range suffixExtras(dir) {
		s := strings.ToLower(stem(e.Path))
		if !strings.HasPrefix(s, prefix) {
			continue
		}
		rest := s[len(prefix):]
		if trailerRe.MatchString(rest) || extraRe.MatchString(rest) {
			out = append(out, e)
		}
	}
	return out
}

// FolderExtras lists the extras in a movie's, show's or season's folder:
// files named as extras, and the videos in folders for extras, two levels
// deep ("Extras/Deleted Scenes/Bank job.mkv"). An inner folder's kind wins.
func FolderExtras(dir string) []Extra {
	out := suffixExtras(dir)
	var walk func(dir, kind string, depth int)
	walk = func(dir, kind string, depth int) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range entries {
			name := e.Name()
			if strings.HasPrefix(name, ".") {
				continue
			}
			p := filepath.Join(dir, name)
			if e.IsDir() {
				if depth < 2 {
					k, ok := ExtraDirKind(name)
					if !ok {
						k = kind
					}
					walk(p, k, depth+1)
				}
				continue
			}
			if isVideo(name) {
				out = append(out, Extra{p, kind})
			}
		}
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if k, ok := ExtraDirKind(e.Name()); ok {
			walk(filepath.Join(dir, e.Name()), k, 1)
		}
	}
	SortExtras(out)
	return out
}

// SortExtras puts extras in ExtraKinds order, then by file.
func SortExtras(extras []Extra) {
	rank := map[string]int{}
	for i, k := range ExtraKinds {
		rank[k] = i
	}
	sort.SliceStable(extras, func(i, j int) bool {
		a, b := extras[i], extras[j]
		if rank[a.Kind] != rank[b.Kind] {
			return rank[a.Kind] < rank[b.Kind]
		}
		return a.Path < b.Path
	})
}

func suffixExtras(dir string) []Extra {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []Extra
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if k, ok := ExtraFileKind(e.Name()); ok {
			out = append(out, Extra{filepath.Join(dir, e.Name()), k})
		}
	}
	return out
}

// Trailers keeps the extras that are trailers.
func Trailers(extras []Extra) []string {
	var out []string
	for _, e := range extras {
		if e.Kind == ExtraTrailer {
			out = append(out, e.Path)
		}
	}
	return out
}

// MovieTrailers lists the trailers for a movie file (see MovieExtras).
func MovieTrailers(root, path string) []string { return Trailers(MovieExtras(root, path)) }

// FolderTrailers lists the trailers in a show's or season's folder.
func FolderTrailers(dir string) []string { return Trailers(FolderExtras(dir)) }

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
	if _, ok := ExtraDirKind(filepath.Base(filepath.Dir(path))); ok {
		if name := cleanTitle(stem(path)); name != "" {
			return name
		}
	}
	return "Trailer"
}

// ExtraName is what an extra is called on screen: its file name without the
// kind's suffix, unless that leaves only one of the title's names. Empty
// means it has no name of its own, so players call it by its kind.
func ExtraName(path string, titles ...string) string {
	s := stem(path)
	if loc := extraRe.FindStringIndex(s); loc != nil {
		s = s[:loc[0]]
	} else if loc := trailerRe.FindStringIndex(s); loc != nil {
		s = s[:loc[0]]
	}
	name := cleanTitle(s)
	for _, t := range titles {
		if t := cleanTitle(t); t != "" && strings.EqualFold(name, t) {
			return ""
		}
	}
	return name
}
