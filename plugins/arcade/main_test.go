package main

import (
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"github.com/yeixio/matinee/plugins/arcade/gamedb"
	plugin "github.com/yeixio/matinee/pluginsdk"
)

// setUp builds the game list from testdata/list.xml, with MAME builds for
// the given drivers, and points the plugin at them.
func setUp(t *testing.T, drivers ...string) {
	t.Helper()
	f, err := os.Open("testdata/list.xml")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	list, err := gamedb.FromXML(f)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	out, err := os.Create(filepath.Join(dir, "games.json.gz"))
	if err != nil {
		t.Fatal(err)
	}
	if err := list.Write(out); err != nil {
		t.Fatal(err)
	}
	out.Close()
	os.MkdirAll(filepath.Join(dir, "mame"), 0o755)
	for _, d := range drivers {
		os.WriteFile(filepath.Join(dir, "mame", build(d)+".wasm"), nil, 0o644)
	}
	gamesFile, buildsDir = filepath.Join(dir, "games.json.gz"), filepath.Join(dir, "mame")
	loadOnce, db, builds = sync.Once{}, nil, nil
	catvers = map[string]catver{}
}

func touch(t *testing.T, paths ...string) {
	t.Helper()
	for _, p := range paths {
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestGameList(t *testing.T) {
	setUp(t)
	if err := load(); err != nil {
		t.Fatal(err)
	}
	if db.MAME != "0.289" {
		t.Errorf("MAME version %q", db.MAME)
	}
	var names []string
	for _, g := range db.Games {
		names = append(names, g.Name)
	}
	// No BIOS, devices, or computers.
	if want := []string{"puckman", "pacman", "galaga", "mslug", "kinst"}; !slices.Equal(names, want) {
		t.Errorf("games %v, want %v", names, want)
	}
	pac := db.Game("pacman")
	if pac.CloneOf != "puckman" || !slices.Equal(pac.Needs, []string{"puckman"}) || pac.Rotate != 90 || !pac.SaveState {
		t.Errorf("pacman: %+v", pac)
	}
	if g := db.Game("galaga"); !slices.Equal(g.Needs, []string{"namco51"}) || !slices.Equal(g.Controls, []string{"Joystick (2-way)"}) || g.Buttons != 1 {
		t.Errorf("galaga: %+v", g)
	}
	// The BIOS, but not a device without ROMs.
	if g := db.Game("mslug"); !slices.Equal(g.Needs, []string{"neogeo"}) {
		t.Errorf("mslug needs %v", g.Needs)
	}
	if !db.Game("kinst").Disk {
		t.Error("kinst needs a disk")
	}
}

func TestIndex(t *testing.T) {
	setUp(t, "pacman/pacman.cpp", "midway/kinst.cpp")
	// A MAME folder: ROMs in roms/, artwork beside them.
	root := t.TempDir()
	touch(t,
		filepath.Join(root, "roms", "pacman.zip"),
		filepath.Join(root, "roms", "galaga.zip"),
		filepath.Join(root, "roms", "neogeo.zip"),
		filepath.Join(root, "kinst.zip"),
		filepath.Join(root, "artwork", "pacman.zip"),
		filepath.Join(root, "snap", "puckman.png"), // the parent set's
		filepath.Join(root, "flyers", "pacman.jpg"),
	)
	os.WriteFile(filepath.Join(root, "catver.ini"), []byte(";; catver\n[Category]\npuckman=Maze / Collect\nkinst=Fighter / Versus * Mature *\n"), 0o644)

	idx := func(rel string) *plugin.Item {
		t.Helper()
		res, err := index(plugin.IndexRequest{Root: root, Path: filepath.Join(root, rel)})
		if err != nil {
			t.Fatal(err)
		}
		return res.Item
	}

	pac := idx("roms/pacman.zip")
	if pac == nil || pac.Kind != "game" || pac.Title != "Pac-Man (Midway)" || pac.Year != 1980 {
		t.Fatalf("pacman: %+v", pac)
	}
	if pac.Data["playable"] != true || pac.Data["build"] != "pacman_pacman" || pac.Data["dir"] != "roms/" || pac.Data["ext"] != ".zip" {
		t.Errorf("pacman data: %v", pac.Data)
	}
	if !slices.Equal(pac.Genres, []string{"Maze"}) {
		t.Errorf("pacman genres %v", pac.Genres)
	}
	if filepath.Base(pac.Poster) != "pacman.jpg" || filepath.Base(pac.Backdrop) != "puckman.png" || pac.Data["art"] != "local" {
		t.Errorf("pacman art %q %q", pac.Poster, pac.Backdrop)
	}
	if !slices.Contains(pac.Facts, plugin.Fact{Label: "Screen", Value: "Vertical"}) || !slices.Contains(pac.Facts, plugin.Fact{Label: "Controls", Value: "Joystick (4-way)"}) {
		t.Errorf("pacman facts %v", pac.Facts)
	}

	gal := idx("roms/galaga.zip")
	if gal.Data["playable"] != false || gal.Data["art"] != "online" {
		t.Errorf("galaga data: %v", gal.Data)
	}
	if !slices.Contains(gal.Facts, plugin.Fact{Label: "Plays in the browser", Value: "No, not in this version of the plugin"}) {
		t.Errorf("galaga facts %v", gal.Facts)
	}

	ki := idx("kinst.zip")
	if ki.Data["playable"] != false || !slices.Equal(ki.Genres, []string{"Fighter"}) {
		t.Errorf("kinst: %v %v", ki.Data, ki.Genres)
	}

	if it := idx("roms/neogeo.zip"); it != nil {
		t.Errorf("the BIOS was filed: %+v", it)
	}
	if it := idx("artwork/pacman.zip"); it != nil {
		t.Errorf("artwork was filed as a game: %+v", it)
	}
}

func TestThumbnailName(t *testing.T) {
	if got := thumbnailName(`Mr. Do!/Do's Castle: "Deluxe"`); got != `Mr. Do!_Do's Castle_ _Deluxe_` {
		t.Errorf("got %q", got)
	}
}
