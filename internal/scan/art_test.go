package scan

import (
	"context"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/yeixio/matinee/internal/probe"
	"github.com/yeixio/matinee/internal/store/storetest"
)

func TestFindCover(t *testing.T) {
	for _, c := range []struct {
		name  string
		files []string
		want  string
	}{
		{"usual name in any case", []string{"Folder.JPG", "back.jpg"}, "Folder.JPG"},
		{"cover beats folder", []string{"folder.jpg", "Cover.png"}, "Cover.png"},
		{"jpeg and webp", []string{"front.jpeg"}, "front.jpeg"},
		{"named like a cover", []string{"Abbey Road - Front Cover.jpg", "Abbey Road - Back Cover.jpg"}, "Abbey Road - Front Cover.jpg"},
		{"large album art over small", []string{"AlbumArtSmall.jpg", "AlbumArt_{1234}_Large.jpg"}, "AlbumArt_{1234}_Large.jpg"},
		{"the only image", []string{"scan001.jpg"}, "scan001.jpg"},
		{"not a back cover or disc", []string{"back.jpg", "cd.jpg"}, ""},
		{"several unnamed images", []string{"a.jpg", "b.jpg"}, ""},
		{"not an image", []string{"cover.txt"}, ""},
	} {
		dir := t.TempDir()
		for _, f := range c.files {
			os.WriteFile(filepath.Join(dir, f), []byte("x"), 0o644)
		}
		got := findCover(dir)
		if c.want != "" {
			c.want = filepath.Join(dir, c.want)
		}
		if got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

// Covers turn up in any case, in the album folder above CD folders, and
// when added after the tracks were first scanned.
func TestScanFindsAlbumCovers(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe not installed")
	}
	root := t.TempDir()
	mk := func(rel string) string {
		p := filepath.Join(root, rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		return p
	}
	for _, rel := range []string{"Band/Double/CD 1/01 A.mp3", "Band/Double/CD 2/01 B.mp3", "Band/Later/01 C.mp3"} {
		if out, err := exec.Command(ffmpeg, "-loglevel", "error", "-f", "lavfi", "-i", "sine=d=1", mk(rel)).CombinedOutput(); err != nil {
			t.Fatalf("make song: %v\n%s", err, out)
		}
	}
	os.WriteFile(mk("Band/Double/Folder.JPG"), []byte("x"), 0o644)

	st := storetest.Open(t)
	ctx := context.Background()
	lib, _ := st.CreateLibrary(ctx, "Music", "music", []string{root})
	s := &Scanner{Store: st, Prober: probe.Prober{Path: ffprobe}, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	if _, err := s.Scan(ctx, lib); err != nil {
		t.Fatal(err)
	}
	albums := map[string]string{}
	artists, _ := st.TopLevel(ctx, 0, lib.ID)
	if len(artists) != 1 {
		t.Fatalf("artists: %+v", artists)
	}
	list := func() {
		kids, _ := st.Children(ctx, 0, artists[0].ID)
		for _, a := range kids {
			albums[a.Title] = a.Poster
		}
	}
	list()
	if len(albums) != 2 || albums["Double"] != filepath.Join(root, "Band/Double/Folder.JPG") || albums["Later"] != "" {
		t.Fatalf("albums after the first scan: %+v", albums)
	}

	os.WriteFile(mk("Band/Later/cover.png"), []byte("x"), 0o644)
	if stats, err := s.Scan(ctx, lib); err != nil || stats.Unchanged != 3 {
		t.Fatalf("rescan: %+v %v", stats, err)
	}
	list()
	if albums["Later"] != filepath.Join(root, "Band/Later/cover.png") {
		t.Fatalf("a cover added later: %+v", albums)
	}
}
