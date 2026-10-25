package api

import (
	"bytes"
	"encoding/binary"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yeixio/matinee/internal/store"
	"github.com/yeixio/matinee/internal/subtitle"
)

func TestSubtitles(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	c, tmp := newTestServer(t)
	var login struct{ Token string }
	c.do("POST", "/api/setup", credentials{"admin", "correct horse"}, &login)
	c.token = login.Token

	// A black H.264 movie with an embedded Spanish SRT track and a German PGS
	// track (a white box from 1s to 9s), plus an English sidecar in Windows-1252.
	src := t.TempDir()
	srt := filepath.Join(src, "es.srt")
	os.WriteFile(srt, []byte("1\r\n00:00:01,000 --> 00:00:04,000\r\nHola <i>mundo</i>\r\n\r\n"), 0o644)
	sup := filepath.Join(src, "de.sup")
	os.WriteFile(sup, pgsBox(320, 240, 100, 180, 120, 40, 1, 9), 0o644)
	dir := filepath.Join(tmp, "movies", "Sub Test (2020)")
	movie := filepath.Join(dir, "Sub Test (2020).mkv")
	makeMedia(t, movie,
		"-f", "lavfi", "-i", "color=c=black:duration=10:size=320x240:rate=24", "-i", srt, "-i", sup,
		"-map", "0:v", "-map", "1:s", "-map", "2:s", "-c:v", "libx264", "-preset", "ultrafast", "-c:s:0", "srt", "-c:s:1", "copy",
		"-metadata:s:s:0", "language=spa", "-metadata:s:s:1", "language=ger", "-disposition:s:1", "forced")
	os.WriteFile(filepath.Join(dir, "Sub Test (2020).en.srt"), []byte("1\n00:00:02,000 --> 00:00:03,500\nCaf\xe9\n"), 0o644)

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
	byLabel := map[string]subtitle.Track{}
	for _, tr := range item.Subtitles {
		byLabel[tr.Label] = tr
	}
	es, de, en := byLabel["Spanish"], byLabel["German (Forced)"], byLabel["English"]
	if len(item.Subtitles) != 3 || es.ID != "e0" || de.ID != "e1" || !de.BurnIn || !en.External {
		t.Fatalf("tracks: %+v", item.Subtitles)
	}

	// Text tracks come back as WebVTT, embedded or not, whatever their encoding.
	if vtt := get(t, c, base+"/subtitles/"+es.ID+".vtt"); !strings.HasPrefix(vtt, "WEBVTT") || !strings.Contains(vtt, "00:01.000 --> 00:04.000") || !strings.Contains(vtt, "Hola") {
		t.Fatalf("embedded track:\n%s", vtt)
	}
	if vtt := get(t, c, base+"/subtitles/"+en.ID+".vtt"); !strings.Contains(vtt, "Café") {
		t.Fatalf("sidecar track:\n%s", vtt)
	}
	if code := c.do("GET", base+"/subtitles/"+de.ID+".vtt", nil, nil); code != http.StatusUnprocessableEntity {
		t.Fatalf("picture track as text: %d", code)
	}
	if code := c.do("GET", base+"/subtitles/nope.vtt", nil, nil); code != http.StatusNotFound {
		t.Fatalf("unknown track: %d", code)
	}

	// Picture subtitles are burned into a transcode: the box shows up white.
	var hls struct{ URL string }
	if code := c.do("POST", base+"/hls?subtitle="+de.ID, nil, &hls); code != 200 {
		t.Fatalf("start hls with burn-in: %d", code)
	}
	seg := filepath.Join(t.TempDir(), "seg.ts")
	os.WriteFile(seg, []byte(get(t, c, strings.Replace(hls.URL, "index.m3u8", "seg00000.ts", 1))), 0o644)
	if box, rest := luma(t, seg, "crop=120:40:100:180"), luma(t, seg, "crop=120:40:100:20"); box < 200 || rest > 40 {
		t.Fatalf("burned-in box: luma %d inside, %d outside", box, rest)
	}
	if code := c.do("POST", base+"/hls?subtitle=e9", nil, nil); code != http.StatusBadRequest {
		t.Fatalf("burn-in of an unknown track: %d", code)
	}
}

// luma returns the average brightness (0-255) of a video's frame at 2s
// after the given filter.
func luma(t *testing.T, file, filter string) int {
	t.Helper()
	out, err := exec.Command("ffmpeg", "-v", "error", "-i", file, "-ss", "2", "-frames:v", "1", "-vf", filter+",format=gray,scale=1:1:flags=area",
		"-f", "rawvideo", "-").Output()
	if err != nil || len(out) != 1 {
		t.Fatalf("measure %s: %v (%d bytes)", file, err, len(out))
	}
	return int(out[0])
}

// pgsBox writes a PGS (Blu-ray) subtitle stream showing a w×h white box at
// x,y on a width×height canvas from second from until second to.
func pgsBox(width, height, x, y, w, h int, from, to float64) []byte {
	var buf bytes.Buffer
	seg := func(pts float64, kind byte, body []byte) {
		buf.WriteString("PG")
		binary.Write(&buf, binary.BigEndian, uint32(pts*90000))
		binary.Write(&buf, binary.BigEndian, uint32(0))
		buf.WriteByte(kind)
		binary.Write(&buf, binary.BigEndian, uint16(len(body)))
		buf.Write(body)
	}
	u16 := func(b *bytes.Buffer, v int) { binary.Write(b, binary.BigEndian, uint16(v)) }
	pcs := func(n int, state byte, objects int) []byte {
		var b bytes.Buffer
		u16(&b, width)
		u16(&b, height)
		b.WriteByte(0x10) // frame rate, unused
		u16(&b, n)
		b.Write([]byte{state, 0, 0, byte(objects)}) // state, palette update, palette id, objects
		if objects > 0 {
			u16(&b, 0)            // object id
			b.Write([]byte{0, 0}) // window id, not cropped
			u16(&b, x)
			u16(&b, y)
		}
		return b.Bytes()
	}
	var wds bytes.Buffer
	wds.WriteByte(1)
	wds.WriteByte(0)
	for _, v := range []int{x, y, w, h} {
		u16(&wds, v)
	}
	// Run-length encoded lines of color 1: 0x00, then a long colored run, then end of line.
	var rle bytes.Buffer
	for range h {
		rle.Write([]byte{0, 0xC0 | byte(w>>8), byte(w), 1, 0, 0})
	}
	var ods bytes.Buffer
	u16(&ods, 0)
	ods.Write([]byte{0, 0xC0}) // version, first and last fragment
	n := rle.Len() + 4
	ods.Write([]byte{byte(n >> 16), byte(n >> 8), byte(n)})
	u16(&ods, w)
	u16(&ods, h)
	ods.Write(rle.Bytes())

	seg(from, 0x16, pcs(0, 0x80, 1))
	seg(from, 0x17, wds.Bytes())
	seg(from, 0x14, []byte{0, 0, 1, 235, 128, 128, 255}) // palette 0, entry 1: opaque white
	seg(from, 0x15, ods.Bytes())
	seg(from, 0x80, nil)
	seg(to, 0x16, pcs(1, 0, 0))
	seg(to, 0x17, wds.Bytes())
	seg(to, 0x80, nil)
	return buf.Bytes()
}
