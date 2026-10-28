// The arcade plugin's server part: it files MAME ROM sets in an Arcade
// library, with each game's name, year and maker from MAME's own list, and
// artwork from folders next to the ROMs or, failing that, online.
package main

import (
	"bufio"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/yeixio/matinee/plugins/arcade/gamedb"
	plugin "github.com/yeixio/matinee/pluginsdk"
)

func init() {
	plugin.Register(plugin.Handlers{Index: index, Lookup: lookup})
}

func main() {}

// Where the package keeps the game list and the MAME builds. Tests point
// them elsewhere.
var (
	gamesFile = filepath.Join(plugin.PackageDir, "games.json.gz")
	buildsDir = filepath.Join(plugin.PackageDir, "player", "mame")
)

var (
	loadOnce sync.Once
	db       *gamedb.DB
	dbErr    error
	builds   map[string]bool
)

func load() error {
	loadOnce.Do(func() {
		db, dbErr = gamedb.Load(gamesFile)
		builds = map[string]bool{}
		entries, _ := os.ReadDir(buildsDir)
		for _, e := range entries {
			if name, ok := strings.CutSuffix(e.Name(), ".wasm"); ok {
				builds[name] = true
			}
		}
	})
	return dbErr
}

// build names the MAME build for a driver source file, as the arcade
// workflow names it: pacman/pacman.cpp is player/mame/pacman_pacman.js.
func build(source string) string {
	return strings.NewReplacer("/", "_", ".cpp", "").Replace(source)
}

func index(r plugin.IndexRequest) (plugin.IndexResult, error) {
	if err := load(); err != nil {
		return plugin.IndexResult{}, fmt.Errorf("reading the game list: %w", err)
	}
	rel, err := filepath.Rel(r.Root, r.Path)
	if err != nil {
		return plugin.IndexResult{}, nil
	}
	for _, part := range strings.Split(filepath.ToSlash(filepath.Dir(rel)), "/") {
		if notROMs[strings.ToLower(part)] {
			return plugin.IndexResult{}, nil
		}
	}
	ext := strings.ToLower(filepath.Ext(r.Path))
	name := strings.ToLower(strings.TrimSuffix(filepath.Base(r.Path), filepath.Ext(r.Path)))
	g := db.Game(name)
	if g == nil {
		// A BIOS, a device, or something that isn't an arcade game. The
		// player still reads BIOS and device sets when a game needs them.
		return plugin.IndexResult{}, nil
	}

	dir := ""
	if d := filepath.Dir(rel); d != "." {
		dir = filepath.ToSlash(d) + "/"
	}
	bases := artBases(r.Root, filepath.Dir(r.Path))
	b := build(g.Source)
	playable, why := true, ""
	switch {
	case g.Disk:
		playable, why = false, "needs a disk image"
	case !builds[b]:
		playable, why = false, "not in this version of the plugin"
	}

	it := &plugin.Item{Kind: "game", Title: g.Description, Year: year(g.Year)}
	it.Facts = facts(g, playable, why)
	if genre := categories(bases).genre(g); genre != "" {
		it.Genres = []string{genre}
	}
	it.Poster, it.Backdrop = localArt(bases, g)
	art := "online"
	if it.Poster != "" || it.Backdrop != "" {
		art = "local"
	}
	it.Data = map[string]any{
		"name":      g.Name,
		"ext":       ext,
		"dir":       dir,
		"needs":     g.Needs,
		"build":     b,
		"playable":  playable,
		"rotate":    g.Rotate,
		"width":     g.Width,
		"height":    g.Height,
		"saveState": g.SaveState,
		"art":       art,
		"mame":      db.MAME,
	}
	return plugin.IndexResult{Item: it}, nil
}

func year(s string) int {
	if len(s) != 4 {
		return 0
	}
	y, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return y
}

func facts(g *gamedb.Game, playable bool, why string) []plugin.Fact {
	var fs []plugin.Fact
	add := func(label, value string) {
		if value != "" {
			fs = append(fs, plugin.Fact{Label: label, Value: value})
		}
	}
	add("Manufacturer", g.Manufacturer)
	if g.Players > 0 {
		add("Players", strconv.Itoa(g.Players))
	}
	controls := strings.Join(g.Controls, ", ")
	switch {
	case g.Buttons == 1:
		controls += ", 1 button"
	case g.Buttons > 1:
		controls += fmt.Sprintf(", %d buttons", g.Buttons)
	}
	add("Controls", strings.TrimPrefix(controls, ", "))
	switch g.Rotate {
	case 90, 270:
		add("Screen", "Vertical")
	case 0, 180:
		if g.Width > 0 {
			add("Screen", "Horizontal")
		}
	}
	add("Emulation", map[string]string{"good": "Good", "imperfect": "Imperfect", "preliminary": "Preliminary"}[g.Status])
	if playable {
		add("Plays in the browser", "Yes")
	} else {
		add("Plays in the browser", "No, "+why)
	}
	add("MAME name", g.Name)
	return fs
}

// notROMs are folders in a MAME folder that hold zips that aren't games:
// a library can be the whole MAME folder, with roms/ and snap/ in it.
var notROMs = map[string]bool{"artwork": true, "samples": true, "cheat": true, "ctrlr": true, "hash": true, "plugins": true, "language": true, "bgfx": true}

// artBases are the folders artwork and catver.ini are looked for in: the
// folder a game is in and the one above it, as far up as the library
// folder. A plugin sees only its library folders, so artwork has to be in
// one: point the library at the MAME folder that has roms/ and snap/ in it,
// or put snap/ inside the ROM folder.
func artBases(root, dir string) []string {
	bases := []string{dir}
	if dir != root {
		if up := filepath.Dir(dir); up != dir {
			bases = append(bases, up)
		}
	}
	if !slices.Contains(bases, root) {
		bases = append(bases, root)
	}
	return bases
}

// Artwork folders, as MAME and its front ends lay them out, best first.
var (
	posterFolders   = []string{"flyers", "flyer", "titles", "snap", "snaps"}
	backdropFolders = []string{"snap", "snaps", "titles"}
)

// localArt looks for a game's pictures in artwork folders, such as
// snap/pacman.png and flyers/pacman.jpg. A clone without its own uses its
// parent's.
func localArt(bases []string, g *gamedb.Game) (poster, backdrop string) {
	names := []string{g.Name}
	if g.CloneOf != "" {
		names = append(names, g.CloneOf)
	}
	find := func(folders []string) string {
		for _, base := range bases {
			for _, folder := range folders {
				for _, n := range names {
					for _, ext := range []string{".png", ".jpg"} {
						p := filepath.Join(base, folder, n+ext)
						if info, err := os.Stat(p); err == nil && info.Mode().IsRegular() {
							return p
						}
					}
				}
			}
		}
		return ""
	}
	poster, backdrop = find(posterFolders), find(backdropFolders)
	if backdrop == poster {
		backdrop = ""
	}
	return poster, backdrop
}

// catver.ini, in the folders artwork comes from, sorts games into
// categories such as "Maze / Collect".
type catver map[string]string

var (
	catversMu sync.Mutex
	catvers   = map[string]catver{}
)

func categories(bases []string) catver {
	catversMu.Lock()
	defer catversMu.Unlock()
	key := strings.Join(bases, "\x00")
	if c, ok := catvers[key]; ok {
		return c
	}
	c := catver{}
	for _, base := range bases {
		f, err := os.Open(filepath.Join(base, "catver.ini"))
		if err != nil {
			continue
		}
		section := ""
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			switch {
			case strings.HasPrefix(line, "["):
				section = strings.ToLower(strings.Trim(line, "[]"))
			case section == "category" || section == "":
				if k, v, ok := strings.Cut(line, "="); ok {
					c[strings.TrimSpace(k)] = strings.TrimSpace(v)
				}
			}
		}
		f.Close()
		break
	}
	catvers[key] = c
	return c
}

// genre is the first part of a game's category, "Maze" for "Maze /
// Collect", without catver's "* Mature *" marks.
func (c catver) genre(g *gamedb.Game) string {
	cat := c[g.Name]
	if cat == "" && g.CloneOf != "" {
		cat = c[g.CloneOf]
	}
	cat = strings.TrimSpace(strings.ReplaceAll(cat, "* Mature *", ""))
	first, _, _ := strings.Cut(cat, "/")
	return strings.TrimSpace(first)
}

// thumbnails is where the online artwork comes from: the libretro
// thumbnail collection, which names pictures after MAME's descriptions.
const thumbnails = "https://raw.githubusercontent.com/libretro-thumbnails/MAME/master/"

// lookup finds artwork online for games without pictures of their own.
func lookup(r plugin.LookupRequest) (*plugin.Details, error) {
	if r.Kind != "game" || r.Data["art"] != "online" || r.Title == "" {
		return nil, nil
	}
	file := thumbnailName(r.Title) + ".png"
	var d plugin.Details
	for _, folder := range []string{"Named_Boxarts", "Named_Titles", "Named_Snaps"} {
		u := thumbnails + folder + "/" + url.PathEscape(file)
		res, err := plugin.Fetch(plugin.Request{Method: "HEAD", URL: u})
		if err != nil {
			return nil, err
		}
		if res.Status != 200 {
			continue
		}
		switch {
		case d.Poster == "":
			d.Poster = u
		case folder == "Named_Snaps":
			d.Backdrop = u
		}
	}
	if d.Poster == "" {
		return nil, nil
	}
	return &d, nil
}

// thumbnailName is a title as the thumbnail collection writes it: these
// characters become underscores.
func thumbnailName(title string) string {
	return strings.Map(func(r rune) rune {
		if strings.ContainsRune(`&*/:`+"`"+`<>?\|"`, r) {
			return '_'
		}
		return r
	}, title)
}
