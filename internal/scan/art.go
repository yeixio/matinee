package scan

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/yeixio/matinee/internal/store"
)

// Album and book covers come in many names: cover.jpg, Folder.jpg (which
// some players write), front.png, AlbumArt_{…}_Large.jpg, or a lone scan
// with the album's own name. Names are matched without regard to case,
// since network shares and copies from other systems keep whatever case
// the file was given.

var imageExts = []string{".jpg", ".jpeg", ".png", ".webp"}

func isImage(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	for _, e := range imageExts {
		if ext == e {
			return true
		}
	}
	return false
}

// coverNames are the usual cover file names, best first, without extension.
var coverNames = []string{"cover", "folder", "front", "album", "albumartlarge", "albumart", "thumb", "poster"}

// findCover returns the cover image in dir: one named after a stem in
// prefer (such as the book file's own name), then a usual cover name,
// then any image with "cover" or "front" in its name, then the folder's
// only image. Back covers, discs and booklet pages aren't covers.
func findCover(dir string, prefer ...string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	byStem := map[string]string{}
	var images []string
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") || !isImage(e.Name()) {
			continue
		}
		images = append(images, e.Name())
		s := strings.ToLower(stem(e.Name()))
		if _, ok := byStem[s]; !ok {
			byStem[s] = e.Name()
		}
	}
	if len(images) == 0 {
		return ""
	}
	for _, names := range [][]string{prefer, coverNames} {
		for _, n := range names {
			if name, ok := byStem[strings.ToLower(n)]; ok {
				return filepath.Join(dir, name)
			}
		}
	}
	best, bestScore := "", 0
	var others []string
	for _, name := range images {
		words := strings.FieldsFunc(strings.ToLower(stem(name)), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
		score := 0
		for _, w := range words {
			switch {
			case w == "back" || w == "disc" || w == "disk" || w == "cd" || w == "inlay" || w == "inside" || w == "booklet" || w == "tray":
				score = -1
			case score >= 0 && (w == "cover" || w == "front" || strings.HasPrefix(w, "albumart")):
				score += 2
			case score > 0 && w == "large":
				score++
			case score > 0 && w == "small":
				score--
			}
			if score < 0 {
				break
			}
		}
		switch {
		case score < 0:
		case score > bestScore:
			best, bestScore = name, score
		case score == 0:
			others = append(others, name)
		}
	}
	if best != "" {
		return filepath.Join(dir, best)
	}
	if len(images) == 1 && len(others) == 1 {
		return filepath.Join(dir, others[0])
	}
	return ""
}

// findNamed returns the image in dir named after the first of names it
// has, such as an artist's photo or a book's cover named after its file.
func findNamed(dir string, names ...string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, n := range names {
		for _, e := range entries {
			if !e.IsDir() && isImage(e.Name()) && strings.EqualFold(stem(e.Name()), n) {
				return filepath.Join(dir, e.Name())
			}
		}
	}
	return ""
}

// albumCover finds the cover for a track or book file in its folder, or,
// for a CD 1 / Disc 2 folder, in the album's folder above it.
func albumCover(root, path string) string {
	dir := filepath.Dir(path)
	if dir == root {
		return "" // loose files at the top don't share an album
	}
	if c := findCover(dir); c != "" {
		return c
	}
	if dir != root && under(dir, []string{root}) && discDirRe.MatchString(filepath.Base(dir)) {
		return findCover(filepath.Dir(dir))
	}
	return ""
}

// fillFolderArt gives albums or books without a cover of their own one
// that has turned up in their folders since their files were scanned.
// Scans skip unchanged files, so a cover.jpg added later is only found here.
func fillFolderArt(ctx context.Context, st *store.Store, lib store.Library, kind string, find func(root, path string) string) error {
	missing, err := st.ContainersWithoutLocalArt(ctx, lib.ID, kind)
	if err != nil {
		return err
	}
	for _, c := range missing {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		tried := map[string]bool{}
		for _, p := range c.Paths {
			if tried[filepath.Dir(p)] {
				continue
			}
			tried[filepath.Dir(p)] = true
			if cover := find(rootOf(lib.Paths, p), p); cover != "" {
				if err := st.SetArt(ctx, c.ID, cover, ""); err != nil {
					return err
				}
				break
			}
		}
	}
	return nil
}

// rootOf returns the library folder path is in.
func rootOf(roots []string, path string) string {
	best := ""
	for _, r := range roots {
		if under(path, []string{r}) && len(r) > len(best) {
			best = r
		}
	}
	return best
}
