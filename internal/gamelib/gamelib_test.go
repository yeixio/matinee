package gamelib

import (
	"archive/zip"
	"context"
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
	k := &Kind{Dir: t.TempDir(), LibretroDB: srv.URL, Art: "https://art.example"}
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

	// Arcade sets are left to the Arcade library for now.
	z = filepath.Join(root, "mame", "pacman.zip")
	writeZip(t, z, map[string][]byte{"pacman.6e": []byte("rom")})
	if _, err = k.Index(ctx, nil, file(t, root, z)); err != scan.ErrSkip {
		t.Fatalf("arcade set: %v", err)
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
