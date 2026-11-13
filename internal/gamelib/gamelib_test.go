package gamelib

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/yeixio/matinee/internal/scan"
	"github.com/yeixio/matinee/internal/store"
)

func write(t *testing.T, p string, data []byte) {
	t.Helper()
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeZip(t *testing.T, p string, files map[string][]byte) {
	t.Helper()
	os.MkdirAll(filepath.Dir(p), 0o755)
	f, _ := os.Create(p)
	zw := zip.NewWriter(f)
	for name, data := range files {
		w, _ := zw.Create(name)
		w.Write(data)
	}
	zw.Close()
	f.Close()
}

func file(t *testing.T, root, p string) scan.File {
	t.Helper()
	st, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	return scan.File{Library: store.Library{ID: 1, Kind: KindName}, Root: root, Path: p, Info: st}
}

func fact(it store.Item, label string) string {
	for _, f := range it.Facts {
		if f.Label == label {
			return f.Value
		}
	}
	return ""
}

func TestIndex(t *testing.T) {
	gb := []byte("a game boy game, as it is")
	crc := crc32.ChecksumIEEE(gb)
	var asked atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked.Add(1)
		switch r.URL.Path {
		case "/metadat/no-intro/Nintendo - Game Boy.dat":
			fmt.Fprintf(w, "clrmamepro (\n\tname \"Nintendo - Game Boy\"\n)\n\ngame (\n\tname \"Tetris (World) (Rev 1)\"\n\trom ( name \"Tetris (World) (Rev 1).gb\" size %d crc %08X )\n)\n", len(gb), crc)
		case "/metadat/releaseyear/Nintendo - Game Boy.dat":
			fmt.Fprintf(w, "game (\n\tcomment \"Tetris\"\n\treleaseyear \"1989\"\n\trom ( crc %08X )\n)\n", crc)
		case "/metadat/maxusers/Nintendo - Game Boy.dat":
			fmt.Fprintf(w, "game (\n\tcomment \"Tetris\"\n\tusers \"2\"\n\trom ( crc %08X )\n)\n", crc)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	k := &Kind{Dir: t.TempDir(), LibretroDB: srv.URL, Art: "https://art.example", MAMEList: mameList(t)}
	root := t.TempDir()
	ctx := context.Background()

	// A loose file, named anything, in a folder named for its system:
	// matched by its contents.
	p := filepath.Join(root, "Game Boy", "tetris copy.gb")
	write(t, p, gb)
	it, err := k.Index(ctx, nil, file(t, root, p))
	if err != nil {
		t.Fatal(err)
	}
	if it.Kind != ItemKind || it.Title != "Tetris" || it.Year == nil || *it.Year != 1989 {
		t.Fatalf("item = %+v", it)
	}
	if fact(it, FactSystem) != "Game Boy" || fact(it, FactRegion) != "World · Rev 1" || fact(it, FactPlayers) != "2" {
		t.Fatalf("facts = %+v", it.Facts)
	}
	if it.Poster != "https://art.example/Nintendo%20-%20Game%20Boy/Named_Boxarts/Tetris%20%28World%29%20%28Rev%201%29.png" {
		t.Fatalf("poster = %s", it.Poster)
	}

	// The same game zipped, in a folder with no system's name: told by
	// what's in the zip, matched by the CRC the zip lists. The lists
	// aren't fetched again.
	before := asked.Load()
	z := filepath.Join(root, "misc", "t.zip")
	writeZip(t, z, map[string][]byte{"readme.txt": []byte("hi"), "whatever.gb": gb})
	it, err = k.Index(ctx, nil, file(t, root, z))
	if err != nil || it.Title != "Tetris" {
		t.Fatalf("zip: %+v, %v", it, err)
	}
	if asked.Load() != before {
		t.Fatal("fetched the lists again")
	}

	// Not on the list: named after its file.
	p = filepath.Join(root, "gb", "Homebrew Thing (PD).gb")
	write(t, p, []byte("something else"))
	if it, err = k.Index(ctx, nil, file(t, root, p)); err != nil || it.Title != "Homebrew Thing" || it.Year != nil {
		t.Fatalf("unknown: %+v, %v", it, err)
	}

	// A system with no list at all (a 404): by its file's name.
	p = filepath.Join(root, "snes", "Super Game (USA).sfc")
	write(t, p, []byte("snes"))
	if it, err = k.Index(ctx, nil, file(t, root, p)); err != nil || it.Title != "Super Game" || fact(it, FactSystem) != "Super Nintendo" {
		t.Fatalf("no list: %+v, %v", it, err)
	}
}

// Files left out say why, so admins can see it.
func TestSkipReasons(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	k := &Kind{Dir: t.TempDir(), MAMEList: mameList(t), Art: "https://art.example"}
	cases := []struct{ path, reason, group string }{
		{filepath.Join(root, "mame", "notaset.zip"), SkipUnknown, "mame"},
		{filepath.Join(root, "odd", "stuff.zip"), SkipUnknown, "odd"},
		{filepath.Join(root, "stray.zip"), SkipUnknown, ""},
	}
	for _, c := range cases {
		writeZip(t, c.path, map[string][]byte{"rom.u1": []byte("rom")})
		_, err := k.Index(ctx, nil, file(t, root, c.path))
		if !errors.Is(err, scan.ErrSkip) {
			t.Fatalf("%s: %v", c.path, err)
		}
		if reason, group := scan.SkipOf(err); reason != c.reason || group != c.group {
			t.Fatalf("%s: %q, %q", c.path, reason, group)
		}
	}
}

func TestDiscs(t *testing.T) {
	k := &Kind{Art: "https://art.example"}
	root := t.TempDir()
	dir := filepath.Join(root, "PlayStation", "Big Game (USA)")
	write(t, filepath.Join(dir, "Big Game (USA) (Disc 1).cue"), []byte("FILE"))
	write(t, filepath.Join(dir, "Big Game (USA) (Disc 2).cue"), []byte("FILE"))
	write(t, filepath.Join(dir, "Big Game (USA).m3u"), []byte("Big Game (USA) (Disc 1).cue\n"))
	ctx := context.Background()
	if _, err := k.Index(ctx, nil, file(t, root, filepath.Join(dir, "Big Game (USA) (Disc 1).cue"))); err != scan.ErrSkip {
		t.Fatalf("a disc listed in an .m3u: %v", err)
	}
	it, err := k.Index(ctx, nil, file(t, root, filepath.Join(dir, "Big Game (USA).m3u")))
	if err != nil || it.Title != "Big Game" || fact(it, FactSystem) != "PlayStation" {
		t.Fatalf("m3u: %+v, %v", it, err)
	}
	if !strings.Contains(it.Poster, "Sony%20-%20PlayStation/Named_Boxarts/Big%20Game%20%28USA%29.png") {
		t.Fatalf("poster = %s", it.Poster)
	}
	if k.Wants(filepath.Join(dir, "Big Game (USA) (Track 1).bin")) {
		t.Fatal("wants a disc's track")
	}
}

// mameList serves a MAME list of a few machines, as MAME publishes it,
// and returns its address for Kind.MAMEList.
func mameList(t *testing.T) string {
	t.Helper()
	xml := `<?xml version="1.0"?>
<mame build="0.289 (mame0289)">
	<machine name="puckman" sourcefile="pacman/pacman.cpp">
		<description>Puck Man (Japan set 1)</description><year>1980</year><manufacturer>Namco</manufacturer>
		<rom name="pm1_prg1.6e" size="2048" crc="f36e88ab"/>
		<display rotate="90"/><input players="2"><control type="joy" ways="4"/></input>
		<driver status="good" savestate="supported"/>
	</machine>
	<machine name="pacman" cloneof="puckman" romof="puckman" sourcefile="pacman/pacman.cpp">
		<description>Pac-Man (Midway)</description><year>1980</year><manufacturer>Namco (Midway license)</manufacturer>
		<rom name="pacman.6e" size="4096" crc="c1e6ab10"/>
		<display rotate="90"/><input players="2"/><driver status="good"/>
	</machine>
	<machine name="esprade"><description>ESP Ra.De. (World)</description><year>1998</year><manufacturer>Atlus / Cave</manufacturer>
		<rom name="u42.int" size="524288" crc="3b510a73"/></machine>
	<machine name="neogeo" isbios="yes"><description>Neo-Geo</description><rom name="sp-s2.sp1" size="131072" crc="9036d879"/></machine>
	<machine name="z80" isdevice="yes"><description>Zilog Z80</description></machine>
</mame>`
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("mame0289.xml")
	w.Write([]byte(xml))
	zw.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/mame0289/mame0289lx.zip" {
			http.NotFound(w, r)
			return
		}
		w.Write(buf.Bytes())
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/mame%[1]s/mame%[1]slx.zip"
}

func TestArcade(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	k := &Kind{Dir: t.TempDir(), MAMEList: mameList(t), Art: "https://art.example"}
	index := func(dir, name string) (store.Item, error) {
		p := filepath.Join(root, dir, name+".zip")
		writeZip(t, p, map[string][]byte{"rom.6e": []byte("rom")})
		return k.Index(ctx, nil, file(t, root, p))
	}
	it, err := index("mame", "pacman")
	if err != nil {
		t.Fatal(err)
	}
	if it.Title != "Pac-Man" || it.Year == nil || *it.Year != 1980 || fact(it, FactPublisher) != "Namco (Midway license)" ||
		fact(it, FactPlayers) != "2" || fact(it, FactScreen) != "Vertical" || fact(it, FactSystem) != "Arcade (MAME)" {
		t.Fatalf("pacman: %+v", it)
	}
	var d gameData
	if err := json.Unmarshal(it.PluginData, &d); err != nil || d.Group != "mame|puckman" || d.Edition != "Midway" {
		t.Fatalf("pacman's group: %s", it.PluginData)
	}
	if !strings.Contains(it.Poster, "/MAME/") {
		t.Fatalf("pacman's poster: %q", it.Poster)
	}
	// A BIOS is used by games but isn't one, and isn't counted as left out.
	if _, err := index("mame", "neogeo"); !errors.Is(err, scan.ErrSkip) {
		t.Fatalf("bios: %v", err)
	} else if reason, _ := scan.SkipOf(err); reason != "" {
		t.Fatalf("bios counted: %q", reason)
	}
	// An arcade set filed in a console's folder is still an arcade game.
	if it, err := index("c64", "esprade"); err != nil || it.Title != "ESP Ra.De." || fact(it, FactSystem) != "Arcade (MAME)" {
		t.Fatalf("esprade in c64: %+v, %v", it, err)
	}
	// Kept: another Kind reads it without downloading it again.
	k2 := &Kind{Dir: k.Dir, MAMEList: "http://127.0.0.1:1/%s"}
	if l := k2.arcade(ctx); l == nil || l.Games["puckman"] == nil || l.Games["pacman"].CloneOf != "puckman" || !l.other("neogeo") {
		t.Fatalf("kept list: %+v", l)
	}
}

// A game's versions show as one: an arcade game's clones under its parent
// set, a console game's regions under the USA or World release.
func TestFinishScanGroupsVersions(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "matinee.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	lib, err := st.CreateLibrary(ctx, "Games", KindName, []string{"/games"})
	if err != nil {
		t.Fatal(err)
	}
	add := func(path, title, group, edition string) int64 {
		id, err := st.UpsertFile(ctx, store.Item{LibraryID: lib.ID, Kind: ItemKind, Title: title, Path: path, Playable: true,
			PluginData: groupData(group, edition)})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	pacman := add("/games/mame/pacman.zip", "Pac-Man", "mame|puckman", "Midway")
	puckman := add("/games/mame/puckman.zip", "Puck Man", "mame|puckman", "Japan set 1")
	eu := add("/games/snes/Super Game (Europe).sfc", "Super Game", "snes|supergame", "Europe")
	us := add("/games/snes/Super Game (USA).sfc", "Super Game", "snes|supergame", "USA")
	alone := add("/games/gb/Tetris.gb", "Tetris", "gb|tetris", "")
	k := &Kind{}
	for range 2 { // and again, changing nothing
		if err := k.FinishScan(ctx, st, lib); err != nil {
			t.Fatal(err)
		}
	}
	got := map[int64]int64{}
	editions := map[int64]string{}
	vs, err := st.GameVersions(ctx, lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range vs {
		got[v.ID], editions[v.ID] = v.VersionOf, v.OldEdition
	}
	want := map[int64]int64{pacman: puckman, puckman: 0, eu: us, us: 0, alone: 0}
	for id, of := range want {
		if got[id] != of {
			t.Errorf("item %d is a version of %d, want %d", id, got[id], of)
		}
	}
	if editions[pacman] != "Midway" || editions[eu] != "Europe" {
		t.Errorf("editions: %v", editions)
	}
}
