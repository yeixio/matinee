package stream

import (
	"context"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"time"
)

// Tone mapping turns an HDR picture into SDR when it's converted: without
// it, HDR shown as SDR looks flat, grey and washed out. Every conversion
// makes 8-bit SDR H.264, so every HDR picture converted is tone mapped when
// FFmpeg has the tools for it (zscale, or libplacebo). Without them, such as
// with Homebrew's FFmpeg, the picture is converted as is and Settings says
// that tone mapping isn't available.

// The ways to tone map an admin can pick, and ToneMapOff for none.
const (
	ToneMapOff      = "off"
	ToneMapHable    = "hable"
	ToneMapMobius   = "mobius"
	ToneMapReinhard = "reinhard"
)

// ToneMapAlgorithms are the ways to tone map, the default first.
var ToneMapAlgorithms = []string{ToneMapHable, ToneMapMobius, ToneMapReinhard}

// ToneTools is what this server's FFmpeg can tone map with.
type ToneTools struct {
	// Zscale converts HDR10 and HLG (FFmpeg built with zimg).
	Zscale bool `json:"zscale"`
	// Placebo also converts Dolby Vision with no HDR10 layer under it
	// (profile 5), which needs the Dolby Vision metadata applied
	// (libplacebo, on a Vulkan device).
	Placebo bool `json:"placebo"`
}

var (
	toneOnce  sync.Once
	toneFound ToneTools
	dvOnce    sync.Once
	dvStrip   bool
)

// canStripDV says whether the transcoder's FFmpeg can take Dolby Vision
// metadata out of a picture it copies (dovi_rpu, FFmpeg 7.1 and later).
func (t *Transcoder) canStripDV() bool {
	dvOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, t.FFmpeg, "-hide_banner", "-bsfs").Output()
		dvStrip = err == nil && slices.Contains(strings.Fields(string(out)), "dovi_rpu")
	})
	return dvStrip
}

// ToneTools finds what the transcoder's FFmpeg can tone map with, once.
func (t *Transcoder) ToneTools() ToneTools {
	toneOnce.Do(func() { toneFound = findToneTools(t.FFmpeg) })
	return toneFound
}

func findToneTools(ffmpeg string) ToneTools {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, ffmpeg, "-hide_banner", "-filters").Output()
	if err != nil {
		return ToneTools{}
	}
	has := func(name string) bool {
		for _, l := range strings.Split(string(out), "\n") {
			if f := strings.Fields(l); len(f) >= 2 && f[1] == name {
				return true
			}
		}
		return false
	}
	tools := ToneTools{Zscale: has("zscale") && has("tonemap")}
	if has("libplacebo") {
		// It needs a Vulkan device, which a GPU or a software one gives.
		test := exec.CommandContext(ctx, ffmpeg, "-hide_banner", "-loglevel", "error", "-init_hw_device", "vulkan",
			"-f", "lavfi", "-i", "color=c=black:s=64x64:d=0.1", "-vf", "libplacebo=colorspace=bt709:format=yuv420p", "-f", "null", "-")
		tools.Placebo = test.Run() == nil
	}
	return tools
}

// ToneMap is the filter chain that tone maps pic with algorithm (see
// ToneMapAlgorithms), and what it needs before -i; "" when the picture
// isn't HDR, tone mapping is off, or FFmpeg can't.
func ToneMap(pic Picture, algorithm string, tools ToneTools) (chain string, input []string) {
	if !pic.Known || algorithm == ToneMapOff || algorithm == "" {
		return "", nil
	}
	hdr := pic.HDR
	if hdr == "dolby-vision" {
		if pic.HDRBase == "" {
			// Profile 5: its colours are Dolby's own, only right with its metadata applied.
			if !tools.Placebo {
				return "", nil
			}
			return "libplacebo=tonemapping=" + algorithm + ":colorspace=bt709:color_primaries=bt709:color_trc=bt709:range=tv:format=yuv420p",
				[]string{"-init_hw_device", "vulkan"}
		}
		// Profiles 7 and 8 carry an HDR10 or HLG picture underneath.
		hdr = pic.HDRBase
	}
	var transfer string
	switch hdr {
	case "hdr10", "hdr10+":
		transfer = "smpte2084"
	case "hlg":
		transfer = "arib-std-b67"
	default:
		return "", nil
	}
	if !tools.Zscale {
		return "", nil
	}
	// To linear light, then BT.709's colours, the tone curve, and back to
	// BT.709's gamma in 8 bits.
	return "zscale=tin=" + transfer + ":min=bt2020nc:pin=bt2020:t=linear:npl=100,format=gbrpf32le,zscale=p=bt709," +
		"tonemap=tonemap=" + algorithm + ":desat=0,zscale=t=bt709:m=bt709:r=tv,format=yuv420p", nil
}
