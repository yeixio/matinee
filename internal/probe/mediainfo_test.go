package probe

import (
	"context"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// A Dolby Vision profile 8.1 film with a TrueHD Atmos track, a commentary,
// and subtitles, the way ffprobe describes an MKV remux.
const dolbyVisionRemux = `{
  "format": {"format_name": "matroska,webm", "duration": "7380.5", "size": "61000000000", "bit_rate": "66123456"},
  "chapters": [{}, {}, {}],
  "streams": [
    {"codec_type": "video", "codec_name": "hevc", "profile": "Main 10", "width": 3840, "height": 2160,
     "pix_fmt": "yuv420p10le", "color_transfer": "smpte2084", "color_primaries": "bt2020", "color_space": "bt2020nc",
     "avg_frame_rate": "24000/1001", "disposition": {"default": 1},
     "tags": {"BPS-eng": "58000000"},
     "side_data_list": [{"side_data_type": "DOVI configuration record", "dv_profile": 8, "dv_level": 6, "dv_bl_signal_compatibility_id": 1}]},
    {"codec_type": "audio", "codec_name": "truehd", "profile": "Dolby TrueHD + Dolby Atmos", "channels": 8,
     "channel_layout": "7.1", "sample_rate": "48000", "bits_per_raw_sample": "24", "disposition": {"default": 1},
     "tags": {"language": "eng", "title": "TrueHD 7.1"}},
    {"codec_type": "audio", "codec_name": "ac3", "channels": 2, "channel_layout": "stereo", "sample_rate": "48000",
     "bit_rate": "192000", "disposition": {"comment": 1}, "tags": {"language": "eng", "title": "Director's Commentary"}},
    {"codec_type": "subtitle", "codec_name": "hdmv_pgs_subtitle", "disposition": {"forced": 1}, "tags": {"language": "eng"}},
    {"codec_type": "subtitle", "codec_name": "subrip", "disposition": {"hearing_impaired": 1}, "tags": {"LANGUAGE": "spa"}}
  ]
}`

func TestParseMediaInfo(t *testing.T) {
	mi, err := parseMediaInfo([]byte(dolbyVisionRemux))
	if err != nil {
		t.Fatal(err)
	}
	if mi.Container != "matroska" || mi.DurationMs != 7380500 || mi.Size != 61000000000 || mi.Bitrate != 66123456 || mi.Chapters != 3 {
		t.Fatalf("format: %+v", mi)
	}
	v := mi.Video[0]
	if v.Resolution != "4K" || v.HDR != "dolby-vision" || v.HDRBase != "hdr10" || v.DVProfile != 8 || v.BitDepth != 10 ||
		v.FrameRate != 23.976 || v.Bitrate != 58000000 {
		t.Fatalf("video: %+v", v)
	}
	if a := mi.Audio[0]; !a.Atmos || a.Channels != 8 || a.BitDepth != 24 || a.Language != "eng" {
		t.Fatalf("audio 0: %+v", a)
	}
	if a := mi.Audio[1]; !a.Commentary || a.Atmos || a.Bitrate != 192000 {
		t.Fatalf("audio 1: %+v", a)
	}
	if len(mi.Subtitles) != 2 || !mi.Subtitles[0].Forced || !mi.Subtitles[1].SDH || mi.Subtitles[1].Language != "spa" {
		t.Fatalf("subtitles: %+v", mi.Subtitles)
	}
	want := []string{"4k", "hdr", "dolby-vision", "hevc", "atmos", "7.1"}
	if got := mi.Tags(); !slices.Equal(got, want) {
		t.Fatalf("tags: %v, want %v", got, want)
	}
}

func TestFrameSideDataFindsHDR10Plus(t *testing.T) {
	v := VideoStream{HDR: "hdr10"}
	applyFrameSideData(&v, []byte(`{"frames": [{"side_data_list": [
		{"side_data_type": "Mastering display metadata"},
		{"side_data_type": "HDR Dynamic Metadata SMPTE2094-40 (HDR10+)"}]}]}`))
	if v.HDR != "hdr10+" {
		t.Fatalf("hdr: %q", v.HDR)
	}
	dv := VideoStream{HDR: "dolby-vision", HDRBase: "hdr10"}
	applyFrameSideData(&dv, []byte(`{"frames": [{"side_data_list": [{"side_data_type": "HDR Dynamic Metadata SMPTE2094-40 (HDR10+)"}]}]}`))
	if dv.HDR != "dolby-vision" || dv.HDRBase != "hdr10+" {
		t.Fatalf("dv: %+v", dv)
	}
}

func TestMusicTags(t *testing.T) {
	mi := MediaInfo{Audio: []AudioStream{{Codec: "flac", Channels: 2, BitDepth: 24, SampleRate: 96000}}}
	if got := mi.Tags(); !slices.Equal(got, []string{"lossless", "hi-res", "stereo"}) {
		t.Fatalf("tags: %v", got)
	}
	if got := (MediaInfo{Error: "bad"}).Tags(); !slices.Equal(got, []string{"unreadable"}) {
		t.Fatalf("tags: %v", got)
	}
}

func TestResolutionLabel(t *testing.T) {
	for _, c := range []struct {
		w, h int
		want string
	}{
		{3840, 2160, "4K"}, {3840, 1600, "4K"}, {1920, 800, "1080p"}, {1920, 1036, "1080p"},
		{1280, 536, "720p"}, {720, 576, "576p"}, {720, 480, "480p"}, {640, 360, "SD"}, {0, 0, ""},
	} {
		if got := ResolutionLabel(c.w, c.h); got != c.want {
			t.Errorf("%dx%d: %q, want %q", c.w, c.h, got, c.want)
		}
	}
}

// Analyze on a real HDR10 file made with x265.
func TestAnalyzeHDR10File(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe not installed")
	}
	file := filepath.Join(t.TempDir(), "hdr.mkv")
	out, err := exec.Command(ffmpeg, "-loglevel", "error", "-f", "lavfi", "-i", "testsrc2=s=1920x800:d=0.2",
		"-f", "lavfi", "-i", "sine=d=0.2", "-c:v", "libx265", "-pix_fmt", "yuv420p10le",
		"-x265-params", "log-level=error:colorprim=bt2020:transfer=smpte2084:colormatrix=bt2020nc",
		"-c:a", "ac3", "-ac", "6", "-metadata:s:a:0", "language=ger", file).CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "libx265") {
			t.Skip("ffmpeg has no libx265")
		}
		t.Fatalf("make file: %v\n%s", err, out)
	}
	mi, err := Prober{Path: ffprobe, LowPriority: true}.Analyze(context.Background(), file)
	if err != nil {
		t.Fatal(err)
	}
	if v := mi.Video[0]; v.Codec != "hevc" || v.HDR != "hdr10" || v.BitDepth != 10 || v.Resolution != "1080p" {
		t.Fatalf("video: %+v", v)
	}
	if a := mi.Audio[0]; a.Codec != "ac3" || a.Channels != 6 || a.Language != "ger" {
		t.Fatalf("audio: %+v", a)
	}
	if got := mi.Tags(); !slices.Equal(got, []string{"1080p", "hdr", "hdr10", "hevc", "5.1"}) {
		t.Fatalf("tags: %v", got)
	}
}
