package scan

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/yeixio/matinee/internal/probe"
	"github.com/yeixio/matinee/internal/store"
)

// Kind is a type of library: which files belong in it and how they're filed.
// Movies, TV, music and audiobooks are built in; library-type plugins
// register more.
type Kind interface {
	Name() string
	Wants(path string) bool
	// Playable reports whether a probed file has what this library plays.
	// Files that don't are left out, along with folders holding nothing else.
	Playable(pr probe.Result) bool
	// Index files one file, creating any containers it lives in, and returns
	// the playable item. The scanner fills in the file and probe fields.
	Index(ctx context.Context, st *store.Store, f File) (store.Item, error)
}

// File is one media file found during a scan.
type File struct {
	Library store.Library
	Root    string // the library folder the file was found under
	Path    string
	Info    fs.FileInfo
	Probe   probe.Result
}

// Unprobed is a Kind whose files aren't audio or video, such as a plugin's
// games. They aren't run through ffprobe, so Playable isn't asked.
type Unprobed interface {
	Kind
	Unprobed()
}

// Session is a Kind that sets something up for a whole scan, such as a
// plugin's sandbox. The scanner calls BeginScan before the first file and
// the function it returns after the last.
type Session interface {
	Kind
	BeginScan(ctx context.Context, lib store.Library) (end func())
}

// Finisher is a Kind with work to do once a scan has filed every file,
// such as adding up a book's parts.
type Finisher interface {
	Kind
	FinishScan(ctx context.Context, st *store.Store, lib store.Library) error
}

// ErrSkip from Index leaves a file out, like one with nothing playable.
var ErrSkip = errUnplayable

var (
	kindsMu sync.RWMutex
	kinds   = map[string]Kind{}
)

// Register adds a library kind. Built-in kinds register from init; plugins
// register theirs while the server runs.
func Register(k Kind) {
	kindsMu.Lock()
	defer kindsMu.Unlock()
	kinds[k.Name()] = k
}

// Unregister removes a library kind, when a plugin is turned off. Libraries
// of that kind keep their items but aren't scanned.
func Unregister(name string) {
	kindsMu.Lock()
	defer kindsMu.Unlock()
	delete(kinds, name)
}

// KindByName returns a registered library kind.
func KindByName(name string) (Kind, bool) {
	kindsMu.RLock()
	defer kindsMu.RUnlock()
	k, ok := kinds[name]
	return k, ok
}

// KindNames lists the registered library kinds.
func KindNames() []string {
	kindsMu.RLock()
	defer kindsMu.RUnlock()
	names := make([]string, 0, len(kinds))
	for n := range kinds {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

func init() {
	Register(movies{})
	Register(tv{})
	Register(music{})
	Register(audiobooks{})
}

// findArt returns the first of names that exists in dir.
func findArt(dir string, names ...string) string {
	for _, n := range names {
		p := filepath.Join(dir, n)
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p
		}
	}
	return ""
}

// ---- movies ----

type movies struct{}

func (movies) Name() string { return "movies" }

func (movies) Wants(path string) bool {
	return isVideo(path) && !strings.Contains(strings.ToLower(filepath.Base(path)), "sample")
}

func (movies) Playable(pr probe.Result) bool { return pr.VideoCodec != "" }

func (movies) Index(ctx context.Context, st *store.Store, f File) (store.Item, error) {
	mi := ParseMovie(f.Path)
	it := store.Item{Kind: "movie", Title: mi.Title, Year: nonZero(mi.Year)}
	dir, s := filepath.Dir(f.Path), stem(f.Path)
	// Shared names like poster.jpg only count when the movie has its own folder.
	if dir != f.Root {
		it.Poster = findArt(dir, s+"-poster.jpg", "poster.jpg", "folder.jpg", "cover.jpg")
		it.Backdrop = findArt(dir, s+"-fanart.jpg", "fanart.jpg", "backdrop.jpg")
	} else {
		it.Poster = findArt(dir, s+"-poster.jpg", s+".jpg")
		it.Backdrop = findArt(dir, s+"-fanart.jpg")
	}
	return it, nil
}

// ---- tv ----

type tv struct{}

func (tv) Name() string                  { return "tv" }
func (tv) Wants(path string) bool        { return movies{}.Wants(path) }
func (tv) Playable(pr probe.Result) bool { return pr.VideoCodec != "" }

func (tv) Index(ctx context.Context, st *store.Store, f File) (store.Item, error) {
	ep := ParseEpisode(f.Root, f.Path)
	if !ep.OK {
		// Not an episode name; file it under its folder as a special.
		ep = EpisodeInfo{Show: cleanTitle(filepath.Base(filepath.Dir(f.Path))), Title: cleanTitle(stem(f.Path))}
	}
	showID, err := st.EnsureContainer(ctx, f.Library.ID, nil, "show", ep.Show, nil, nil)
	if err != nil {
		return store.Item{}, err
	}
	if rel, err := filepath.Rel(f.Root, f.Path); err == nil {
		if parts := strings.Split(filepath.ToSlash(rel), "/"); len(parts) >= 2 {
			showDir := filepath.Join(f.Root, parts[0])
			poster := findArt(showDir, "poster.jpg", "folder.jpg", "cover.jpg")
			backdrop := findArt(showDir, "fanart.jpg", "backdrop.jpg")
			if poster != "" || backdrop != "" {
				if err := st.SetArt(ctx, showID, poster, backdrop); err != nil {
					return store.Item{}, err
				}
			}
		}
	}
	seasonID, err := st.EnsureContainer(ctx, f.Library.ID, &showID, "season", seasonTitle(ep.Season), &ep.Season, nil)
	if err != nil {
		return store.Item{}, err
	}
	it := store.Item{Kind: "episode", ParentID: &seasonID, IndexNumber: nonZero(ep.Episode), Title: ep.Title}
	if it.Title == "" {
		it.Title = "Episode " + strconv.Itoa(ep.Episode)
	}
	it.Poster = findArt(filepath.Dir(f.Path), stem(f.Path)+"-thumb.jpg", stem(f.Path)+".jpg")
	return it, nil
}

// ---- music ----

type music struct{}

func (music) Name() string                  { return "music" }
func (music) Wants(path string) bool        { return isAudio(path) }
func (music) Playable(pr probe.Result) bool { return pr.AudioCodec != "" }

func (music) Index(ctx context.Context, st *store.Store, f File) (store.Item, error) {
	tr := trackFromTags(f.Probe.Tags, ParseTrackPath(f.Root, f.Path))
	artistID, err := st.EnsureContainer(ctx, f.Library.ID, nil, "artist", tr.Artist, nil, nil)
	if err != nil {
		return store.Item{}, err
	}
	albumID, err := st.EnsureContainer(ctx, f.Library.ID, &artistID, "album", tr.Album, nil, nonZero(tr.Year))
	if err != nil {
		return store.Item{}, err
	}
	// An artist photo sits in the artist's folder, one up from the album.
	if artistDir := filepath.Dir(filepath.Dir(f.Path)); artistDir != f.Root && strings.HasPrefix(artistDir, f.Root) {
		if photo := findNamed(artistDir, "artist", "folder", "photo"); photo != "" {
			if err := st.SetArt(ctx, artistID, photo, ""); err != nil {
				return store.Item{}, err
			}
		}
	}
	cover := albumCover(f.Root, f.Path)
	if cover == "" && f.Probe.HasCover {
		cover = "embedded:" + f.Path
	}
	if cover != "" {
		if err := st.SetArt(ctx, albumID, cover, ""); err != nil {
			return store.Item{}, err
		}
	}
	return store.Item{
		Kind: "track", ParentID: &albumID, Title: tr.Title,
		IndexNumber: nonZero(tr.Track), DiscNumber: nonZero(tr.Disc), Year: nonZero(tr.Year),
	}, nil
}

// FinishScan picks up covers added to album folders since their tracks were scanned.
func (music) FinishScan(ctx context.Context, st *store.Store, lib store.Library) error {
	return fillFolderArt(ctx, st, lib, "album", albumCover)
}

// trackFromTags prefers embedded tags and falls back to the folder layout.
func trackFromTags(tags map[string]string, fromPath TrackInfo) TrackInfo {
	t := fromPath
	pick := func(dst *string, keys ...string) bool {
		for _, k := range keys {
			if v := strings.TrimSpace(tags[k]); v != "" {
				*dst = v
				return true
			}
		}
		return false
	}
	if !pick(&t.Artist, "album_artist", "albumartist") {
		if isCompilation(tags) {
			// Without an album artist, a compilation's tracks would each
			// file the album under their own artist.
			t.Artist = "Various Artists"
		} else {
			pick(&t.Artist, "artist")
		}
	}
	pick(&t.Album, "album")
	pick(&t.Title, "title")
	if n := probe.LeadingInt(tags["track"]); n > 0 {
		t.Track = n
	}
	if n := probe.LeadingInt(tags["disc"]); n > 0 {
		t.Disc = n
	}
	if n := probe.LeadingInt(tags["date"]); n > 1000 {
		t.Year = n
	}
	if t.Artist == "" {
		t.Artist = "Unknown Artist"
	}
	if t.Album == "" {
		t.Album = "Unknown Album"
	}
	return t
}

// isCompilation reports whether the tags mark a various-artists album.
func isCompilation(tags map[string]string) bool {
	for _, k := range []string{"compilation", "tcmp", "cpil"} {
		if v := strings.TrimSpace(tags[k]); v == "1" || strings.EqualFold(v, "true") {
			return true
		}
	}
	return false
}

func seasonTitle(n int) string {
	if n == 0 {
		return "Specials"
	}
	return "Season " + strconv.Itoa(n)
}

func nonZero(n int) *int {
	if n == 0 {
		return nil
	}
	return &n
}
