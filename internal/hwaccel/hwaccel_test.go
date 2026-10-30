package hwaccel

import (
	"io"
	"log/slog"
	"slices"
	"strings"
	"testing"
)

var hevc = Video{Codec: "hevc", BurnSub: -1, MaxWidth: 1920, KeyframeSeconds: 4}

// The exact command lines for each backend. Most of these can't run in CI,
// so they're spelled out here for review; people with the hardware confirm
// them (see the "verify hardware transcoding" issues).
func TestArgs(t *testing.T) {
	const keyframes = " -force_key_frames expr:gte(t,n_forced*4)"
	tests := []struct {
		name   string
		accel  *Accel
		mode   Mode
		video  Video
		input  string
		output string
	}{
		{
			name: "cpu", accel: nil, mode: Full, video: hevc,
			output: "-map 0:v:0 -vf scale='min(1920,iw)':-2 -c:v libx264 -preset veryfast -crf 22 -maxrate 10M -bufsize 20M -pix_fmt yuv420p" + keyframes,
		},
		{
			name: "vaapi full", accel: &Accel{Kind: VAAPI, Device: "/dev/dri/renderD128"}, mode: Full, video: hevc,
			input:  "-hwaccel vaapi -hwaccel_device /dev/dri/renderD128 -hwaccel_output_format vaapi",
			output: "-map 0:v:0 -vf scale_vaapi=w='min(1920,iw)':h=-2:format=nv12 -c:v h264_vaapi -rc_mode VBR -b:v 8M -maxrate 10M -bufsize 20M -profile:v high" + keyframes,
		},
		{
			name: "vaapi encode", accel: &Accel{Kind: VAAPI, Device: "/dev/dri/renderD129"}, mode: Encode, video: hevc,
			input:  "-init_hw_device vaapi=va:/dev/dri/renderD129 -filter_hw_device va",
			output: "-map 0:v:0 -vf scale='min(1920,iw)':-2,format=nv12,hwupload -c:v h264_vaapi -rc_mode VBR -b:v 8M -maxrate 10M -bufsize 20M -profile:v high" + keyframes,
		},
		{
			name: "nvenc full", accel: &Accel{Kind: NVENC}, mode: Full, video: hevc,
			input:  "-hwaccel cuda -hwaccel_output_format cuda",
			output: "-map 0:v:0 -vf scale_cuda=w='min(1920,iw)':h=-2:format=yuv420p -c:v h264_nvenc -preset p4 -tune hq -rc vbr -cq 23 -b:v 8M -maxrate 10M -bufsize 20M -profile:v high -forced-idr 1" + keyframes,
		},
		{
			name: "nvenc encode", accel: &Accel{Kind: NVENC}, mode: Encode, video: hevc,
			output: "-map 0:v:0 -vf scale='min(1920,iw)':-2,format=yuv420p -c:v h264_nvenc -preset p4 -tune hq -rc vbr -cq 23 -b:v 8M -maxrate 10M -bufsize 20M -profile:v high -forced-idr 1" + keyframes,
		},
		{
			name: "qsv full on linux", accel: &Accel{Kind: QSV, Device: "/dev/dri/renderD128"}, mode: Full, video: hevc,
			input:  "-hwaccel qsv -qsv_device /dev/dri/renderD128 -hwaccel_output_format qsv -c:v hevc_qsv",
			output: "-map 0:v:0 -vf scale_qsv=w='min(1920,iw)':h=-2:format=nv12 -c:v h264_qsv -preset veryfast -b:v 8M -maxrate 10M -bufsize 20M -profile:v high -forced_idr 1" + keyframes,
		},
		{
			name: "qsv full on windows", accel: &Accel{Kind: QSV}, mode: Full, video: Video{Codec: "mpeg2video", BurnSub: -1, MaxWidth: 1280},
			input:  "-hwaccel qsv -hwaccel_output_format qsv -c:v mpeg2_qsv",
			output: "-map 0:v:0 -vf scale_qsv=w='min(1280,iw)':h=-2:format=nv12 -c:v h264_qsv -preset veryfast -b:v 8M -maxrate 10M -bufsize 20M -profile:v high -forced_idr 1",
		},
		{
			name: "qsv encode", accel: &Accel{Kind: QSV}, mode: Encode, video: hevc,
			output: "-map 0:v:0 -vf scale='min(1920,iw)':-2,format=nv12 -c:v h264_qsv -preset veryfast -b:v 8M -maxrate 10M -bufsize 20M -profile:v high -forced_idr 1" + keyframes,
		},
		{
			name: "videotoolbox full", accel: &Accel{Kind: VideoToolbox}, mode: Full, video: hevc,
			input:  "-hwaccel videotoolbox",
			output: "-map 0:v:0 -vf scale='min(1920,iw)':-2,format=yuv420p -c:v h264_videotoolbox -b:v 8M -profile:v high" + keyframes,
		},
		{
			name: "amf full", accel: &Accel{Kind: AMF}, mode: Full, video: hevc,
			input:  "-hwaccel auto",
			output: "-map 0:v:0 -vf scale='min(1920,iw)':-2,format=yuv420p -c:v h264_amf -usage transcoding -quality speed -rc vbr_peak -b:v 8M -maxrate 10M -bufsize 20M -profile:v high" + keyframes,
		},
		{
			name: "v4l2 encode", accel: &Accel{Kind: V4L2}, mode: Encode, video: hevc,
			output: "-map 0:v:0 -vf scale='min(1920,iw)':-2,format=yuv420p -c:v h264_v4l2m2m -b:v 8M" + keyframes,
		},
		{
			name: "burned-in subtitle", accel: &Accel{Kind: VAAPI, Device: "/dev/dri/renderD128"}, mode: Encode,
			video: Video{Codec: "hevc", BurnSub: 2, MaxWidth: 1920},
			input: "-init_hw_device vaapi=va:/dev/dri/renderD128 -filter_hw_device va",
			output: "-filter_complex [0:s:2][0:v:0]scale2ref[sub][vid];[vid][sub]overlay=eof_action=pass,scale='min(1920,iw)':-2,format=nv12,hwupload[v] -map [v] " +
				"-c:v h264_vaapi -rc_mode VBR -b:v 8M -maxrate 10M -bufsize 20M -profile:v high",
		},
		{
			name: "one quality step on the cpu", accel: nil, mode: CPU,
			video:  Video{Codec: "hevc", BurnSub: -1, Renditions: []Rendition{{MaxWidth: 1280, MaxRate: 4_000_000}}},
			output: "-map 0:v:0 -vf scale='min(1280,iw)':-2 -c:v libx264 -preset veryfast -crf 22 -maxrate 4000000 -bufsize 8000000 -pix_fmt yuv420p",
		},
		{
			name: "adaptive on the cpu", accel: nil, mode: CPU,
			video: Video{Codec: "hevc", BurnSub: -1, KeyframeSeconds: 4, Renditions: []Rendition{{1920, 8_000_000}, {854, 1_500_000}}},
			output: "-filter_complex [0:v:0]split=2[s0][s1];[s0]scale='min(1920,iw)':-2[v0];[s1]scale='min(854,iw)':-2[v1] -map [v0] -map [v1] " +
				"-c:v:0 libx264 -preset:v:0 veryfast -crf:v:0 22 -maxrate:v:0 8000000 -bufsize:v:0 16000000 -pix_fmt:v:0 yuv420p -force_key_frames:v:0 expr:gte(t,n_forced*4) " +
				"-c:v:1 libx264 -preset:v:1 veryfast -crf:v:1 22 -maxrate:v:1 1500000 -bufsize:v:1 3000000 -pix_fmt:v:1 yuv420p -force_key_frames:v:1 expr:gte(t,n_forced*4)",
		},
		{
			name: "adaptive vaapi full", accel: &Accel{Kind: VAAPI, Device: "/dev/dri/renderD128"}, mode: Full,
			video: Video{Codec: "hevc", BurnSub: -1, Renditions: []Rendition{{1280, 4_000_000}, {640, 700_000}}},
			input: "-hwaccel vaapi -hwaccel_device /dev/dri/renderD128 -hwaccel_output_format vaapi",
			output: "-filter_complex [0:v:0]split=2[s0][s1];[s0]scale_vaapi=w='min(1280,iw)':h=-2:format=nv12[v0];[s1]scale_vaapi=w='min(640,iw)':h=-2:format=nv12[v1] -map [v0] -map [v1] " +
				"-c:v:0 h264_vaapi -rc_mode:v:0 VBR -b:v:0 3200000 -maxrate:v:0 4000000 -bufsize:v:0 8000000 -profile:v:0 high " +
				"-c:v:1 h264_vaapi -rc_mode:v:1 VBR -b:v:1 560000 -maxrate:v:1 700000 -bufsize:v:1 1400000 -profile:v:1 high",
		},
		{
			name: "adaptive with a burned-in subtitle", accel: &Accel{Kind: NVENC}, mode: Encode,
			video: Video{Codec: "hevc", BurnSub: 1, Renditions: []Rendition{{1920, 8_000_000}, {1280, 4_000_000}}},
			output: "-filter_complex [0:s:1][0:v:0]scale2ref[sub][vid];[vid][sub]overlay=eof_action=pass,split=2[s0][s1];" +
				"[s0]scale='min(1920,iw)':-2,format=yuv420p[v0];[s1]scale='min(1280,iw)':-2,format=yuv420p[v1] -map [v0] -map [v1] " +
				"-c:v:0 h264_nvenc -preset:v:0 p4 -tune:v:0 hq -rc:v:0 vbr -cq:v:0 23 -b:v:0 6400000 -maxrate:v:0 8000000 -bufsize:v:0 16000000 -profile:v:0 high -forced-idr:v:0 1 " +
				"-c:v:1 h264_nvenc -preset:v:1 p4 -tune:v:1 hq -rc:v:1 vbr -cq:v:1 23 -b:v:1 3200000 -maxrate:v:1 4000000 -bufsize:v:1 8000000 -profile:v:1 high -forced-idr:v:1 1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in, out := Args(tt.accel, tt.mode, tt.video)
			if got := strings.Join(in, " "); got != tt.input {
				t.Errorf("input\n got: %s\nwant: %s", got, tt.input)
			}
			if got := strings.Join(out, " "); got != tt.output {
				t.Errorf("output\n got: %s\nwant: %s", got, tt.output)
			}
		})
	}
}

func TestPlan(t *testing.T) {
	gpu := &Accel{Kind: VAAPI, Device: "/dev/dri/renderD128", Decode: true}
	encodeOnly := &Accel{Kind: VAAPI, Device: "/dev/dri/renderD128"}
	tests := []struct {
		name  string
		accel *Accel
		video Video
		want  []Mode
	}{
		{"no accelerator", nil, hevc, []Mode{CPU}},
		{"gpu decodes the source", gpu, hevc, []Mode{Full, Encode, CPU}},
		{"gpu can't decode the source", gpu, Video{Codec: "mpeg4", BurnSub: -1}, []Mode{Encode, CPU}},
		{"subtitles are drawn in software", gpu, Video{Codec: "hevc", BurnSub: 0}, []Mode{Encode, CPU}},
		{"decoding failed its test", encodeOnly, hevc, []Mode{Encode, CPU}},
		{"backend without hardware decode", &Accel{Kind: V4L2, Decode: true}, hevc, []Mode{Encode, CPU}},
	}
	for _, tt := range tests {
		if got := Plan(tt.accel, tt.video); !slices.Equal(got, tt.want) {
			t.Errorf("%s: got %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestEveryKindIsComplete(t *testing.T) {
	for _, k := range Kinds {
		if k.Name() == "" || encoder[k] == "" {
			t.Errorf("%s is missing a name or encoder", k)
		}
		if _, out := Args(&Accel{Kind: k}, Encode, hevc); !slices.Contains(out, encoder[k]) {
			t.Errorf("%s: encode args don't use %s: %v", k, encoder[k], out)
		}
	}
	if !ValidPreference("auto") || !ValidPreference("off") || !ValidPreference("vaapi") || ValidPreference("cuda") {
		t.Error("ValidPreference is wrong")
	}
}

func TestParseList(t *testing.T) {
	out := `Encoders:
 V..... = Video
 ------
 V....D h264_nvenc           NVIDIA NVENC H.264 encoder (codec h264)
 V....D h264_vaapi           H.264/AVC (VAAPI) (codec h264)
`
	got := parseList(out)
	if !got["h264_vaapi"] || !got["h264_nvenc"] || got["h264_qsv"] {
		t.Fatalf("parsed %v", got)
	}
	filters := " ... scale_vaapi       V->V       Scale to/from VAAPI surfaces.\n"
	if !parseList(filters)["scale_vaapi"] {
		t.Fatal("missed scale_vaapi")
	}
}

func TestGPUName(t *testing.T) {
	mesa := "[AVHWDeviceContext @ 0x1] Initialised VAAPI connection: version 1.22\n" +
		"[AVHWDeviceContext @ 0x1] VAAPI driver: Mesa Gallium driver 24.2.8-1 for AMD Radeon RX 7900 XTX (radeonsi, navi31, LLVM 19.1.1, DRM 3.59, 6.8.0-45-generic).\n"
	if got := gpuName(VAAPI, mesa); got != "AMD Radeon RX 7900 XTX" {
		t.Errorf("mesa: got %q", got)
	}
	intel := "[AVHWDeviceContext @ 0x1] VAAPI driver: Intel iHD driver for Intel(R) Gen Graphics - 24.1.0 ().\n"
	if got := gpuName(VAAPI, intel); got != "Intel(R) Gen Graphics - 24.1.0" {
		t.Errorf("intel: got %q", got)
	}
	if got := gpuName(VAAPI, "nothing useful\n"); got != "" {
		t.Errorf("no driver line: got %q", got)
	}
}

func TestPreference(t *testing.T) {
	m := NewManager("ffmpeg", slog.New(slog.NewTextHandler(io.Discard, nil)))
	if m.Active() != nil {
		t.Fatal("active before detection")
	}
	m.found = []Accel{{Kind: NVENC, Name: "NVIDIA NVENC"}, {Kind: VAAPI, Name: "VAAPI"}}
	if a := m.Active(); a == nil || a.Kind != NVENC {
		t.Fatalf("auto picked %v, want the first found", a)
	}
	m.SetPreference("vaapi")
	if a := m.Active(); a == nil || a.Kind != VAAPI {
		t.Fatalf("vaapi preference picked %v", a)
	}
	m.SetPreference("qsv")
	if a := m.Active(); a != nil {
		t.Fatalf("an unavailable choice should mean the CPU, got %v", a)
	}
	m.SetPreference("off")
	if a := m.Active(); a != nil {
		t.Fatalf("off picked %v", a)
	}
	m.SetPreference("")
	if s := m.Status(); s.Preference != Auto || s.Active == nil || len(s.Available) != 2 {
		t.Fatalf("status %+v", s)
	}
}

// On a machine without video hardware, detection finds nothing and says so
// quickly instead of failing.
func TestDetectWithoutHardware(t *testing.T) {
	m := NewManager("ffmpeg", slog.New(slog.NewTextHandler(io.Discard, nil)))
	if _, err := m.list(t.Context(), "-encoders"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	t.Setenv("MATINEE_VAAPI_DEVICE", "/nonexistent/renderD128")
	m.Detect(t.Context())
	s := m.Status()
	if s.Detecting {
		t.Fatal("still detecting")
	}
	for _, a := range s.Available {
		// A CI runner may genuinely have one; it must then have passed its test.
		t.Logf("found %s (%s)", a.Name, a.Detail)
	}
}
