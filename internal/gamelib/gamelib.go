// Package gamelib is the Games library: game files in a folder, one folder
// per system as Box Office keeps them, each matched by its contents against
// No-Intro's list of that system's games (from libretro's database), with
// its year, maker, genre, players and box art. Games are played on the
// server and streamed (see internal/gamestream).
//
// It only reads game files, and never looks for, links to or downloads
// games: only the lists of games and their box art come from the internet.
package gamelib

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/yeixio/matinee/internal/gamedat"
	"github.com/yeixio/matinee/internal/probe"
	"github.com/yeixio/matinee/internal/scan"
	"github.com/yeixio/matinee/internal/store"
)

// KindName is the Games library's kind.
const KindName = "games"

// ItemKind is a game's item kind, the one the Arcade plugin's games have
// too.
const ItemKind = "game"

// Where the lists and box art come from, unless set otherwise.
const (
	DefaultLibretroDB = "https://raw.githubusercontent.com/libretro/libretro-database/master"
	DefaultArt        = "https://thumbnails.libretro.com"
)

// Facts' labels. The web player shows them in its own language
// (games.fact.* in web.json); other clients show them as they are.
const (
	FactSystem    = store.FactPlatform
	FactRegion    = "region"
	FactPublisher = "publisher"
	FactPlayers   = "players"
	FactGenre     = "genre"
	// FactScreen is "Vertical" for an arcade game with an upright screen.
	FactScreen = "screen"
)

// Kind is the Games library kind.
type Kind struct {
	// Dir keeps the lists downloaded: data/games/lists.
	Dir string
	// LibretroDB and Art override where the lists and box art come from,
	// such as for a mirror or a test; empty uses the defaults.
	LibretroDB string
	Art        string
	// MAMEList overrides where MAME's list of machines comes from (see
	// DefaultMAMEList).
	MAMEList string
	HTTP     *http.Client
	Log      *slog.Logger

	mu    sync.Mutex
	lists map[string]*lists // by system key
	arts  map[artListKey]artList
	dlMu  sync.Mutex

	arcadeMu    sync.Mutex
	arcadeL     *arcadeList
	arcadeTried time.Time
}

// lists are what's known about one system's games.
type lists struct {
	at    time.Time
	ix    *gamedat.Index // nil when there's no list
	facts gamedat.FactIndex
}

var (
	_ scan.Unprobed = (*Kind)(nil)
	_ scan.Finisher = (*Kind)(nil)
)

func (k *Kind) Name() string               { return KindName }
func (k *Kind) Unprobed()                  {}
func (k *Kind) Playable(probe.Result) bool { return true }

// Wants takes the files of every system Matinee knows, and archives, which
// may hold one. Which system a file is for is worked out in Index.
func (k *Kind) Wants(path string) bool {
	return gamedat.IsGameFile(filepath.Base(path))
}

// Index files one game.
func (k *Kind) Index(ctx context.Context, st *store.Store, f scan.File) (store.Item, error) {
	sys, inside, ok := SystemFor(f.Root, f.Path)
	if !sys.Arcade && k.misfiledArcade(ctx, f.Path) {
		sys, ok = gamedat.Systems[0], true
	}
	switch {
	case ok && sys.Arcade:
		return k.indexArcade(ctx, sys, f)
	case !ok:
		return store.Item{}, scan.Skip(SkipUnknown, topFolder(f.Root, f.Path))
	}
	if sys.Disc && strings.EqualFold(filepath.Ext(f.Path), ".cue") && hasPlaylist(filepath.Dir(f.Path)) {
		// The .m3u beside it lists this disc with the game's others.
		return store.Item{}, scan.ErrSkip
	}
	base := filepath.Base(f.Path)
	if inside != "" {
		base = filepath.Base(inside)
	}
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	name, facts := stem, (*gamedat.Facts)(nil)
	if !sys.Disc {
		l := k.listsFor(ctx, sys)
		if l.ix != nil {
			crcs := k.crcs(sys, f, inside)
			if e, crc, ok := l.ix.ByCRC(crcs, 0); ok {
				name, facts = e.Name(), l.facts[crc]
			} else if e, ok := l.ix.ByName(stem); ok {
				name = e.Name()
				if len(e.Roms) > 0 {
					facts = l.facts[e.Roms[0].CRC]
				}
			}
		}
	}
	it := k.item(sys, name, facts)
	fileStem := strings.TrimSuffix(filepath.Base(f.Path), filepath.Ext(f.Path))
	it.Poster = localArt(f.Path, fileStem)
	if it.Poster == "" && stem != fileStem {
		it.Poster = localArt(f.Path, stem)
	}
	if it.Poster == "" && name != fileStem {
		it.Poster = localArt(f.Path, name)
	}
	if it.Poster == "" {
		it.Poster = k.onlineArt(ctx, sys, name)
	}
	it.Backdrop = k.onlineBackdrop(ctx, sys, name)
	it.PluginData = groupData(sys.Key+"|"+looseTitle(cmp.Or(gamedat.Title(name), name)), editionOf(name))
	return it, nil
}

// SkipUnknown is why a file is left out (scan.Skip) when Matinee can't
// tell it's a game for a system it knows, such as a zip that holds no
// console's game and isn't one of MAME's sets.
const SkipUnknown = "unknown"

// topFolder is the folder under the library's that path is in, which is
// usually its system's, or "" for a file at the top.
func topFolder(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return ""
	}
	if first, _, ok := strings.Cut(filepath.ToSlash(rel), "/"); ok {
		return first
	}
	return ""
}

func (k *Kind) item(sys gamedat.System, name string, facts *gamedat.Facts) store.Item {
	it := store.Item{Kind: ItemKind, Title: cmp.Or(gamedat.Title(name), name)}
	it.Facts = []store.Fact{{Label: FactSystem, Value: sys.Name}}
	if tags := gamedat.Tags(name); len(tags) > 0 {
		it.Facts = append(it.Facts, store.Fact{Label: FactRegion, Value: strings.Join(tags, " · ")})
	}
	if facts != nil {
		if y, err := strconv.Atoi(strings.TrimSpace(facts.Year)); err == nil && y > 1950 && y < 2100 {
			it.Year = &y
		}
		if m := cmp.Or(facts.Publisher, facts.Developer); m != "" {
			it.Facts = append(it.Facts, store.Fact{Label: FactPublisher, Value: m})
		}
		if facts.Players > 0 {
			it.Facts = append(it.Facts, store.Fact{Label: FactPlayers, Value: strconv.Itoa(facts.Players)})
		}
		if facts.Genre != "" {
			it.Facts = append(it.Facts, store.Fact{Label: FactGenre, Value: facts.Genre})
			it.Details = &store.Metadata{Genres: []string{facts.Genre}}
		}
	}
	return it
}

// SystemFor says which system a game file is for: by the name of a folder
// it's in, below root (snes, "Super Nintendo"), or else by its extension,
// or for an archive by the extensions of the files in it. inside is the
// game's name in an archive.
func SystemFor(root, path string) (sys gamedat.System, inside string, ok bool) {
	archive := gamedat.IsArchive(path)
	if archive {
		files, err := gamedat.ListArchive(path)
		if err != nil {
			return gamedat.System{}, "", false
		}
		// The biggest file a console's, or failing that the biggest.
		var best *gamedat.File
		for i, f := range files {
			if _, console := gamedat.SystemByExt(f.Name); console {
				if best == nil || f.Size > best.Size {
					best = &files[i]
				}
			}
		}
		if best != nil {
			inside = best.Name
		}
	}
	for dir := filepath.Dir(path); ; dir = filepath.Dir(dir) {
		if s, ok := gamedat.SystemByFolder(filepath.Base(dir)); ok {
			if !archive || inside != "" || s.Arcade {
				return s, inside, true
			}
		}
		if dir == root || dir == filepath.Dir(dir) || !strings.HasPrefix(dir, root) {
			break
		}
	}
	if inside != "" {
		s, ok := gamedat.SystemByExt(inside)
		return s, inside, ok
	}
	if archive {
		return gamedat.System{}, "", false
	}
	s, ok := gamedat.SystemByExt(path)
	return s, "", ok
}

func hasPlaylist(dir string) bool {
	entries, _ := os.ReadDir(dir)
	return slices.ContainsFunc(entries, func(e os.DirEntry) bool { return strings.EqualFold(filepath.Ext(e.Name()), ".m3u") })
}

// crcs are a game's CRCs for matching: the one its archive lists, or the
// file read whole, and without its header for a system whose dumps may
// have one.
func (k *Kind) crcs(sys gamedat.System, f scan.File, inside string) []uint32 {
	if inside != "" {
		files, err := gamedat.ListArchive(f.Path)
		if err != nil {
			return nil
		}
		i := slices.IndexFunc(files, func(x gamedat.File) bool { return x.Name == inside })
		if i < 0 {
			return nil
		}
		out := []uint32{files[i].CRC}
		if sys.Header != gamedat.NoHeader {
			if rc, err := gamedat.OpenInArchive(f.Path, inside); err == nil {
				if more, err := gamedat.CRCs(rc, files[i].Size, sys.Header); err == nil && len(more) > 1 {
					out = append(out, more[1:]...)
				}
				rc.Close()
			}
		}
		return out
	}
	if f.Info.Size() > gamedat.MaxHashSize {
		return nil
	}
	r, err := os.Open(f.Path)
	if err != nil {
		return nil
	}
	defer r.Close()
	crcs, _ := gamedat.CRCs(r, f.Info.Size(), sys.Header)
	return crcs
}

// listsFor are a system's lists, read once a day at most. Lists that can't
// be had leave games named after their files.
func (k *Kind) listsFor(ctx context.Context, sys gamedat.System) *lists {
	k.mu.Lock()
	l := k.lists[sys.Key]
	k.mu.Unlock()
	if l != nil && time.Since(l.at) < 24*time.Hour {
		return l
	}
	l = &lists{at: time.Now(), facts: gamedat.FactIndex{}}
	if dat, err := k.dat(ctx, "no-intro", sys.Libretro); err != nil {
		k.log().Warn("couldn't get No-Intro's list of games", "system", sys.Name, "err", err)
	} else if dat != nil {
		l.ix = gamedat.NewIndex(dat)
	}
	for _, kind := range []string{"releaseyear", "publisher", "developer", "genre", "maxusers"} {
		if dat, err := k.dat(ctx, kind, sys.Libretro); err == nil && dat != nil {
			l.facts.Add(dat)
		}
	}
	k.mu.Lock()
	if k.lists == nil {
		k.lists = map[string]*lists{}
	}
	k.lists[sys.Key] = l
	k.mu.Unlock()
	return l
}

// dat is one of libretro's lists for a system, such as no-intro or genre,
// downloaded the first time and again after a month. nil, with no error,
// is a list libretro doesn't have.
func (k *Kind) dat(ctx context.Context, kind, system string) (*gamedat.DAT, error) {
	p, err := k.datFile(ctx, kind, system)
	if err != nil || p == "" {
		return nil, err
	}
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return gamedat.ParseClrMame(f)
}

const listAge = 30 * 24 * time.Hour

func (k *Kind) datFile(ctx context.Context, kind, system string) (string, error) {
	if k.Dir == "" || system == "" {
		return "", nil
	}
	p := filepath.Join(k.Dir, kind, system+".dat")
	missing := p + ".none"
	if st, err := os.Stat(p); err == nil && time.Since(st.ModTime()) < listAge {
		return p, nil
	}
	if st, err := os.Stat(missing); err == nil && time.Since(st.ModTime()) < listAge {
		return "", nil
	}
	k.dlMu.Lock()
	defer k.dlMu.Unlock()
	if st, err := os.Stat(p); err == nil && time.Since(st.ModTime()) < listAge {
		return p, nil // fetched while this waited
	}
	u := strings.TrimRight(cmp.Or(k.LibretroDB, DefaultLibretroDB), "/") + "/metadat/" + kind + "/" + url.PathEscape(system) + ".dat"
	err := k.download(ctx, u, p)
	switch {
	case err == nil:
		os.Remove(missing)
		return p, nil
	case errors.Is(err, os.ErrNotExist):
		os.MkdirAll(filepath.Dir(missing), 0o750)
		os.WriteFile(missing, nil, 0o640)
		return "", nil
	}
	// An older copy is better than none.
	if _, serr := os.Stat(p); serr == nil {
		return p, nil
	}
	return "", err
}

// download saves a URL to a file, whole or not at all. A 404 is
// os.ErrNotExist.
func (k *Kind) download(ctx context.Context, u, dst string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Matinee")
	c := k.HTTP
	if c == nil {
		c = http.DefaultClient
	}
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return os.ErrNotExist
	default:
		return fmt.Errorf("%s answered %s", req.URL.Host, resp.Status)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".download-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := io.Copy(tmp, resp.Body); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), dst)
}

func (k *Kind) log() *slog.Logger {
	if k.Log != nil {
		return k.Log
	}
	return slog.Default()
}
