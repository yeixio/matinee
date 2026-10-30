package scan

import (
	"context"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/yeixio/matinee/internal/probe"
	"github.com/yeixio/matinee/internal/store"
	"github.com/yeixio/matinee/internal/store/storetest"
)

// A soundtrack or compilation whose files name no album artist is one
// album, not one per track artist: under the artist its folder is named
// for when that's one of them, under Various Artists otherwise. Each track
// still names its own artist, and later scans keep the album as it is.
func TestScanKeepsSoundtracksTogether(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe not installed")
	}
	root := t.TempDir()
	song := func(rel, artist, album string) string {
		p := filepath.Join(root, rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if out, err := exec.Command(ffmpeg, "-loglevel", "error", "-y", "-f", "lavfi", "-i", "sine=d=1",
			"-metadata", "artist="+artist, "-metadata", "album="+album, p).CombinedOutput(); err != nil {
			t.Fatalf("make song: %v\n%s", err, out)
		}
		return p
	}
	song("Paul Oakenfold/Swordfish (2001)/01 Intro.mp3", "Paul Oakenfold", "Swordfish")
	song("Paul Oakenfold/Swordfish (2001)/02 Ready Steady Go.mp3", "Dope Smugglaz", "Swordfish")
	song("Paul Oakenfold/Swordfish (2001)/03 Plastic.mp3", "Muse", "Swordfish")
	muse := song("Paul Oakenfold/Swordfish (2001)/04 New Born.mp3", "Muse", "Swordfish")
	song("Compilations/Summer Hits/01 One.mp3", "Someone", "Summer Hits")
	song("Compilations/Summer Hits/CD 2/01 Two.mp3", "Someone Else", "Summer Hits")
	song("Muse/Origin/01 Origin.mp3", "Muse", "Origin")

	st := storetest.Open(t)
	ctx := context.Background()
	lib, _ := st.CreateLibrary(ctx, "Music", "music", []string{root})
	s := &Scanner{Store: st, Prober: probe.Prober{Path: ffprobe}, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	if _, err := s.Scan(ctx, lib); err != nil {
		t.Fatal(err)
	}

	type album struct {
		id     int64
		tracks []store.Item
	}
	library := func() map[string]map[string]album {
		out := map[string]map[string]album{}
		artists, _ := st.TopLevel(ctx, 0, lib.ID)
		for _, ar := range artists {
			out[ar.Title] = map[string]album{}
			kids, _ := st.Children(ctx, 0, ar.ID)
			for _, al := range kids {
				tracks, _ := st.Tracks(ctx, 0, al.ID)
				out[ar.Title][al.Title] = album{al.ID, tracks}
			}
		}
		return out
	}
	names := func(m map[string]map[string]album) []string {
		var out []string
		for ar, als := range m {
			for al, a := range als {
				out = append(out, ar+"/"+al+"/"+string(rune('0'+len(a.tracks))))
			}
		}
		sort.Strings(out)
		return out
	}
	got := library()
	want := []string{"Muse/Origin/1", "Paul Oakenfold/Swordfish/4", "Various Artists/Summer Hits/2"}
	if n := names(got); len(n) != len(want) || n[0] != want[0] || n[1] != want[1] || n[2] != want[2] {
		t.Fatalf("albums: %v", n)
	}
	sword := got["Paul Oakenfold"]["Swordfish"]
	byTitle := map[string]store.Item{}
	for _, tr := range sword.tracks {
		byTitle[tr.Title] = tr
	}
	if a := byTitle["Intro"]; a.Artist != "Paul Oakenfold" {
		t.Errorf("the album artist's own track: %+v", a)
	}
	if a := byTitle["Ready Steady Go"]; a.Artist != "Dope Smugglaz" || a.ArtistID != nil || a.Album != "Swordfish" {
		t.Errorf("a guest track: %+v", a)
	}
	if a := byTitle["Plastic"]; a.Artist != "Muse" || a.ArtistID == nil {
		t.Errorf("a track by an artist with a page: %+v", a)
	}

	// The file changes; scanning it again files it under Muse for a moment,
	// and the scan puts it back on the same album.
	later := time.Now().Add(time.Minute)
	os.Chtimes(muse, later, later)
	if stats, err := s.Scan(ctx, lib); err != nil || stats.Updated != 1 {
		t.Fatalf("rescan: %+v %v", stats, err)
	}
	again := library()
	if n := names(again); len(n) != 3 || again["Paul Oakenfold"]["Swordfish"].id != sword.id {
		t.Fatalf("after a rescan: %v", n)
	}
}
