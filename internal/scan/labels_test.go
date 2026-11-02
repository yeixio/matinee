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

func TestAlbumLabel(t *testing.T) {
	for _, c := range []struct {
		tags map[string]string
		want string
	}{
		{map[string]string{"label": "Anjunadeep"}, "Anjunadeep"},
		{map[string]string{"publisher": " Anjunabeats "}, "Anjunabeats"},
		{map[string]string{"organization": "Anjunadeep; Armada"}, "Anjunadeep"},
		{map[string]string{"label": "[no label]"}, ""},
		{map[string]string{"label": "Not On Label"}, ""},
		{map[string]string{"artist": "Above & Beyond"}, ""},
	} {
		if got := albumLabel(c.tags); got != c.want {
			t.Errorf("%v: %q, want %q", c.tags, got, c.want)
		}
	}
}

// A scan files each album under the label its songs' tags name, from ID3's
// publisher and Vorbis comments alike.
func TestScanReadsLabels(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe not installed")
	}
	root := t.TempDir()
	song := func(rel string, tags ...string) {
		p := filepath.Join(root, rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		args := []string{"-loglevel", "error", "-f", "lavfi", "-i", "sine=d=1"}
		for _, tg := range tags {
			args = append(args, "-metadata", tg)
		}
		if out, err := exec.Command(ffmpeg, append(args, p)...).CombinedOutput(); err != nil {
			t.Fatalf("make song: %v\n%s", err, out)
		}
	}
	song("Lane 8/Little by Little/01 A.mp3", "album=Little by Little", "publisher=Anjunadeep")
	song("Above & Beyond/Common Ground/01 B.flac", "album=Common Ground", "LABEL=Anjunabeats")
	song("Someone/Demo/01 C.mp3", "album=Demo")

	st := storetest.Open(t)
	ctx := context.Background()
	lib, _ := st.CreateLibrary(ctx, "Music", "music", []string{root})
	s := &Scanner{Store: st, Prober: probe.Prober{Path: ffprobe}, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	if _, err := s.Scan(ctx, lib); err != nil {
		t.Fatal(err)
	}
	labels, err := st.Labels(ctx, 0, lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(labels) != 2 || labels[0].Name != "Anjunabeats" || labels[1].Name != "Anjunadeep" {
		t.Fatalf("labels: %+v", labels)
	}
	if left, _ := st.AlbumsToReadLabel(ctx, lib.ID); len(left) != 0 {
		t.Fatalf("albums left to read: %+v", left)
	}
}
