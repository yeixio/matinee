package stream

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/yeixio/matinee/internal/store"
)

func TestCopyArgs(t *testing.T) {
	dv := Plays{Video: []string{"hevc"}, HDR: []string{"dolby-vision", "hdr10", "hlg"}}
	hdr := Plays{Video: []string{"hevc"}, HDR: []string{"hdr10", "hlg"}}
	p81 := Picture{Known: true, HDR: "dolby-vision", HDRBase: "hdr10", DVProfile: 8}
	p5 := Picture{Known: true, HDR: "dolby-vision", DVProfile: 5}
	p7 := Picture{Known: true, HDR: "dolby-vision", HDRBase: "hdr10", DVProfile: 7}
	for _, c := range []struct {
		name    string
		codec   string
		pic     Picture
		can     Plays
		strip   bool
		want    string
		notWant string
	}{
		{"hdr10", "hevc", Picture{Known: true, HDR: "hdr10"}, hdr, true, "-tag:v hvc1", "dovi"},
		{"8.1 to dolby vision keeps it", "hevc", p81, dv, true, "-tag:v hvc1 -strict unofficial", "dovi"},
		{"8.1 to hdr10 strips it", "hevc", p81, hdr, true, "-tag:v hvc1 -bsf:v dovi_rpu=strip=1", "unofficial"},
		{"8.1 to hdr10, old ffmpeg", "hevc", p81, hdr, false, "-tag:v hvc1", "dovi"},
		{"5 to dolby vision is dvh1", "hevc", p5, dv, true, "-tag:v dvh1 -strict unofficial", "dovi"},
		{"7 plays as its base", "hevc", p7, dv, true, "-tag:v hvc1 -bsf:v dovi_rpu=strip=1", "unofficial"},
		{"av1 dolby vision", "av1", Picture{Known: true, HDR: "dolby-vision", HDRBase: "hdr10", DVProfile: 10}, Plays{Video: []string{"av1"}, HDR: []string{"dolby-vision"}}, true, "-c:v copy -strict unofficial", "tag"},
	} {
		got := strings.Join(copyArgs(c.codec, c.pic, c.can, c.strip), " ")
		if !strings.Contains(got, c.want) || strings.Contains(got, c.notWant) {
			t.Errorf("%s: got %q, want %q and not %q", c.name, got, c.want, c.notWant)
		}
	}
}

func TestVideoPlanDolbyVision(t *testing.T) {
	ms := int64(60_000)
	uhd := store.Item{VideoCodec: "hevc", Width: intp(3840), DurationMs: &ms, Size: 300_000_000}
	dvOnly := Plays{Video: []string{"hevc"}, HDR: []string{"dolby-vision"}}
	dv := Plays{Video: []string{"hevc"}, HDR: []string{"dolby-vision", "hdr10"}}
	hlg := Plays{Video: []string{"hevc"}, HDR: []string{"hlg"}}
	sdr := Plays{Video: []string{"hevc"}}
	for _, c := range []struct {
		name string
		pic  Picture
		can  Plays
		copy bool
	}{
		{"8.1 on dolby vision", Picture{Known: true, HDR: "dolby-vision", HDRBase: "hdr10", DVProfile: 8}, dvOnly, true},
		{"8.1 on sdr", Picture{Known: true, HDR: "dolby-vision", HDRBase: "hdr10", DVProfile: 8}, sdr, false},
		{"8.4 on an hlg screen", Picture{Known: true, HDR: "dolby-vision", HDRBase: "hlg", DVProfile: 8}, hlg, true},
		{"5 on dolby vision", Picture{Known: true, HDR: "dolby-vision", DVProfile: 5}, dvOnly, true},
		{"5 on hlg is converted", Picture{Known: true, HDR: "dolby-vision", DVProfile: 5}, hlg, false},
		{"7 on dolby vision and hdr10", Picture{Known: true, HDR: "dolby-vision", HDRBase: "hdr10", DVProfile: 7}, dv, true},
		{"7 needs hdr10, not dolby vision", Picture{Known: true, HDR: "dolby-vision", HDRBase: "hdr10", DVProfile: 7}, dvOnly, false},
	} {
		copy, _ := videoPlan(uhd, c.pic, -1, nil, c.can)
		if copy != c.copy {
			t.Errorf("%s: copy %v, want %v", c.name, copy, c.copy)
		}
	}
}

// withDVConfig adds a Dolby Vision configuration record (dvcC or dvvC, as
// FFmpeg writes it) to the hvc1 sample entry of an MP4 whose moov comes
// after its media, so no chunk offsets move.
func withDVConfig(t *testing.T, mp4 []byte, profile, compat int) []byte {
	t.Helper()
	box := "dvcC"
	if profile > 7 {
		box = "dvvC"
	}
	rec := make([]byte, 32)
	binary.BigEndian.PutUint32(rec, 32)
	copy(rec[4:], box)
	rec[8] = 1 // version 1.0
	// profile (7 bits), level (6), rpu, el, bl present
	binary.BigEndian.PutUint16(rec[10:], uint16(profile<<9|6<<3|1<<2|1))
	rec[12] = byte(compat << 4)

	// Find the path down to hvc1, remembering where each box starts.
	var starts []int
	pos, end := 0, len(mp4)
	for _, name := range []string{"moov", "trak", "mdia", "minf", "stbl", "stsd", "hvc1"} {
		found := false
		for pos+8 <= end {
			size := int(binary.BigEndian.Uint32(mp4[pos:]))
			if size < 8 {
				t.Fatalf("odd box at %d", pos)
			}
			if string(mp4[pos+4:pos+8]) == name {
				starts = append(starts, pos)
				end = pos + size
				pos += 8
				switch name {
				case "stsd":
					pos += 8 // version, flags, entry count
				case "hvc1":
					pos += 78 // the visual sample entry's fields
				}
				found = true
				break
			}
			pos += size
		}
		if !found {
			t.Fatalf("no %s box", name)
		}
	}
	out := slices.Concat(mp4[:end], rec, mp4[end:])
	for _, s := range starts {
		binary.BigEndian.PutUint32(out[s:], binary.BigEndian.Uint32(out[s:])+32)
	}
	return out
}

// TestStartDolbyVision copies Dolby Vision files the way each screen takes
// them: with their Dolby Vision for a Dolby Vision screen, without it for
// HDR10, and converted when there's nothing under it to fall back to.
func TestStartDolbyVision(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe not installed")
	}
	dir := t.TempDir()
	plain := filepath.Join(dir, "plain.mp4")
	gen := exec.Command(ffmpeg, "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "testsrc2=size=320x180:rate=12:duration=3",
		"-c:v", "libx265", "-preset", "ultrafast", "-pix_fmt", "yuv420p10le", "-tag:v", "hvc1",
		"-x265-params", "log-level=none:keyint=12:colorprim=bt2020:transfer=smpte2084:colormatrix=bt2020nc",
		"-color_primaries", "bt2020", "-color_trc", "smpte2084", "-colorspace", "bt2020nc", plain)
	if out, err := gen.CombinedOutput(); err != nil {
		t.Skipf("can't make an HEVC clip here: %v\n%s", err, out)
	}
	data, _ := os.ReadFile(plain)
	files := map[int]string{}
	for _, p := range []struct{ profile, compat int }{{8, 1}, {5, 0}} {
		f := filepath.Join(dir, "dv"+string(rune('0'+p.profile))+".mp4")
		os.WriteFile(f, withDVConfig(t, data, p.profile, p.compat), 0o644)
		out, _ := exec.Command(ffprobe, "-v", "error", "-show_streams", f).Output()
		if !strings.Contains(string(out), "dv_profile="+string(rune('0'+p.profile))) {
			t.Fatalf("FFmpeg doesn't see the Dolby Vision record it was given:\n%s", out)
		}
		files[p.profile] = f
	}

	tr := NewTranscoder(ffmpeg, filepath.Join(dir, "cache"), nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer tr.Close()
	// What a player gets once the init piece is read: the sample entry and
	// whether it carries Dolby Vision.
	start := func(user int64, path string, pic Picture, can Plays) (id string, init []byte, kind string) {
		t.Helper()
		it := store.Item{ID: user, Path: path, VideoCodec: "hevc", Width: intp(320)}
		id, err := tr.StartFor(context.Background(), user, it, 0, Request{BurnSub: -1, Audio: -1, Plays: can, Picture: pic})
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range tr.Sessions() {
			if s.ID == id {
				kind = s.Kind
			}
		}
		init, _ = os.ReadFile(filepath.Join(dir, "cache", id, "init.mp4"))
		return id, init, kind
	}
	hasDV := func(b []byte) bool { return bytes.Contains(b, []byte("dvcC")) || bytes.Contains(b, []byte("dvvC")) }
	dv := Plays{Video: []string{"hevc"}, HDR: []string{"dolby-vision", "hdr10", "hlg"}}
	hdr10 := Plays{Video: []string{"hevc"}, HDR: []string{"hdr10", "hlg"}}
	p81 := Picture{Known: true, HDR: "dolby-vision", HDRBase: "hdr10", DVProfile: 8}
	p5 := Picture{Known: true, HDR: "dolby-vision", DVProfile: 5}

	if _, init, kind := start(1, files[8], p81, dv); kind != "copy" || !hasDV(init) || !bytes.Contains(init, []byte("hvc1")) {
		t.Errorf("8.1 for a Dolby Vision screen: %s, Dolby Vision kept %v", kind, hasDV(init))
	}
	if _, init, kind := start(2, files[8], p81, hdr10); kind != "copy" {
		t.Errorf("8.1 for an HDR10 screen: %s", kind)
	} else if tr.canStripDV() && hasDV(init) {
		t.Error("8.1 for an HDR10 screen still says Dolby Vision")
	}
	if _, init, kind := start(3, files[5], p5, dv); kind != "copy" || !hasDV(init) || !bytes.Contains(init, []byte("dvh1")) {
		t.Errorf("5 for a Dolby Vision screen: %s, dvh1 %v, Dolby Vision kept %v", kind, bytes.Contains(init, []byte("dvh1")), hasDV(init))
	}
	if id, _, kind := start(4, files[5], p5, hdr10); kind != "video" {
		t.Errorf("5 for an HDR10 screen: %s, want it converted", kind)
	} else if _, err := os.Stat(filepath.Join(dir, "cache", id, "seg00000.ts")); err != nil {
		t.Error(err)
	}
}
