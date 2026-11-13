package gamelib

import (
	"archive/zip"
	"cmp"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/yeixio/matinee/internal/gamedat"
	"github.com/yeixio/matinee/internal/scan"
	"github.com/yeixio/matinee/internal/store"
)

// Arcade games are MAME sets: one zip (or 7z) per machine, named by MAME's
// short name. What each is comes from MAME's own list of machines for the
// version of MAME the server plays them with (gamedat.MAMEVersion),
// downloaded from MAME's releases the first time and kept, cut down to the
// arcade games, in Dir.

// DefaultMAMEList is where MAME's list of machines comes from: %s is the
// version without its dot, as MAME names its releases (0289).
const DefaultMAMEList = "https://github.com/mamedev/mame/releases/download/mame%[1]s/mame%[1]slx.zip"

// arcadeGame is what's kept of one arcade machine in MAME's list.
type arcadeGame struct {
	Description  string   `json:"d"`
	Year         string   `json:"y,omitempty"`
	Manufacturer string   `json:"m,omitempty"`
	CloneOf      string   `json:"c,omitempty"`
	Players      int      `json:"p,omitempty"`
	Buttons      int      `json:"b,omitempty"`
	Controls     []string `json:"ctl,omitempty"`
	Rotate       int      `json:"r,omitempty"`
	Status       string   `json:"st,omitempty"`
}

// arcadeList is MAME's list of machines as the Games library keeps it.
type arcadeList struct {
	Version string                 `json:"version"`
	Games   map[string]*arcadeGame `json:"games"`
	// Others are sets that aren't games: BIOS, devices and the like, which
	// games need but which aren't shown.
	Others []string `json:"others"`

	others map[string]bool
}

// arcadeNotROMs are folders in a MAME folder that hold zips that aren't
// games, so a library can be a whole MAME folder.
var arcadeNotROMs = map[string]bool{"artwork": true, "samples": true, "cheat": true, "ctrlr": true, "hash": true,
	"plugins": true, "language": true, "bgfx": true, "snap": true, "snaps": true, "titles": true, "flyers": true}

// arcade is MAME's list, read once; nil when it can't be had yet, which is
// tried again after an hour.
func (k *Kind) arcade(ctx context.Context) *arcadeList {
	k.arcadeMu.Lock()
	defer k.arcadeMu.Unlock()
	if k.arcadeL != nil || time.Since(k.arcadeTried) < time.Hour {
		return k.arcadeL
	}
	k.arcadeTried = time.Now()
	l, err := k.loadArcade(ctx)
	if err != nil {
		k.log().Warn("couldn't get MAME's list of arcade games; arcade games are named by their files", "err", err)
		return nil
	}
	k.arcadeL = l
	return l
}

func (k *Kind) loadArcade(ctx context.Context) (*arcadeList, error) {
	if k.Dir == "" {
		return nil, fmt.Errorf("no folder to keep it in")
	}
	p := filepath.Join(k.Dir, "mame", "mame-"+gamedat.MAMEVersion+".json.gz")
	if l, err := readArcade(p); err == nil {
		return l, nil
	}
	v := strings.ReplaceAll(gamedat.MAMEVersion, ".", "")
	u := fmt.Sprintf(cmp.Or(k.MAMEList, DefaultMAMEList), v)
	zipPath := filepath.Join(k.Dir, "mame", "mame"+v+"lx.zip")
	k.log().Info("getting MAME's list of arcade games", "url", u)
	if err := k.download(ctx, u, zipPath); err != nil {
		return nil, err
	}
	defer os.Remove(zipPath)
	l, err := parseArcade(zipPath)
	if err != nil {
		return nil, err
	}
	if err := writeArcade(p, l); err != nil {
		return nil, err
	}
	return l, nil
}

// parseArcade reads MAME's list from the zip MAME publishes it in.
func parseArcade(zipPath string) (*arcadeList, error) {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	for _, f := range zr.File {
		if !strings.EqualFold(filepath.Ext(f.Name), ".xml") {
			continue
		}
		r, err := f.Open()
		if err != nil {
			return nil, err
		}
		defer r.Close()
		ml, err := gamedat.ParseXML(r, func(string) bool { return false })
		if err != nil {
			return nil, err
		}
		return arcadeFrom(ml), nil
	}
	return nil, fmt.Errorf("no list in %s", filepath.Base(zipPath))
}

// arcadeFrom keeps MAME's arcade games: machines with ROMs that start on
// their own, and aren't a BIOS, a device or a mechanical one (pinball,
// slot machines).
func arcadeFrom(ml *gamedat.List) *arcadeList {
	l := &arcadeList{Version: ml.Version, Games: map[string]*arcadeGame{}}
	for name, m := range ml.Machines {
		if !m.HasRoms {
			continue
		}
		if m.IsBIOS || m.IsDevice || m.IsMechanical || !m.Runnable {
			l.Others = append(l.Others, name)
			continue
		}
		l.Games[name] = &arcadeGame{Description: m.Description, Year: m.Year, Manufacturer: m.Manufacturer,
			CloneOf: m.CloneOf, Players: m.Players, Buttons: m.Buttons, Controls: m.Controls, Rotate: m.Rotate, Status: m.Status}
	}
	slices.Sort(l.Others)
	return l
}

func readArcade(p string) (*arcadeList, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	var l arcadeList
	if err := json.NewDecoder(zr).Decode(&l); err != nil {
		return nil, err
	}
	if len(l.Games) == 0 {
		return nil, fmt.Errorf("%s has no games", p)
	}
	return &l, nil
}

func writeArcade(p string, l *arcadeList) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), ".mame-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	zw := gzip.NewWriter(tmp)
	if err := json.NewEncoder(zw).Encode(l); err != nil {
		tmp.Close()
		return err
	}
	if err := zw.Close(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), p)
}

func (l *arcadeList) other(name string) bool {
	if l.others == nil {
		l.others = make(map[string]bool, len(l.Others))
		for _, n := range l.Others {
			l.others[n] = true
		}
	}
	return l.others[name]
}

// indexArcade files one MAME set.
func (k *Kind) indexArcade(ctx context.Context, sys gamedat.System, f scan.File) (store.Item, error) {
	if rel, err := filepath.Rel(f.Root, filepath.Dir(f.Path)); err == nil {
		for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
			if arcadeNotROMs[strings.ToLower(part)] {
				return store.Item{}, scan.ErrSkip
			}
		}
	}
	fileStem := strings.TrimSuffix(filepath.Base(f.Path), filepath.Ext(f.Path))
	name := strings.ToLower(fileStem)
	it := store.Item{Kind: ItemKind, Title: name}
	it.Facts = []store.Fact{{Label: FactSystem, Value: sys.Name}}
	group, edition := name, ""
	if l := k.arcade(ctx); l != nil {
		g := l.Games[name]
		if g == nil {
			if l.other(name) {
				// A BIOS or device: games use it, but it isn't one.
				return store.Item{}, scan.ErrSkip
			}
			return store.Item{}, scan.Skip(SkipUnknown, topFolder(f.Root, f.Path))
		}
		it.Title = cmp.Or(gamedat.Title(g.Description), name)
		if y, err := strconv.Atoi(g.Year); err == nil {
			it.Year = &y
		}
		if g.Manufacturer != "" {
			it.Facts = append(it.Facts, store.Fact{Label: FactPublisher, Value: g.Manufacturer})
		}
		if g.Players > 0 {
			it.Facts = append(it.Facts, store.Fact{Label: FactPlayers, Value: strconv.Itoa(g.Players)})
		}
		if g.Rotate == 90 || g.Rotate == 270 {
			it.Facts = append(it.Facts, store.Fact{Label: FactScreen, Value: "Vertical"})
		}
		group = cmp.Or(g.CloneOf, name)
		edition = strings.Join(gamedat.Tags(g.Description), " · ")
		// libretro's pictures of arcade games are named by MAME's
		// description.
		it.Poster = cmp.Or(localArt(f.Path, fileStem), k.onlineArt(ctx, sys, g.Description))
		it.Backdrop = k.onlineBackdrop(ctx, sys, g.Description)
	} else {
		it.Poster = localArt(f.Path, fileStem)
	}
	it.PluginData = groupData(sys.Key+"|"+group, edition)
	return it, nil
}

// gameData is what the Games library keeps with each game: which game it
// is a version of, for grouping a game's regions and an arcade game's
// clones as one (see FinishScan).
type gameData struct {
	Group   string `json:"group"`
	Edition string `json:"edition,omitempty"`
}

// SystemKey is the system (gamedat's key) a game with the data the Games
// library keeps with it (store.Item.PluginData) is filed for, or "".
func SystemKey(data json.RawMessage) string {
	var d gameData
	if json.Unmarshal(data, &d) != nil {
		return ""
	}
	key, _, _ := strings.Cut(d.Group, "|")
	return key
}

// InLibrary says whether an item with this data is a game a Games library
// filed (rather than, say, a plugin's).
func InLibrary(data json.RawMessage) bool {
	var d gameData
	return json.Unmarshal(data, &d) == nil && d.Group != ""
}

func groupData(group, edition string) json.RawMessage {
	b, _ := json.Marshal(gameData{Group: group, Edition: edition})
	return b
}

// editionOf is what tells one version of a console game from another: the
// tags in its No-Intro name, such as "USA" or "Europe · Rev 1".
func editionOf(name string) string {
	return strings.Join(gamedat.Tags(name), " · ")
}

// FinishScan groups the versions of each game, so it shows once: an
// arcade game's clones under it, a console game's other regions and
// revisions under the one most likely wanted.
func (k *Kind) FinishScan(ctx context.Context, st *store.Store, lib store.Library) error {
	items, err := st.GameVersions(ctx, lib.ID)
	if err != nil {
		return err
	}
	groups := map[string][]store.GameVersion{}
	for _, it := range items {
		var d gameData
		if json.Unmarshal(it.Data, &d) != nil || d.Group == "" {
			continue
		}
		it.Edition = d.Edition
		groups[d.Group] = append(groups[d.Group], it)
	}
	for _, g := range groups {
		slices.SortFunc(g, func(a, b store.GameVersion) int {
			return cmp.Or(cmp.Compare(versionRank(a), versionRank(b)), cmp.Compare(a.ID, b.ID))
		})
		for i, it := range g {
			of := int64(0)
			if i > 0 {
				of = g[0].ID
			}
			if it.VersionOf == of && it.OldEdition == it.Edition {
				continue
			}
			if err := st.SetGameVersion(ctx, it.ID, of, it.Edition); err != nil {
				return err
			}
		}
	}
	return nil
}

// versionRank is how likely a version of a game is the one people want:
// an arcade parent set before its clones, then USA, World and Europe
// releases, then the rest. Lower is likelier.
func versionRank(v store.GameVersion) int {
	var d gameData
	json.Unmarshal(v.Data, &d)
	rank := 0
	if _, set, ok := strings.Cut(d.Group, "|"); ok && strings.HasPrefix(d.Group, "mame|") && set != strings.ToLower(strings.TrimSuffix(filepath.Base(v.Path), filepath.Ext(v.Path))) {
		rank += 100 // a clone
	}
	e := strings.ToLower(d.Edition)
	switch {
	case strings.Contains(e, "usa"), strings.Contains(e, "world"):
	case strings.Contains(e, "europe"):
		rank += 1
	case e == "":
		rank += 2
	default:
		rank += 3
	}
	for _, bad := range []string{"beta", "proto", "demo", "sample", "hack", "bootleg", "pirate"} {
		if strings.Contains(e, bad) {
			rank += 10
		}
	}
	return rank
}

// misfiledArcade says whether an archive outside the arcade folder is an
// arcade set all the same: named for one of MAME's games, with no
// console's game file in it. Sets do end up in a console's folder.
func (k *Kind) misfiledArcade(ctx context.Context, path string) bool {
	if !gamedat.IsArchive(path) {
		return false
	}
	name := strings.ToLower(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)))
	l := k.arcade(ctx)
	if l == nil || l.Games[name] == nil {
		return false
	}
	files, err := gamedat.ListArchive(path)
	if err != nil {
		return false
	}
	for _, f := range files {
		if _, ok := gamedat.SystemByExt(f.Name); ok {
			return false
		}
	}
	return true
}
