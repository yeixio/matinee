package stream

import (
	"slices"
	"strings"

	"github.com/yeixio/matinee/internal/store"
)

// Plays is what a player takes as it is inside the server's HLS: the video
// codecs it decodes and the kinds of HDR its screen shows. A player that
// says nothing gets H.264 in SDR, which every HLS player plays.
type Plays struct {
	Video []string // h264, hevc, av1
	HDR   []string // hdr10, hdr10+, hlg, dolby-vision
}

// VideoCodecs and HDRKinds are the values Plays may hold.
var (
	VideoCodecs = []string{"h264", "hevc", "av1"}
	HDRKinds    = []string{"hdr10", "hdr10+", "hlg", "dolby-vision"}
)

// ParsePlays reads Plays from comma-separated lists, as in ?video=h264,hevc&hdr=hdr10.
// Values Matinee doesn't know are left out.
func ParsePlays(video, hdr string) Plays {
	pick := func(list string, known []string) []string {
		var out []string
		for _, v := range strings.Split(strings.ToLower(list), ",") {
			if v = strings.TrimSpace(v); slices.Contains(known, v) && !slices.Contains(out, v) {
				out = append(out, v)
			}
		}
		return out
	}
	return Plays{Video: pick(video, VideoCodecs), HDR: pick(hdr, HDRKinds)}
}

func (p Plays) codec(c string) bool { return c == "h264" || slices.Contains(p.Video, c) }

// shows says whether the screen shows this HDR. HDR10+ falls back to HDR10,
// and Dolby Vision to the layer under it, when the file has one.
func (p Plays) shows(hdr, base string) bool {
	switch hdr {
	case "":
		return true
	case "hdr10+":
		return slices.Contains(p.HDR, "hdr10+") || slices.Contains(p.HDR, "hdr10")
	case "dolby-vision":
		return slices.Contains(p.HDR, "dolby-vision") || base != "" && p.shows(base, "")
	}
	return slices.Contains(p.HDR, hdr)
}

// Picture is what the closer look at a file (media analysis) found about
// its video. Until it has looked, Known is false and the picture may be
// HDR of any kind.
type Picture struct {
	Known        bool
	HDR, HDRBase string
	// DVProfile is the Dolby Vision profile (5, 7, 8 or 10), 0 when it isn't.
	DVProfile int
}

// shownAs is the HDR a screen has to show for the picture to look right.
// Profile 7 (two layers, from discs) plays as its HDR10 base even on a Dolby
// Vision screen: almost no player decodes its second layer.
func (p Picture) shownAs() (hdr, base string) {
	if p.HDR == "dolby-vision" && p.DVProfile == 7 {
		return p.HDRBase, ""
	}
	return p.HDR, p.HDRBase
}

// keepsDolbyVision says whether a copied picture goes to the player with
// its Dolby Vision: the file has it, the screen shows it, and it isn't
// profile 7, which plays as its base.
func keepsDolbyVision(pic Picture, can Plays) bool {
	return pic.HDR == "dolby-vision" && pic.DVProfile != 7 && slices.Contains(can.HDR, "dolby-vision")
}

// copyArgs are FFmpeg's output options for a picture copied as it is.
// stripDV says FFmpeg can take the Dolby Vision metadata out (its dovi_rpu
// filter, FFmpeg 7.1 and later).
func copyArgs(codec string, pic Picture, can Plays, stripDV bool) []string {
	out := []string{"-map", "0:v:0", "-c:v", "copy"}
	keep := keepsDolbyVision(pic, can)
	if codec == "hevc" {
		// Apple's players only take HEVC tagged hvc1 (parameter sets in
		// the sample description, not hev1's in the stream), or dvh1 for
		// Dolby Vision with nothing under it (profile 5), which only a
		// Dolby Vision player shows.
		tag := "hvc1"
		if keep && pic.HDRBase == "" {
			tag = "dvh1"
		}
		out = append(out, "-tag:v", tag)
	}
	switch {
	case keep:
		// FFmpeg 7 and later only write the Dolby Vision configuration
		// (dvcC) players look for when told to.
		out = append(out, "-strict", "unofficial")
	case pic.HDR == "dolby-vision" && stripDV:
		// A screen showing the HDR10 or HLG under it gets just that, so
		// no player mistakes it for Dolby Vision it can't show.
		out = append(out, "-bsf:v", "dovi_rpu=strip=1")
	}
	return out
}

// Why a transcode converts the picture rather than copying it.
const (
	ConvertCodec    = "codec"    // the player doesn't decode this video codec
	ConvertHDR      = "hdr"      // the screen doesn't show this HDR
	ConvertSubtitle = "subtitle" // a picture subtitle is drawn into the video
	ConvertQuality  = "quality"  // a lower quality, or an adaptive stream, was asked for
)

// videoPlan says whether a transcode copies the file's picture, and when it
// doesn't, why. The picture is copied (repackaged) when the player decodes
// its codec, its screen shows its HDR, nothing has to be drawn into it and
// it fits the quality asked for.
//
// H.264 is copied whatever its HDR, as Matinee always has: HDR in H.264 is
// rare, and every player takes H.264.
func videoPlan(it store.Item, pic Picture, burnSub int, steps []Step, can Plays) (copy bool, why string) {
	switch {
	case !can.codec(it.VideoCodec):
		return false, ConvertCodec
	case it.VideoCodec != "h264" && !pic.Known && !(can.shows("hdr10", "") && can.shows("hlg", "")):
		// Not looked at yet: only a screen that shows HDR10 and HLG is
		// sure to show it right. Analysis usually follows a scan closely.
		return false, ConvertHDR
	case it.VideoCodec != "h264" && !can.shows(pic.shownAs()):
		// Profile 5 has nothing under its Dolby Vision, so a screen
		// without it always gets it converted.
		return false, ConvertHDR
	case burnSub >= 0:
		return false, ConvertSubtitle
	case len(steps) > 1 || len(steps) == 1 && !fits(it, steps[0]):
		return false, ConvertQuality
	}
	return true, ""
}

// fragmented says whether a copied picture goes in fMP4 pieces rather
// than MPEG-TS, which only carries H.264 well for every player.
func fragmented(codec string, copy bool) bool { return copy && codec != "h264" }
