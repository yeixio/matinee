package lyrics

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/yeixio/matinee/internal/probe"
)

func times(l Lyrics) []int64 {
	var out []int64
	for _, x := range l.Lines {
		if x.Ms == nil {
			out = append(out, -1)
		} else {
			out = append(out, *x.Ms)
		}
	}
	return out
}

func TestParseSynced(t *testing.T) {
	l := Parse("\ufeff[ar:Someone]\r\n[ti:A song]\r\n[offset:+500]\r\n[00:12.34]First <00:12.50>line\r\n[00:05.1][00:30.00]Chorus\r\n\r\n[01:02:03]Odd\r\n[00:40.120]\r\n[00:45.00]Last\r\nnot timed\r\n")
	if !l.Synced {
		t.Fatal("not synced")
	}
	want := []struct {
		ms   int64
		text string
	}{{4600, "Chorus"}, {11840, "First line"}, {29500, "Chorus"}, {39620, ""}, {44500, "Last"}, {61530, "Odd"}}
	if len(l.Lines) != len(want) {
		t.Fatalf("lines: %+v", times(l))
	}
	for i, w := range want {
		if *l.Lines[i].Ms != w.ms || l.Lines[i].Text != w.text {
			t.Errorf("line %d: %d %q, want %d %q", i, *l.Lines[i].Ms, l.Lines[i].Text, w.ms, w.text)
		}
	}
	if l := Parse("[1:02:03.5]Long mix"); *l.Lines[0].Ms != (3723*1000 + 500) {
		t.Errorf("h:mm:ss: %d", *l.Lines[0].Ms)
	}
}

func TestParsePlain(t *testing.T) {
	l := Parse("\n\nOne\nTwo\n\n\n\nThree\n\n")
	if l.Synced {
		t.Fatal("synced")
	}
	var got []string
	for _, x := range l.Lines {
		if x.Ms != nil {
			t.Fatal("a time on plain words")
		}
		got = append(got, x.Text)
	}
	if want := []string{"One", "Two", "", "Three"}; !equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	if l := Parse("[ar:Only tags]\n\n"); !l.Empty() || l.Lines == nil {
		t.Fatalf("only tags: %+v", l)
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestEmbedded(t *testing.T) {
	l := Embedded(map[string]string{"title": "x", "lyrics-eng": "Plain words", "unsyncedlyrics": "[00:01.00]Timed"})
	if !l.Synced || l.Source != "embedded" || l.Lines[0].Text != "Timed" {
		t.Fatalf("%+v", l)
	}
	if l := Embedded(map[string]string{"comment": "[00:01.00]no"}); !l.Empty() || l.Source != "" {
		t.Fatalf("comment read as lyrics: %+v", l)
	}
}

// song makes a short song with ffmpeg, with these tags.
func song(t *testing.T, dir string, tags map[string]string) string {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("no ffmpeg")
	}
	path := filepath.Join(dir, "01 Song.mp3")
	args := []string{"-v", "error", "-y", "-f", "lavfi", "-i", "sine=frequency=440:duration=3", "-id3v2_version", "3"}
	for k, v := range tags {
		args = append(args, "-metadata", k+"="+v)
	}
	if out, err := exec.Command("ffmpeg", append(args, path)...).CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg: %v %s", err, out)
	}
	return path
}

func lrclib(t *testing.T, handler http.HandlerFunc) (*LRCLIB, *atomic.Int32) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("User-Agent") != "Matinee test" {
			t.Errorf("user agent %q", r.Header.Get("User-Agent"))
		}
		handler(w, r)
	}))
	t.Cleanup(srv.Close)
	return &LRCLIB{Base: srv.URL, UserAgent: "Matinee test", Dir: t.TempDir()}, &calls
}

func TestFind(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := song(t, dir, map[string]string{"artist": "The Band", "title": "The Song", "album": "The Album", "lyrics": "Words in the tags"})
	online, calls := lrclib(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if r.URL.Path == "/get" && q.Get("artist_name") == "The Band" && q.Get("track_name") == "The Song" && q.Get("album_name") == "The Album" && q.Get("duration") == "3" {
			json.NewEncoder(w).Encode(lrclibTrack{SyncedLyrics: "[00:01.00]From online"})
			return
		}
		http.NotFound(w, r)
	})
	f := &Finder{Prober: probe.Prober{Path: "ffprobe"}, Online: online}
	s := Song{Path: path, Artist: "Band", Title: "Song"}

	// The tags' words, untimed; online is off.
	l, err := f.Find(ctx, s, false)
	if err != nil || l.Source != "embedded" || l.Synced || l.Lines[0].Text != "Words in the tags" || calls.Load() != 0 {
		t.Fatalf("embedded: %+v %v (%d calls)", l, err, calls.Load())
	}
	// Online has timed words, which win; the second time comes from the cache.
	for range 2 {
		l, err = f.Find(ctx, s, true)
		if err != nil || l.Source != "online" || !l.Synced || l.Lines[0].Text != "From online" {
			t.Fatalf("online: %+v %v", l, err)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("%d calls to LRCLIB, want 1", calls.Load())
	}
	// A timed .lrc next to the song wins over everything, and LRCLIB isn't asked.
	if err := os.WriteFile(filepath.Join(dir, "01 Song.lrc"), []byte("[00:00.50]From the file"), 0o644); err != nil {
		t.Fatal(err)
	}
	l, err = f.Find(ctx, s, true)
	if err != nil || l.Source != "file" || !l.Synced || l.Lines[0].Text != "From the file" || calls.Load() != 1 {
		t.Fatalf("file: %+v %v", l, err)
	}
}

func TestFindNothing(t *testing.T) {
	ctx := context.Background()
	path := song(t, t.TempDir(), map[string]string{"artist": "Nobody", "title": "Nothing"})
	online, calls := lrclib(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/search" {
			// Another recording, much longer, and one without words.
			json.NewEncoder(w).Encode([]lrclibTrack{{Duration: 300, PlainLyrics: "Other"}, {Duration: 3}})
			return
		}
		http.NotFound(w, r)
	})
	f := &Finder{Prober: probe.Prober{Path: "ffprobe"}, Online: online}
	for range 2 {
		l, err := f.Find(ctx, Song{Path: path}, true)
		if err != nil || !l.Empty() || l.Lines == nil || l.Source != "" {
			t.Fatalf("%+v %v", l, err)
		}
	}
	// No album: straight to search; and "not there" is remembered.
	if calls.Load() != 1 {
		t.Fatalf("%d calls", calls.Load())
	}
}

func TestFindOnlineDown(t *testing.T) {
	ctx := context.Background()
	path := song(t, t.TempDir(), map[string]string{"artist": "A", "title": "B", "lyrics": "Local"})
	online, _ := lrclib(t, func(w http.ResponseWriter, r *http.Request) { http.Error(w, "down", 502) })
	f := &Finder{Prober: probe.Prober{Path: "ffprobe"}, Online: online}
	if l, err := f.Find(ctx, Song{Path: path}, true); err != nil || l.Source != "embedded" {
		t.Fatalf("with local words: %+v %v", l, err)
	}
	path = song(t, t.TempDir(), map[string]string{"artist": "A", "title": "B"})
	if _, err := f.Find(ctx, Song{Path: path}, true); err == nil {
		t.Fatal("no error with LRCLIB down and nothing local")
	}
}
