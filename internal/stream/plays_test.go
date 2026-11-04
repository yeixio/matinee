package stream

import (
	"context"
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

func TestParsePlays(t *testing.T) {
	p := ParsePlays("H264, hevc,vp9,hevc", "hdr10,dolby-vision,nope")
	if !slices.Equal(p.Video, []string{"h264", "hevc"}) || !slices.Equal(p.HDR, []string{"hdr10", "dolby-vision"}) {
		t.Errorf("got %+v", p)
	}
	if p := ParsePlays("", ""); p.Video != nil || p.HDR != nil {
		t.Errorf("empty: %+v", p)
	}
}

func TestVideoPlan(t *testing.T) {
	ms := int64(60_000)
	uhd := store.Item{VideoCodec: "hevc", Width: intp(3840), DurationMs: &ms, Size: 300_000_000}
	hevc := Plays{Video: []string{"hevc"}}
	hdr := Plays{Video: []string{"hevc", "av1"}, HDR: []string{"hdr10", "hlg"}}
	sdr := Picture{Known: true}
	for _, c := range []struct {
		name  string
		it    store.Item
		pic   Picture
		burn  int
		steps []Step
		can   Plays
		copy  bool
		why   string
	}{
		{"h264 to anyone", store.Item{VideoCodec: "h264"}, Picture{}, -1, nil, Plays{}, true, ""},
		{"hevc to a player that says nothing", uhd, sdr, -1, nil, Plays{}, false, ConvertCodec},
		{"hevc to a player that decodes it", uhd, sdr, -1, nil, hevc, true, ""},
		{"not analyzed yet, sdr screen", uhd, Picture{}, -1, nil, hevc, false, ConvertHDR},
		{"not analyzed yet, hdr screen", uhd, Picture{}, -1, nil, hdr, true, ""},
		{"hdr10 to an sdr screen", uhd, Picture{Known: true, HDR: "hdr10"}, -1, nil, hevc, false, ConvertHDR},
		{"hdr10 to an hdr screen", uhd, Picture{Known: true, HDR: "hdr10"}, -1, nil, hdr, true, ""},
		{"hdr10+ falls back to hdr10", uhd, Picture{Known: true, HDR: "hdr10+"}, -1, nil, hdr, true, ""},
		{"dolby vision 8.1 on hdr10", uhd, Picture{Known: true, HDR: "dolby-vision", HDRBase: "hdr10"}, -1, nil, hdr, true, ""},
		{"dolby vision 5 needs dolby vision", uhd, Picture{Known: true, HDR: "dolby-vision"}, -1, nil, hdr, false, ConvertHDR},
		{"picture subtitle", uhd, sdr, 2, nil, hevc, false, ConvertSubtitle},
		{"lower quality", uhd, sdr, -1, Steps[1:2], hevc, false, ConvertQuality},
		{"adaptive", uhd, sdr, -1, Steps, hevc, false, ConvertQuality},
		{"av1", store.Item{VideoCodec: "av1"}, Picture{Known: true, HDR: "hlg"}, -1, nil, hdr, true, ""},
		{"vp9 isn't copied", store.Item{VideoCodec: "vp9"}, sdr, -1, nil, hdr, false, ConvertCodec},
	} {
		copy, why := videoPlan(c.it, c.pic, c.burn, c.steps, c.can)
		if copy != c.copy || why != c.why {
			t.Errorf("%s: got %v %q, want %v %q", c.name, copy, why, c.copy, c.why)
		}
	}
}

func TestSignPlaylistInit(t *testing.T) {
	got := string(signPlaylist([]byte("#EXTM3U\n#EXT-X-MAP:URI=\"init.mp4\"\n#EXTINF:4,\nseg00000.m4s\n"), "a$b"))
	if !strings.Contains(got, `#EXT-X-MAP:URI="init.mp4?token=a%24b"`) || !strings.Contains(got, "seg00000.m4s?token=a%24b") {
		t.Errorf("got %s", got)
	}
}

// A 4K HEVC HDR10 file is repackaged for a player that plays it: the
// picture is copied into fMP4 pieces, still 4K, still HDR10, tagged hvc1.
func TestStartCopiesHEVC(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe not installed")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "uhd.mkv")
	gen := exec.Command(ffmpeg, "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "testsrc2=size=3840x2160:rate=12:duration=3",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=3",
		"-c:v", "libx265", "-preset", "ultrafast", "-pix_fmt", "yuv420p10le", "-x265-params", "log-level=none:keyint=12:colorprim=bt2020:transfer=smpte2084:colormatrix=bt2020nc",
		"-color_primaries", "bt2020", "-color_trc", "smpte2084", "-colorspace", "bt2020nc",
		"-c:a", "ac3", src)
	if out, err := gen.CombinedOutput(); err != nil {
		t.Skipf("can't make an HEVC clip here: %v\n%s", err, out)
	}
	tr := NewTranscoder(ffmpeg, filepath.Join(dir, "cache"), nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer tr.Close()
	it := store.Item{ID: 1, Path: src, VideoCodec: "hevc", AudioCodec: "ac3", Width: intp(3840)}
	id, err := tr.StartFor(context.Background(), 7, it, 0, Request{BurnSub: -1,
		Plays: Plays{Video: []string{"hevc"}, HDR: []string{"hdr10"}}, Picture: Picture{Known: true, HDR: "hdr10"}})
	if err != nil {
		t.Fatal(err)
	}
	if s := tr.Sessions(); len(s) != 1 || s[0].Kind != "copy" || s[0].Why != "" {
		t.Errorf("sessions: %+v", s)
	}
	sdir := filepath.Join(dir, "cache", id)
	for _, f := range []string{"init.mp4", "seg00000.m4s"} {
		if !hlsFileRe.MatchString(f) {
			t.Errorf("%s isn't served", f)
		}
		if _, err := os.Stat(filepath.Join(sdir, f)); err != nil {
			t.Errorf("%s: %v", f, err)
		}
	}
	// HDR10 is in the HEVC stream itself (as in real files), which a copy keeps.
	// What a player gets once the init piece and the first segment are put together.
	init, _ := os.ReadFile(filepath.Join(sdir, "init.mp4"))
	seg, _ := os.ReadFile(filepath.Join(sdir, "seg00000.m4s"))
	joined := filepath.Join(dir, "joined.mp4")
	os.WriteFile(joined, append(init, seg...), 0o644)
	out, err := exec.Command(ffprobe, "-v", "error", "-select_streams", "v:0", "-show_entries",
		"stream=codec_name,codec_tag_string,width,color_transfer", "-of", "default=nw=1", joined).Output()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"codec_name=hevc", "codec_tag_string=hvc1", "width=3840", "color_transfer=smpte2084"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("copied picture: want %s in\n%s", want, out)
		}
	}
	playlist, _ := os.ReadFile(filepath.Join(sdir, "index.m3u8"))
	if !strings.Contains(string(playlist), `#EXT-X-MAP:URI="init.mp4"`) {
		t.Errorf("playlist: %s", playlist)
	}

	// The same file for a screen without HDR is converted, tone mapped to
	// SDR, and says why.
	id2, err := tr.StartFor(context.Background(), 8, it, 0, Request{BurnSub: -1,
		Plays: Plays{Video: []string{"hevc"}}, Picture: Picture{Known: true, HDR: "hdr10"}, ToneMap: ToneMapHable})
	if err != nil {
		t.Fatal(err)
	}
	tools := tr.ToneTools()
	for _, s := range tr.Sessions() {
		if s.ID == id2 && (s.Kind != "video" || s.Why != ConvertHDR || s.ToneMapped != tools.Zscale) {
			t.Errorf("sdr session: %+v", s)
		}
	}
	if !tools.Zscale {
		t.Log("this FFmpeg can't tone map")
		return
	}
	out, err = exec.Command(ffprobe, "-v", "error", "-select_streams", "v:0", "-show_entries",
		"stream=codec_name,pix_fmt,color_transfer,color_primaries", "-of", "default=nw=1", filepath.Join(dir, "cache", id2, "seg00000.ts")).Output()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"codec_name=h264", "pix_fmt=yuv420p", "color_transfer=bt709", "color_primaries=bt709"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("tone mapped picture: want %s in\n%s", want, out)
		}
	}
}

func TestToneMap(t *testing.T) {
	all := ToneTools{Zscale: true, Placebo: true}
	if c, _ := ToneMap(Picture{Known: true, HDR: "hdr10"}, ToneMapHable, all); !strings.Contains(c, "tin=smpte2084") || !strings.Contains(c, "tonemap=tonemap=hable") {
		t.Errorf("hdr10: %s", c)
	}
	if c, _ := ToneMap(Picture{Known: true, HDR: "hlg"}, ToneMapMobius, all); !strings.Contains(c, "tin=arib-std-b67") || !strings.Contains(c, "tonemap=mobius") {
		t.Errorf("hlg: %s", c)
	}
	if c, _ := ToneMap(Picture{Known: true, HDR: "dolby-vision", HDRBase: "hdr10"}, ToneMapHable, all); !strings.Contains(c, "tin=smpte2084") {
		t.Errorf("dolby vision 8.1 uses its hdr10 layer: %s", c)
	}
	if c, in := ToneMap(Picture{Known: true, HDR: "dolby-vision"}, ToneMapHable, all); !strings.HasPrefix(c, "libplacebo=") || len(in) == 0 {
		t.Errorf("dolby vision 5: %s %v", c, in)
	}
	if c, _ := ToneMap(Picture{Known: true, HDR: "dolby-vision"}, ToneMapHable, ToneTools{Zscale: true}); c != "" {
		t.Errorf("dolby vision 5 without libplacebo: %s", c)
	}
	for _, c := range []struct {
		pic   Picture
		algo  string
		tools ToneTools
	}{
		{Picture{Known: true}, ToneMapHable, all},
		{Picture{Known: true, HDR: "hdr10"}, ToneMapOff, all},
		{Picture{Known: true, HDR: "hdr10"}, ToneMapHable, ToneTools{}},
		{Picture{HDR: "hdr10"}, ToneMapHable, all},
	} {
		if got, _ := ToneMap(c.pic, c.algo, c.tools); got != "" {
			t.Errorf("%+v %s %+v: %s", c.pic, c.algo, c.tools, got)
		}
	}
}
