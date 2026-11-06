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
	"strings"
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
	// VideoToolbox's needs FFmpeg 6.1 or later; older ones decode on the
	// CPU (the startup test of the full pipeline fails).
	VideoToolbox: "scale_vt",
}

// decodes lists the source codecs (as ffprobe names them) each backend's
// full pipeline decodes on the GPU. Older GPUs lack some of them; FFmpeg
// then fails quickly and the transcode falls back (see Plan). Apple silicon
// doesn't decode MPEG-2 or MPEG-4 Part 2 (tested on an M5 Pro).
var decodes = map[Kind][]string{
	VAAPI:        {"h264", "hevc", "vp9", "av1", "mpeg2video", "vc1", "vp8"},
	NVENC:        {"h264", "hevc", "vp9", "av1", "mpeg2video", "vc1", "vp8", "mpeg4"},
	QSV:          {"h264", "hevc", "vp9", "av1", "mpeg2video", "vc1", "vp8"},
	VideoToolbox: {"h264", "hevc", "vp9", "av1", "prores"},
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
	// BurnInput is the input BurnSub is in: 0 for the video itself, 1 for
	// a picture subtitle file next to it, given as a second input.
	BurnInput int
	// Styled, when set, is a libass filter that draws ASS/SSA subtitles
	// with their styling into the picture (see stream.StyledFilter). Like
	// BurnSub it needs the frames in memory.
	Styled string
	// MaxWidth caps the width; narrower video keeps its size.
	MaxWidth int
	// KeyframeSeconds forces a keyframe this often, so HLS segments start
	// on one. 0 leaves it to the encoder.
	KeyframeSeconds int
	// Renditions, when set, are the pictures to make instead of one at
	// MaxWidth with the default rates. Several make an adaptive stream:
	// the video is decoded once and split, each copy resized and encoded
	// on its own, as video streams 0, 1 and so on.
	Renditions []Rendition
	// ToneMap, when set, is the filter chain that turns an HDR picture into
	// SDR, on frames in memory (see stream.ToneMap). It runs after the
	// resize, on the smaller picture; frames decoded on a GPU come back to
	// memory for it and go up again for the encoder.
	ToneMap string
	// ToneMapInput is what ToneMap needs before -i, such as a Vulkan device.
	ToneMapInput []string
}

// Rendition is one picture size and bitrate a transcode makes.
type Rendition struct {
	MaxWidth int
	// MaxRate is the peak video bitrate in bits per second; 0 uses the
	// default rates.
	MaxRate int
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
	if a.Decode && v.BurnSub < 0 && v.Styled == "" && slices.Contains(decodes[a.Kind], v.Codec) {
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
	renditions := v.Renditions
	if len(renditions) == 0 {
		renditions = []Rendition{{MaxWidth: v.MaxWidth}}
	}
	keyframes := []string{}
	if v.KeyframeSeconds > 0 {
		keyframes = []string{"-force_key_frames", fmt.Sprintf("expr:gte(t,n_forced*%d)", v.KeyframeSeconds)}
	}
	input, chain := filters(a, m, v.Codec, v.ToneMap)
	input = append(input, v.ToneMapInput...)
	chains := make([]string, len(renditions))
	var enc []string
	for i, r := range renditions {
		chains[i] = chain(r.MaxWidth)
		e := append(encodeArgs(a, m, r.MaxRate), keyframes...)
		if len(renditions) > 1 {
			e = perStream(e, i)
		}
		enc = append(enc, e...)
	}
	return input, append(videoFilter(v, chains), enc...)
}

// filters returns the input arguments for mode m on a, and the filter chain
// that resizes a frame to fit a width and hands it to the encoder.
func filters(a *Accel, m Mode, codec, toneMap string) (input []string, chain func(width int) string) {
	scale := func(w int) string {
		f := fmt.Sprintf("scale='min(%d,iw)':-2", w)
		if toneMap != "" {
			f += "," + toneMap
		}
		return f
	}
	if m == CPU {
		return nil, scale
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
	chain = func(w int) string { return scale(w) + "," + upload }
	if m != Full {
		return input, chain
	}
	// Tone mapping runs in memory: the GPU resizes, keeping 10 bits, and
	// the smaller frames come down for it.
	down := func(gpuScale, format, up string) func(int) string {
		return func(w int) string {
			if toneMap == "" {
				return fmt.Sprintf(gpuScale, w, format)
			}
			f := fmt.Sprintf(gpuScale, w, "p010") + ",hwdownload,format=p010le," + toneMap
			if up != "" {
				f += "," + up
			}
			return f
		}
	}
	switch k {
	case VAAPI:
		input = []string{"-hwaccel", "vaapi", "-hwaccel_device", a.Device, "-hwaccel_output_format", "vaapi"}
		chain = down("scale_vaapi=w='min(%d,iw)':h=-2:format=%s", "nv12", "format=nv12,hwupload")
		if toneMap != "" {
			// The tone-mapped frames go back up to the same device.
			input = []string{"-init_hw_device", "vaapi=va:" + a.Device, "-hwaccel", "vaapi", "-hwaccel_device", "va",
				"-hwaccel_output_format", "vaapi", "-filter_hw_device", "va"}
		}
	case NVENC:
		input = []string{"-hwaccel", "cuda", "-hwaccel_output_format", "cuda"}
		chain = down("scale_cuda=w='min(%d,iw)':h=-2:format=%s", "yuv420p", "")
	case QSV:
		input = []string{"-hwaccel", "qsv"}
		if a.Device != "" {
			input = append(input, "-qsv_device", a.Device)
		}
		input = append(input, "-hwaccel_output_format", "qsv", "-c:v", qsvDecoder[codec])
		chain = down("scale_qsv=w='min(%d,iw)':h=-2:format=%s", "nv12", "format=nv12")
	case VideoToolbox:
		// Requiring VideoToolbox's own frames makes a decode the GPU can't
		// do fail, rather than quietly running on the CPU, so the startup
		// test proves it and a transcode falls back to Encode. The encoder
		// takes the resized frames as they are.
		input = []string{"-hwaccel", "videotoolbox", "-hwaccel_output_format", "videotoolbox_vld"}
		chain = func(w int) string {
			f := fmt.Sprintf("scale_vt=w='min(%d,iw)':h=-2", w)
			if toneMap != "" {
				// Frames come down in the source's depth: 10 bits for
				// nearly all HDR, 8 for the odd 8-bit HLG file.
				f += ",hwdownload,format=p010le|nv12," + toneMap
			}
			return f
		}
	case AMF:
		input = []string{"-hwaccel", "auto"}
	}
	return input, chain
}

// encodeArgs returns the encoder and its rate options for mode m on a, peaking
// at maxRate bits per second (0 for the defaults).
func encodeArgs(a *Accel, m Mode, maxRate int) []string {
	if m == CPU {
		peak, buf := maxrate, bufsize
		if maxRate > 0 {
			peak, buf = strconv.Itoa(maxRate), strconv.Itoa(2*maxRate)
		}
		return []string{"-c:v", "libx264", "-preset", "veryfast", "-crf", "22",
			"-maxrate", peak, "-bufsize", buf, "-pix_fmt", "yuv420p"}
	}
	b, peak, buf := bitrate, maxrate, bufsize
	if maxRate > 0 {
		b, peak, buf = strconv.Itoa(maxRate*4/5), strconv.Itoa(maxRate), strconv.Itoa(2*maxRate)
	}
	k := a.Kind
	switch k {
	case VAAPI:
		return []string{"-c:v", encoder[k], "-rc_mode", "VBR", "-b:v", b, "-maxrate", peak, "-bufsize", buf,
			"-profile:v", "high"}
	case NVENC:
		return []string{"-c:v", encoder[k], "-preset", "p4", "-tune", "hq", "-rc", "vbr", "-cq", "23",
			"-b:v", b, "-maxrate", peak, "-bufsize", buf, "-profile:v", "high", "-forced-idr", "1"}
	case QSV:
		return []string{"-c:v", encoder[k], "-preset", "veryfast", "-b:v", b, "-maxrate", peak, "-bufsize", buf,
			"-profile:v", "high", "-forced_idr", "1"}
	case VideoToolbox:
		return []string{"-c:v", encoder[k], "-b:v", b, "-profile:v", "high"}
	case AMF:
		return []string{"-c:v", encoder[k], "-usage", "transcoding", "-quality", "speed", "-rc", "vbr_peak",
			"-b:v", b, "-maxrate", peak, "-bufsize", buf, "-profile:v", "high"}
	case V4L2:
		return []string{"-c:v", encoder[k], "-b:v", b}
	}
	return nil
}

// perStream points each option in opts at output video stream i only
// (-b:v becomes -b:v:1, -preset becomes -preset:v:1).
func perStream(opts []string, i int) []string {
	out := make([]string, len(opts))
	for j, o := range opts {
		switch {
		case j%2 == 1:
			out[j] = o // a value
		case strings.HasSuffix(o, ":v"):
			out[j] = o + ":" + strconv.Itoa(i)
		default:
			out[j] = o + ":v:" + strconv.Itoa(i)
		}
	}
	return out
}

// videoFilter maps the video through each chain, drawing in a burned-in
// subtitle first. Several chains split the picture, one copy for each.
func videoFilter(v Video, chains []string) []string {
	src, graph := "[0:v:0]", ""
	if v.BurnSub >= 0 {
		// Picture subtitles are drawn at the video's size (their canvas can
		// differ, e.g. 1080p subtitles on a cropped 800-line video).
		graph = "[" + strconv.Itoa(v.BurnInput) + ":s:" + strconv.Itoa(v.BurnSub) + "][0:v:0]scale2ref[sub][vid];[vid][sub]overlay=eof_action=pass"
		if len(chains) == 1 {
			return []string{"-filter_complex", graph + "," + chains[0] + "[v]", "-map", "[v]"}
		}
		src, graph = "", graph+","
	} else if v.Styled != "" {
		if len(chains) == 1 {
			return []string{"-map", "0:v:0", "-vf", v.Styled + "," + chains[0]}
		}
		src = "[0:v:0]" + v.Styled + ","
	} else if len(chains) == 1 {
		return []string{"-map", "0:v:0", "-vf", chains[0]}
	}
	var maps []string
	graph += src + "split=" + strconv.Itoa(len(chains))
	for i := range chains {
		graph += fmt.Sprintf("[s%d]", i)
	}
	for i, c := range chains {
		graph += fmt.Sprintf(";[s%d]%s[v%d]", i, c, i)
		maps = append(maps, "-map", fmt.Sprintf("[v%d]", i))
	}
	return append([]string{"-filter_complex", graph}, maps...)
}
