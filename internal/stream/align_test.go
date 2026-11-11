package stream

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yeixio/matinee/internal/store"
)

func TestParseFirstDTS(t *testing.T) {
	for _, c := range []struct {
		out  string
		want float64
	}{
		{"#format: frame checksums\n#version: 2\n#tb 0: 1/1000\n#media_type 0: video\n#codec_id 0: hevc\n0,      -1020,       -895,       41,   690887, 0x0e1761a4\n", -1.020},
		{"#tb 0: 1/90000\n0,      -7508,          0,     3753,     5287, 0xce326616, S=1,        1\n", -7508.0 / 90000},
		{"#tb 0: 1/1000\n0,          0,         42,       41,     2170, 0xfd0b4f8e\n", 0},
	} {
		got, err := parseFirstDTS(c.out)
		if err != nil || math.Abs(got-c.want) > 1e-9 {
			t.Errorf("parseFirstDTS(%q) = %v, %v; want %v", c.out, got, err, c.want)
		}
	}
	if _, err := parseFirstDTS("#tb 0: 1/1000\n"); err == nil {
		t.Error("no packet: want an error")
	}
}

func TestAlignFilter(t *testing.T) {
	// Dune resumed at 23 s: its picture's decoding starts 1.020 s before.
	if got, want := AlignFilter(-1.020), "aresample=async=1:first_pts=-47936"; got != want {
		t.Errorf("AlignFilter(-1.020) = %q, want %q", got, want)
	}
	if got, want := AlignFilter(0), "aresample=async=1:first_pts=1024"; got != want {
		t.Errorf("AlignFilter(0) = %q, want %q", got, want)
	}
}

// TestCopiedPictureInSyncOnApple plays a copied HEVC picture and its
// converted sound the way Apple's players do with fMP4 HLS without edit
// lists: the picture at each frame's time, the sound sample after sample
// from the start of its track, whatever its samples' durations say. A
// flash in the picture and a beep in the sound at the same moment of the
// film have to come out together, started from the beginning and from
// between keyframes, where the copied picture starts seconds early.
func TestCopiedPictureInSyncOnApple(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "film.mkv")
	const mark = 9.3 // when the film flashes and beeps
	gen := exec.Command(ffmpeg, "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", fmt.Sprintf("color=black:size=320x180:rate=24000/1001:duration=20,drawbox=enable='between(t,%g,%g)':color=white:t=fill", mark, mark+0.04),
		"-f", "lavfi", "-i", fmt.Sprintf("aevalsrc='if(between(t,%g,%g),sin(2*PI*1000*t),0)':s=48000:d=20", mark, mark+0.04),
		"-c:v", "libx265", "-preset", "ultrafast", "-x265-params", "log-level=none:keyint=120:min-keyint=120:bframes=3",
		"-c:a", "ac3", "-shortest", src)
	if out, err := gen.CombinedOutput(); err != nil {
		t.Skipf("can't make an HEVC clip here: %v\n%s", err, out)
	}
	tr := NewTranscoder(ffmpeg, filepath.Join(dir, "cache"), nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer tr.Close()
	can := Plays{Video: []string{"hevc"}, HDR: []string{"hdr10", "hlg"}}
	// Keyframes every 120 frames (5.005 s): 7 s is two seconds past one.
	for i, at := range []float64{0, 7} {
		it := store.Item{ID: int64(i + 1), Path: src, VideoCodec: "hevc", AudioCodec: "ac3", Width: intp(320)}
		id, err := tr.StartFor(context.Background(), int64(i+1), it, at, Request{BurnSub: -1, Audio: -1, Plays: can, Picture: Picture{Known: true}})
		if err != nil {
			t.Fatal(err)
		}
		stream := wholeStream(t, filepath.Join(dir, "cache", id))
		if bytes.Contains(stream, []byte("elst")) {
			t.Errorf("start at %v: the init piece has edit lists", at)
		}
		f := filepath.Join(dir, fmt.Sprintf("stream%d.mp4", i))
		os.WriteFile(f, stream, 0o644)

		// No sound sample may be stretched over a gap: its length is what
		// a player plays.
		for n, d := range soundDurations(stream) {
			if d != aacFrame {
				t.Errorf("start at %v: sound sample %d lasts %d samples' time, not %d", at, n, d, aacFrame)
				break
			}
		}
		flash := flashTime(t, ffmpeg, f)
		beep := beepTime(t, ffmpeg, f)
		if flash < 0 || beep < 0 {
			t.Fatalf("start at %v: flash at %v, beep at %v", at, flash, beep)
		}
		// Within a frame (and the AC3 source's own delay).
		if d := beep - flash; math.Abs(d) > 0.045 {
			t.Errorf("start at %v: the beep is heard %+.3f s from the flash", at, d)
		}
	}
}

// wholeStream waits for FFmpeg to finish the session's pieces and returns
// the init piece followed by all of them, as a player reads them.
func wholeStream(t *testing.T, dir string) []byte {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	var list []byte
	for time.Now().Before(deadline) {
		list, _ = os.ReadFile(filepath.Join(dir, "index.m3u8"))
		if bytes.Contains(list, []byte("#EXT-X-ENDLIST")) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !bytes.Contains(list, []byte("#EXT-X-ENDLIST")) {
		t.Fatal("the session didn't finish")
	}
	all, err := os.ReadFile(filepath.Join(dir, "init.mp4"))
	if err != nil {
		t.Fatal(err)
	}
	sc := bufio.NewScanner(bytes.NewReader(list))
	for sc.Scan() {
		if l := sc.Text(); strings.HasSuffix(l, ".m4s") {
			b, err := os.ReadFile(filepath.Join(dir, l))
			if err != nil {
				t.Fatal(err)
			}
			all = append(all, b...)
		}
	}
	return all
}

// soundDurations lists the durations of the sound track's samples (track 2)
// from the pieces' trun boxes, leaving out the last, which may be short.
func soundDurations(b []byte) []uint32 {
	var out []uint32
	var walk func(b []byte, inTraf bool, dflt uint32, track uint32)
	walk = func(b []byte, inTraf bool, dflt uint32, track uint32) {
		for len(b) >= 8 {
			size := binary.BigEndian.Uint32(b)
			if size < 8 || int(size) > len(b) {
				return
			}
			box, body := string(b[4:8]), b[8:size]
			switch box {
			case "moof":
				walk(body, false, 0, 0)
			case "traf":
				walk(body, true, 0, 0)
			case "tfhd":
				flags := binary.BigEndian.Uint32(body) & 0xffffff
				track = binary.BigEndian.Uint32(body[4:])
				p := 8
				if flags&1 != 0 {
					p += 8
				}
				if flags&2 != 0 {
					p += 4
				}
				if flags&8 != 0 {
					dflt = binary.BigEndian.Uint32(body[p:])
				}
			case "trun":
				if track != 2 {
					break
				}
				flags := binary.BigEndian.Uint32(body) & 0xffffff
				n := int(binary.BigEndian.Uint32(body[4:]))
				p := 8
				if flags&1 != 0 {
					p += 4
				}
				if flags&4 != 0 {
					p += 4
				}
				for range n {
					d := dflt
					if flags&0x100 != 0 {
						d = binary.BigEndian.Uint32(body[p:])
						p += 4
					}
					for _, f := range []uint32{0x200, 0x400, 0x800} {
						if flags&f != 0 {
							p += 4
						}
					}
					out = append(out, d)
				}
			}
			b = b[size:]
		}
	}
	walk(b, false, 0, 0)
	if len(out) > 0 {
		out = out[:len(out)-1]
	}
	return out
}

// flashTime is when the first white frame shows, by its own time.
func flashTime(t *testing.T, ffmpeg, f string) float64 {
	t.Helper()
	out, err := exec.Command(ffmpeg, "-hide_banner", "-loglevel", "error", "-i", f, "-map", "0:v:0",
		"-vf", "signalstats,metadata=print:key=lavfi.signalstats.YAVG:file=-", "-f", "null", "-").Output()
	if err != nil {
		t.Fatal(err)
	}
	pts := -1.0
	for _, l := range strings.Split(string(out), "\n") {
		if _, rest, ok := strings.Cut(l, "pts_time:"); ok {
			fmt.Sscan(rest, &pts)
		}
		if _, v, ok := strings.Cut(l, "YAVG="); ok {
			var y float64
			fmt.Sscan(v, &y)
			if y > 128 {
				return pts
			}
		}
	}
	return -1
}

// beepTime is when the beep is heard with the sound played sample after
// sample from the start of its track, as Apple's players do.
func beepTime(t *testing.T, ffmpeg, f string) float64 {
	t.Helper()
	out, err := exec.Command(ffmpeg, "-hide_banner", "-loglevel", "error", "-i", f, "-map", "0:a:0",
		"-ac", "1", "-f", "s16le", "-").Output()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i+1 < len(out); i += 2 {
		if v := int16(binary.LittleEndian.Uint16(out[i:])); v > 8000 || v < -8000 {
			return float64(i/2) / alignedRate
		}
	}
	return -1
}
