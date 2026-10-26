// Package hwaccel finds the video hardware FFmpeg can use on this machine and
// builds the FFmpeg arguments that put a transcode on it.
//
// Only some of these backends run on hardware the maintainers own, so the
// argument building is covered by unit tests for every backend; detection at
// startup (see detect.go) then proves a backend works on the machine at hand
// before Matinee relies on it.
package hwaccel

import (
	"fmt"
	"slices"
	"strconv"
)

// Kind names a hardware video backend. The values are stored as the
// transcoding setting and used in the API.
type Kind string

const (
	VAAPI        Kind = "vaapi"        // AMD and Intel GPUs on Linux
	NVENC        Kind = "nvenc"        // NVIDIA GPUs (NVENC to encode, NVDEC to decode)
	QSV          Kind = "qsv"          // Intel Quick Sync
	VideoToolbox Kind = "videotoolbox" // Macs
	AMF          Kind = "amf"          // AMD GPUs on Windows
	V4L2         Kind = "v4l2m2m"      // Raspberry Pi and other ARM boards
)

// Kinds lists every backend in the order Matinee prefers them when it picks
// one itself. Detection only tries the ones that make sense on the running OS.
var Kinds = []Kind{NVENC, VAAPI, QSV, VideoToolbox, AMF, V4L2}

var names = map[Kind]string{
	VAAPI:        "VAAPI",
	NVENC:        "NVIDIA NVENC",
	QSV:          "Intel Quick Sync",
	VideoToolbox: "VideoToolbox",
	AMF:          "AMD AMF",
	V4L2:         "V4L2",
}

// Name is the backend's name as people know it.
func (k Kind) Name() string { return names[k] }

// Valid says whether k is a backend Matinee knows.
func (k Kind) Valid() bool { _, ok := names[k]; return ok }

// encoder is each backend's H.264 encoder in FFmpeg.
var encoder = map[Kind]string{
	VAAPI:        "h264_vaapi",
	NVENC:        "h264_nvenc",
	QSV:          "h264_qsv",
	VideoToolbox: "h264_videotoolbox",
	AMF:          "h264_amf",
	V4L2:         "h264_v4l2m2m",
}

// gpuScaler is the filter that resizes frames without leaving the GPU, for
// the backends whose full pipeline keeps frames there.
var gpuScaler = map[Kind]string{
	VAAPI: "scale_vaapi",
	NVENC: "scale_cuda",
	QSV:   "scale_qsv",
}

// decodes lists the source codecs (as ffprobe names them) each backend's
// full pipeline decodes on the GPU. Older GPUs lack some of them; FFmpeg
// then fails quickly and the transcode falls back (see Plan).
var decodes = map[Kind][]string{
	VAAPI:        {"h264", "hevc", "vp9", "av1", "mpeg2video", "vc1", "vp8"},
	NVENC:        {"h264", "hevc", "vp9", "av1", "mpeg2video", "vc1", "vp8", "mpeg4"},
	QSV:          {"h264", "hevc", "vp9", "av1", "mpeg2video", "vc1", "vp8"},
	VideoToolbox: {"h264", "hevc", "vp9", "av1", "mpeg2video", "mpeg4", "prores"},
	AMF:          {"h264", "hevc", "vp9", "av1", "mpeg2video", "vc1"},
}

// QSV's decoders are separate codecs in FFmpeg, named after the source codec.
var qsvDecoder = map[string]string{
	"h264": "h264_qsv", "hevc": "hevc_qsv", "vp9": "vp9_qsv", "av1": "av1_qsv",
	"mpeg2video": "mpeg2_qsv", "vc1": "vc1_qsv", "vp8": "vp8_qsv",
}

// Accel is a backend that works on this machine.
type Accel struct {
	Kind Kind   `json:"id"`
	Name string `json:"name"`
	// Device is the GPU's render node (/dev/dri/renderD128) for VAAPI, and
	// for Quick Sync on Linux.
	Device string `json:"device,omitempty"`
	// Detail describes the GPU when the driver says, such as "AMD Radeon RX 7900 XTX".
	Detail string `json:"detail,omitempty"`
	// Decode means the full pipeline works too: decoding (and, where the
	// backend can, resizing) on the GPU, not only encoding.
	Decode bool `json:"decode"`
}

// Mode is how much of a transcode the hardware does.
type Mode int

const (
	CPU    Mode = iota // all in software
	Encode             // software decode and resize, hardware encode
	Full               // hardware decode, resize where the backend can, and encode
)

func (m Mode) String() string {
	return [...]string{"cpu", "encode", "full"}[m]
}

// Video describes the picture a stream transcode makes.
type Video struct {
	// Codec is the source's video codec, as ffprobe names it.
	Codec string
	// BurnSub is an embedded picture subtitle stream to draw into the
	// video, or -1 for none.
	BurnSub int
	// MaxWidth caps the width; narrower video keeps its size.
	MaxWidth int
	// KeyframeSeconds forces a keyframe this often, so HLS segments start
	// on one. 0 leaves it to the encoder.
	KeyframeSeconds int
}

// Rates for hardware encoders, which have no constant-quality mode common to
// all of them. They match the CPU encode's cap.
const (
	bitrate = "8M"
	maxrate = "10M"
	bufsize = "20M"
)

// Plan lists the modes to try for v on a, best first. A transcode that fails
// to start in one mode starts again in the next; the last is always CPU.
func Plan(a *Accel, v Video) []Mode {
	if a == nil {
		return []Mode{CPU}
	}
	if a.Decode && v.BurnSub < 0 && slices.Contains(decodes[a.Kind], v.Codec) {
		return []Mode{Full, Encode, CPU}
	}
	return []Mode{Encode, CPU}
}

// Args returns the FFmpeg arguments for v's video in mode m on a: input goes
// before -i, output after it (the video's -map, filters and encoder; the
// caller adds audio and the muxer).
func Args(a *Accel, m Mode, v Video) (input, output []string) {
	if a == nil {
		m = CPU
	}
	scale := fmt.Sprintf("scale='min(%d,iw)':-2", v.MaxWidth)
	keyframes := []string{}
	if v.KeyframeSeconds > 0 {
		keyframes = []string{"-force_key_frames", fmt.Sprintf("expr:gte(t,n_forced*%d)", v.KeyframeSeconds)}
	}

	if m == CPU {
		enc := []string{"-c:v", "libx264", "-preset", "veryfast", "-crf", "22",
			"-maxrate", maxrate, "-bufsize", bufsize, "-pix_fmt", "yuv420p"}
		return nil, append(videoFilter(v, scale), append(enc, keyframes...)...)
	}

	k := a.Kind
	// The software filters' last step hands the encoder frames in a format
	// it takes: VAAPI's encoder only takes frames already on the GPU.
	upload := "format=yuv420p"
	switch k {
	case VAAPI:
		upload = "format=nv12,hwupload"
		input = []string{"-init_hw_device", "vaapi=va:" + a.Device, "-filter_hw_device", "va"}
	case QSV:
		upload = "format=nv12"
	}
	chain := scale + "," + upload

	if m == Full {
		switch k {
		case VAAPI:
			input = []string{"-hwaccel", "vaapi", "-hwaccel_device", a.Device, "-hwaccel_output_format", "vaapi"}
			chain = fmt.Sprintf("scale_vaapi=w='min(%d,iw)':h=-2:format=nv12", v.MaxWidth)
		case NVENC:
			input = []string{"-hwaccel", "cuda", "-hwaccel_output_format", "cuda"}
			chain = fmt.Sprintf("scale_cuda=w='min(%d,iw)':h=-2:format=yuv420p", v.MaxWidth)
		case QSV:
			input = []string{"-hwaccel", "qsv"}
			if a.Device != "" {
				input = append(input, "-qsv_device", a.Device)
			}
			input = append(input, "-hwaccel_output_format", "qsv", "-c:v", qsvDecoder[v.Codec])
			chain = fmt.Sprintf("scale_qsv=w='min(%d,iw)':h=-2:format=nv12", v.MaxWidth)
		case VideoToolbox:
			// Decoded frames come back to memory for the software resize,
			// which is cheap next to decoding.
			input = []string{"-hwaccel", "videotoolbox"}
		case AMF:
			input = []string{"-hwaccel", "auto"}
		}
	}

	var enc []string
	switch k {
	case VAAPI:
		enc = []string{"-c:v", encoder[k], "-rc_mode", "VBR", "-b:v", bitrate, "-maxrate", maxrate, "-bufsize", bufsize,
			"-profile:v", "high"}
	case NVENC:
		enc = []string{"-c:v", encoder[k], "-preset", "p4", "-tune", "hq", "-rc", "vbr", "-cq", "23",
			"-b:v", bitrate, "-maxrate", maxrate, "-bufsize", bufsize, "-profile:v", "high", "-forced-idr", "1"}
	case QSV:
		enc = []string{"-c:v", encoder[k], "-preset", "veryfast", "-b:v", bitrate, "-maxrate", maxrate, "-bufsize", bufsize,
			"-profile:v", "high", "-forced_idr", "1"}
	case VideoToolbox:
		enc = []string{"-c:v", encoder[k], "-b:v", bitrate, "-profile:v", "high"}
	case AMF:
		enc = []string{"-c:v", encoder[k], "-usage", "transcoding", "-quality", "speed", "-rc", "vbr_peak",
			"-b:v", bitrate, "-maxrate", maxrate, "-bufsize", bufsize, "-profile:v", "high"}
	case V4L2:
		enc = []string{"-c:v", encoder[k], "-b:v", bitrate}
	}
	return input, append(videoFilter(v, chain), append(enc, keyframes...)...)
}

// videoFilter maps the video through chain, drawing in a burned-in subtitle first.
func videoFilter(v Video, chain string) []string {
	if v.BurnSub >= 0 {
		// Picture subtitles are drawn at the video's size (their canvas can
		// differ, e.g. 1080p subtitles on a cropped 800-line video).
		graph := "[0:s:" + strconv.Itoa(v.BurnSub) + "][0:v:0]scale2ref[sub][vid];[vid][sub]overlay=eof_action=pass," + chain + "[v]"
		return []string{"-filter_complex", graph, "-map", "[v]"}
	}
	return []string{"-map", "0:v:0", "-vf", chain}
}
