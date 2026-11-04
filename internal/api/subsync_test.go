package api

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yeixio/matinee/internal/store"
	"github.com/yeixio/matinee/internal/subtitle"
)

func TestSubtitleSync(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	if exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "flite=text=hi", "-f", "null", "-").Run() != nil {
		t.Skip("no flite voice in this FFmpeg")
	}
	c, tmp := newTestServer(t)
	var login struct{ Token string }
	c.do("POST", "/api/setup", credentials{"admin", "correct horse"}, &login)
	c.token = login.Token

	// Three minutes of a voice saying lines at uneven moments, and a
	// subtitle file for it that shows every line 2.5 s late.
	words := []string{"Where were you last night", "I was at the station", "That is not what he said",
		"We need to leave now", "Get the car", "Do you remember the lake", "Nobody lives there",
		"Hand me the map", "This road goes nowhere", "Listen to me", "The train leaves at nine", "Sit down and eat"}
	args := []string{"-f", "lavfi", "-i", "color=c=black:duration=180:size=160x90:rate=12"}
	var mix, srt strings.Builder
	at, n := 1.5, 0
	for at < 170 {
		args = append(args, "-f", "lavfi", "-i", "flite=text='"+words[n%len(words)]+"'")
		fmt.Fprintf(&mix, "[%d:a]aresample=16000,adelay=%d:all=1[a%d];", n+1, int(at*1000), n)
		start := at + 2.5
		fmt.Fprintf(&srt, "%d\n%s --> %s\n%s\n\n", n+1, srtClock(start), srtClock(start+1.6), words[n%len(words)])
		n++
		at += 3 + float64((n*7919)%11)
	}
	for i := range n {
		fmt.Fprintf(&mix, "[a%d]", i)
	}
	fmt.Fprintf(&mix, "amix=inputs=%d:normalize=0:duration=longest,apad=whole_dur=180[out]", n)
	dir := filepath.Join(tmp, "movies", "Talk (2020)")
	movie := filepath.Join(dir, "Talk (2020).mkv")
	args = append(args, "-filter_complex", mix.String(), "-map", "0:v", "-map", "[out]", "-t", "180",
		"-c:v", "libx264", "-preset", "ultrafast", "-c:a", "aac")
	makeMedia(t, movie, args...)
	os.WriteFile(filepath.Join(dir, "Talk (2020).en.srt"), []byte(srt.String()), 0o644)

	var lib store.Library
	c.do("POST", "/api/libraries", map[string]any{"name": "Movies", "kind": "movies", "paths": []string{filepath.Join(tmp, "movies")}}, &lib)
	waitForScan(t, c)
	var items []store.Item
	c.do("GET", "/api/libraries/"+itoa(lib.ID)+"/items", nil, &items)
	if len(items) != 1 {
		t.Fatalf("items: %+v", items)
	}
	base := "/api/items/" + itoa(items[0].ID)
	var item itemView
	c.do("GET", base, nil, &item)
	if len(item.Subtitles) != 1 || item.Subtitles[0].Sync != nil {
		t.Fatalf("tracks: %+v", item.Subtitles)
	}
	en := item.Subtitles[0]
	first := "00:04.000 --> 00:05.600"

	wait := func() subtitle.Sync {
		t.Helper()
		deadline := time.Now().Add(90 * time.Second)
		for {
			var s subtitle.Sync
			c.do("GET", base+"/subtitles/"+en.ID+"/sync", nil, &s)
			if s.State != "syncing" || time.Now().After(deadline) {
				return s
			}
			time.Sleep(200 * time.Millisecond)
		}
	}

	// Playing with the file syncs it on its own; until then it's as written.
	if vtt := get(t, c, base+"/subtitles/"+en.ID+".vtt"); !strings.Contains(vtt, first) {
		t.Fatalf("before:\n%s", vtt[:200])
	}
	s := wait()
	if s.State != "synced" || !s.Auto || s.Reference != "audio" || s.OffsetMs > -2300 || s.OffsetMs < -2700 {
		t.Fatalf("auto sync: %+v", s)
	}
	if vtt := get(t, c, base+"/subtitles/"+en.ID+".vtt"); strings.Contains(vtt, first) || !strings.Contains(vtt, "00:00:01.") {
		t.Fatalf("after:\n%s", vtt[:200])
	}
	c.do("GET", base, nil, &item)
	if tr := item.Subtitles[0]; tr.Sync == nil || tr.Sync.State != "synced" || tr.Sync.OffsetMs != s.OffsetMs {
		t.Fatalf("item track: %+v", tr)
	}

	// Undoing it shows the file as written, and it isn't synced again on its own.
	if code := c.do("DELETE", base+"/subtitles/"+en.ID+"/sync", nil, &s); code != 200 || s.State != "off" {
		t.Fatalf("undo: %d %+v", code, s)
	}
	get(t, c, base+"/subtitles/"+en.ID+".vtt")
	if s = wait(); s.State != "off" {
		t.Fatalf("after undo: %+v", s)
	}
	if vtt := get(t, c, base+"/subtitles/"+en.ID+".vtt"); !strings.Contains(vtt, first) {
		t.Fatalf("undone:\n%s", vtt[:200])
	}

	// Asking for it syncs it again, from the speech map kept last time.
	if code := c.do("POST", base+"/subtitles/"+en.ID+"/sync", nil, &s); code != 202 || s.State != "syncing" {
		t.Fatalf("sync: %d %+v", code, s)
	}
	if s = wait(); s.State != "synced" || s.Auto {
		t.Fatalf("manual sync: %+v", s)
	}
}

func srtClock(sec float64) string {
	ms := int(sec * 1000)
	return fmt.Sprintf("%02d:%02d:%02d,%03d", ms/3600000, ms/60000%60, ms/1000%60, ms%1000)
}
