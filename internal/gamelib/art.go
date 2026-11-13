package gamelib

import (
	"bufio"
	"cmp"
	"context"
	"errors"
	"html"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/yeixio/matinee/internal/gamedat"
)

// Box art, best first: a picture beside the game or in the folders game
// front ends and scrapers keep them in, then libretro's box art, then its
// title screens and screenshots. Online pictures are picked from the list
// of the pictures libretro has for a system, so a game whose name differs
// a little from libretro's (another region, a missing "The", a different
// colon) still gets one.

// artExts are the picture files a game's art may be.
var artExts = []string{".png", ".jpg", ".jpeg", ".webp"}

// artFolders are where game front ends and scrapers keep box art, beside
// the games: EmulationStation's, Batocera's, RetroArch's and others'.
var artFolders = []string{
	"boxart", "boxarts", "Named_Boxarts", "covers", "box2dfront", "box", "images",
	"media/box2dfront", "media/covers", "media/boxart", "media/images",
	"downloaded_images", "downloaded_media/covers", "downloaded_media/box2dfront",
}

// artSuffixes are what some front ends add to a game's name for its art.
var artSuffixes = []string{"", "-image", "-boxart", "-thumb", "-cover"}

// localArt is a picture of the game on disk, or "".
func localArt(path, stem string) string {
	dir := filepath.Dir(path)
	names := map[string]string{} // lower case name → as it is, per folder
	look := func(folder string) string {
		entries, err := os.ReadDir(folder)
		if err != nil {
			return ""
		}
		clear(names)
		for _, e := range entries {
			if !e.IsDir() {
				names[strings.ToLower(e.Name())] = e.Name()
			}
		}
		for _, suffix := range artSuffixes {
			for _, ext := range artExts {
				if n, ok := names[strings.ToLower(stem+suffix+ext)]; ok {
					return filepath.Join(folder, n)
				}
			}
		}
		return ""
	}
	if p := look(dir); p != "" {
		return p
	}
	for _, f := range artFolders {
		if p := look(filepath.Join(dir, filepath.FromSlash(f))); p != "" {
			return p
		}
	}
	return ""
}

// artKinds are libretro's kinds of picture, in the order they're tried.
var artKinds = []string{"Named_Boxarts", "Named_Titles", "Named_Snaps"}

// onlineArt is the address of libretro's best picture for a game named
// name (No-Intro's name, or its file's), or "" when libretro has none.
// Without the lists of what libretro has, it guesses its box art's
// address by name.
func (k *Kind) onlineArt(ctx context.Context, sys gamedat.System, name string) string {
	return k.onlineArtOf(ctx, sys, name, artKinds, true)
}

// backdropKinds are libretro's pictures of a game being played, for the
// backdrop behind its page: a screenshot, else its title screen.
var backdropKinds = []string{"Named_Snaps", "Named_Titles"}

// onlineBackdrop is the address of libretro's picture of the game being
// played, or "".
func (k *Kind) onlineBackdrop(ctx context.Context, sys gamedat.System, name string) string {
	return k.onlineArtOf(ctx, sys, name, backdropKinds, false)
}

// onlineArtOf is libretro's first picture of kinds for a game. guess
// makes up the address of the first kind's when libretro's lists can't be
// had.
func (k *Kind) onlineArtOf(ctx context.Context, sys gamedat.System, name string, kinds []string, guess bool) string {
	if sys.Libretro == "" {
		return ""
	}
	base := strings.TrimRight(cmp.Or(k.Art, DefaultArt), "/") + "/" + url.PathEscape(sys.Libretro) + "/"
	addr := func(kind, file string) string { return base + kind + "/" + url.PathEscape(file) + ".png" }
	known := false
	for _, kind := range kinds {
		have, err := k.artList(ctx, sys.Libretro, kind)
		if err != nil || have == nil {
			continue
		}
		known = true
		if file := pickArt(have, name); file != "" {
			return addr(kind, file)
		}
	}
	if known || !guess {
		return ""
	}
	return addr(kinds[0], gamedat.ThumbName(name))
}

// artNames are the pictures libretro has of one kind for one system, by
// name without .png.
type artNames struct {
	exact   map[string]bool
	byTitle map[string][]string // looseTitle → names
}

// pickArt is the name of the picture for a game named name, or "": the
// same name, or else the same game's for another region, the closest one
// first.
func pickArt(have *artNames, name string) string {
	if t := gamedat.ThumbName(name); have.exact[t] {
		return t
	}
	same := have.byTitle[looseTitle(name)]
	if len(same) == 0 {
		return ""
	}
	want := gamedat.Tags(name)
	return slices.MinFunc(same, func(a, b string) int {
		return cmp.Or(
			cmp.Compare(artRank(a, want), artRank(b, want)),
			cmp.Compare(len(a), len(b)),
			strings.Compare(a, b),
		)
	})
}

// regionOrder is which region's art is used when the game's own isn't
// there.
var regionOrder = []string{"USA", "World", "Europe", "Japan"}

var unreleased = regexp.MustCompile(`(?i)\((beta|proto|prototype|demo|sample|hack|pirate|unl)[^)]*\)`)

// artRank is how close a picture's name is to a game's tags: lower is
// closer.
func artRank(name string, want []string) int {
	rank := 0
	if unreleased.MatchString(name) {
		rank += 100
	}
	tags := gamedat.Tags(name)
	region := ""
	if len(tags) > 0 {
		region = tags[0]
	}
	if len(want) > 0 && region == want[0] {
		return rank
	}
	rank += 10
	for i, r := range regionOrder {
		if strings.Contains(region, r) {
			return rank + i
		}
	}
	return rank + len(regionOrder)
}

// looseTitle is a game's title for matching names that differ only in
// how they're written: lower case letters and digits, without "the" and
// with "&" as "and".
func looseTitle(name string) string {
	t := strings.ToLower(gamedat.Title(name))
	t = strings.ReplaceAll(t, "&", " and ")
	t = strings.ReplaceAll(t, "_", " ")
	var words []string
	for _, w := range strings.FieldsFunc(t, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '\'' }) {
		w = strings.ReplaceAll(w, "'", "")
		if w != "" && w != "the" {
			words = append(words, w)
		}
	}
	return strings.Join(words, "")
}

// artLists are libretro's lists of pictures, read once a day at most,
// by system and kind.
type artListKey struct{ system, kind string }

type artList struct {
	at    time.Time
	names *artNames
}

// artList is what pictures libretro has of a kind for a system, kept for a
// month in Dir. nil, with no error, is a kind libretro has none of.
func (k *Kind) artList(ctx context.Context, system, kind string) (*artNames, error) {
	key := artListKey{system, kind}
	k.mu.Lock()
	l, ok := k.arts[key]
	k.mu.Unlock()
	if ok && time.Since(l.at) < 24*time.Hour {
		return l.names, nil
	}
	if k.Dir == "" {
		return nil, errors.New("nowhere to keep lists")
	}
	p := filepath.Join(k.Dir, "art", kind, system+".txt")
	missing := p + ".none"
	fresh := func(p string) bool {
		st, err := os.Stat(p)
		return err == nil && time.Since(st.ModTime()) < listAge
	}
	var names *artNames
	switch {
	case fresh(p):
	case fresh(missing):
		p = ""
	default:
		k.dlMu.Lock()
		if !fresh(p) {
			err := k.fetchArtList(ctx, system, kind, p)
			switch {
			case err == nil:
				os.Remove(missing)
			case errors.Is(err, os.ErrNotExist):
				os.MkdirAll(filepath.Dir(missing), 0o750)
				os.WriteFile(missing, nil, 0o640)
				p = ""
			default:
				// An older copy is better than none.
				if _, serr := os.Stat(p); serr != nil {
					k.dlMu.Unlock()
					k.log().Warn("couldn't get libretro's list of game pictures", "system", system, "kind", kind, "err", err)
					k.rememberArt(key, nil) // try again tomorrow
					return nil, err
				}
			}
		}
		k.dlMu.Unlock()
	}
	if p != "" {
		var err error
		if names, err = readArtList(p); err != nil {
			return nil, err
		}
	}
	k.rememberArt(key, names)
	return names, nil
}

func (k *Kind) rememberArt(key artListKey, names *artNames) {
	k.mu.Lock()
	if k.arts == nil {
		k.arts = map[artListKey]artList{}
	}
	k.arts[key] = artList{at: time.Now(), names: names}
	k.mu.Unlock()
}

// fetchArtList saves the names of libretro's pictures of a kind for a
// system, one a line, from the server's folder listing.
func (k *Kind) fetchArtList(ctx context.Context, system, kind, dst string) error {
	u := strings.TrimRight(cmp.Or(k.Art, DefaultArt), "/") + "/" + url.PathEscape(system) + "/" + kind + "/"
	page := dst + ".html"
	defer os.Remove(page)
	if err := k.download(ctx, u, page); err != nil {
		return err
	}
	b, err := os.ReadFile(page)
	if err != nil {
		return err
	}
	var out strings.Builder
	for _, m := range hrefRE.FindAllStringSubmatch(string(b), -1) {
		n, err := url.PathUnescape(html.UnescapeString(m[1]))
		if err != nil || strings.Contains(n, "/") || !strings.EqualFold(filepath.Ext(n), ".png") {
			continue
		}
		out.WriteString(strings.TrimSuffix(n, filepath.Ext(n)))
		out.WriteByte('\n')
	}
	if out.Len() == 0 {
		return errors.New("libretro's list of pictures was empty")
	}
	tmp := dst + ".tmp"
	if err := os.WriteFile(tmp, []byte(out.String()), 0o640); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

var hrefRE = regexp.MustCompile(`(?i)href="([^"?#]+)"`)

func readArtList(p string) (*artNames, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	names := &artNames{exact: map[string]bool{}, byTitle: map[string][]string{}}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		n := sc.Text()
		if n == "" || names.exact[n] {
			continue
		}
		names.exact[n] = true
		t := looseTitle(n)
		names.byTitle[t] = append(names.byTitle[t], n)
	}
	return names, sc.Err()
}
