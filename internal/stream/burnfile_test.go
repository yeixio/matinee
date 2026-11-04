package stream

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yeixio/matinee/internal/store"
)

// pgsSquare is a Blu-ray subtitle (.sup) showing a white 100×50 box in the
// middle of a 320×180 picture from 3 s to 5 s.
func pgsSquare() []byte {
	var b bytes.Buffer
	seg := func(pts uint32, typ byte, data []byte) {
		b.WriteString("PG")
		binary.Write(&b, binary.BigEndian, pts)
		binary.Write(&b, binary.BigEndian, uint32(0))
		b.WriteByte(typ)
		binary.Write(&b, binary.BigEndian, uint16(len(data)))
		b.Write(data)
	}
	u16 := func(v int) []byte { return []byte{byte(v >> 8), byte(v)} }
	cat := func(parts ...[]byte) []byte { return bytes.Join(parts, nil) }
	window := cat([]byte{1, 0}, u16(110), u16(65), u16(100), u16(50))
	show, hide := uint32(3*90000), uint32(5*90000)
	// Composition: an epoch start with one object in window 0.
	seg(show, 0x16, cat(u16(320), u16(180), []byte{0x10}, u16(0), []byte{0x80, 0, 0, 1}, u16(0), []byte{0, 0}, u16(110), u16(65)))
	seg(show, 0x17, window)
	seg(show, 0x14, []byte{0, 0, 1, 235, 128, 128, 255}) // palette: entry 1 is opaque white
	var rle []byte
	for range 50 {
		rle = append(rle, 0x00, 0xC0, 100, 0x01, 0x00, 0x00) // 100 pixels of colour 1, end of line
	}
	obj := cat(u16(100), u16(50), rle)
	n := len(obj)
	seg(show, 0x15, cat(u16(0), []byte{0, 0xC0, byte(n >> 16), byte(n >> 8), byte(n)}, obj))
	seg(show, 0x80, nil)
	// Then nothing.
	seg(hide, 0x16, cat(u16(320), u16(180), []byte{0x10}, u16(1), []byte{0, 0, 0, 0}))
	seg(hide, 0x17, window)
	seg(hide, 0x80, nil)
	return b.Bytes()
}

func TestStartBurnsSubtitleFile(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "Movie.mkv")
	if out, err := exec.Command(ffmpeg, "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "color=c=black:s=320x180:r=12:d=8",
		"-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p", src).CombinedOutput(); err != nil {
		t.Skipf("can't make a clip here: %v\n%s", err, out)
	}
	sup := filepath.Join(dir, "Movie.en.sup")
	os.WriteFile(sup, pgsSquare(), 0o644)

	var logs bytes.Buffer
	tr := NewTranscoder(ffmpeg, filepath.Join(dir, "cache"), nil, slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer tr.Close()
	it := store.Item{ID: 1, Path: src, VideoCodec: "h264", Width: intp(320)}
	// middle is the brightness of the picture's centre in a stream's first frame.
	middle := func(user int64, start float64) byte {
		t.Helper()
		id, err := tr.StartFor(context.Background(), user, it, start, Request{BurnSub: -1, BurnFile: sup})
		if err != nil {
			// DIAG: what FFmpeg says when it runs the same command.
			var args []string
			for _, line := range bytes.Split(logs.Bytes(), []byte("\n")) {
				var e struct {
					Msg  string   `json:"msg"`
					Args []string `json:"args"`
				}
				if json.Unmarshal(line, &e) == nil && e.Msg == "ffmpeg command" {
					args = e.Args
				}
			}
			for i := range args {
				if args[i] == "error" && i > 0 && args[i-1] == "-loglevel" {
					args[i] = "verbose"
				}
			}
			os.RemoveAll(filepath.Join(dir, "cache", "diag"))
			os.MkdirAll(filepath.Join(dir, "cache", "diag"), 0o755)
			for i := range args {
				args[i] = strings.ReplaceAll(args[i], filepath.Join(dir, "cache", id), filepath.Join(dir, "cache", "diag"))
			}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			out, _ := exec.CommandContext(ctx, ffmpeg, args...).CombinedOutput()
			if len(out) > 8000 {
				out = append(out[:3000:3000], out[len(out)-5000:]...)
			}
			files, _ := os.ReadDir(filepath.Join(dir, "cache", "diag"))
			var names []string
			for _, f := range files {
				names = append(names, f.Name())
			}
			t.Logf("args %q\nmade %v", args, names)
			t.Fatalf("%v (start %v)\n%s", err, start, out)
		}
		px, err := exec.Command(ffmpeg, "-v", "error", "-i", filepath.Join(dir, "cache", id, "seg00000.ts"),
			"-vf", "crop=2:2:159:89,format=gray", "-frames:v", "1", "-f", "rawvideo", "-").Output()
		if err != nil || len(px) == 0 {
			t.Fatalf("frame: %v", err)
		}
		return px[0]
	}
	// From 3.5 s in, the box shows straight away: the file is read from the same moment.
	if v := middle(7, 3.5); v < 150 {
		t.Errorf("at 3.5 s the middle is %d, want the white box", v)
	}
	if v := middle(8, 0); v > 60 {
		t.Errorf("at 0 s the middle is %d, want black", v)
	}
}

func TestSubtitleFileStart(t *testing.T) {
	dir := t.TempDir()
	sup := filepath.Join(dir, "a.sup")
	os.WriteFile(sup, pgsSquare(), 0o644)
	idx := filepath.Join(dir, "a.idx")
	os.WriteFile(idx, []byte("# VobSub index file, v7\nid: en, index: 0\ntimestamp: 00:01:02:500, filepos: 000000000\n"), 0o644)
	for path, want := range map[string]float64{sup: 3, idx: 62.5, filepath.Join(dir, "none.sup"): 0} {
		if got := subtitleFileStart(path); got != want {
			t.Errorf("%s: got %v, want %v", filepath.Base(path), got, want)
		}
	}
}
