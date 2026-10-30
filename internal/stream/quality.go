package stream

import (
	"fmt"
	"strings"

	"github.com/yeixio/matinee/internal/hwaccel"
	"github.com/yeixio/matinee/internal/store"
)

// Step is one picture quality a transcode can make. A transcode made of
// several steps is an adaptive stream: players move between them as the
// connection allows.
type Step struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	// Width caps the picture's width; narrower video keeps its size.
	Width  int `json:"width"`
	Height int `json:"height"`
	// VideoBPS is the peak video bitrate and AudioBPS the audio's, in bits
	// per second.
	VideoBPS int `json:"videoBps"`
	AudioBPS int `json:"audioBps"`
}

// Steps are every quality Matinee offers, best first. An admin picks which
// ones adaptive streams use.
var Steps = []Step{
	{ID: "1080p", Label: "1080p", Width: 1920, Height: 1080, VideoBPS: 8_000_000, AudioBPS: 192_000},
	{ID: "720p", Label: "720p", Width: 1280, Height: 720, VideoBPS: 4_000_000, AudioBPS: 128_000},
	{ID: "480p", Label: "480p", Width: 854, Height: 480, VideoBPS: 1_500_000, AudioBPS: 96_000},
	{ID: "360p", Label: "360p", Width: 640, Height: 360, VideoBPS: 700_000, AudioBPS: 96_000},
}

// StepByID finds one of Steps.
func StepByID(id string) (Step, bool) {
	for _, s := range Steps {
		if s.ID == id {
			return s, true
		}
	}
	return Step{}, false
}

// maxCPURenditions is how many steps an adaptive stream makes on the CPU.
// Each one is a separate encode, and a CPU that can't keep up with all of
// them stalls every viewer.
const maxCPURenditions = 2

// Ladder picks the steps an adaptive stream of it makes, best first: the
// ones offered that aren't bigger than the file itself. A file smaller
// than every step still gets the smallest. onCPU keeps it to the best step
// and one from the middle, so there's somewhere to step down to.
func Ladder(it store.Item, offered []Step, onCPU bool) []Step {
	var out []Step
	for i, s := range offered {
		// A step wider than the file would only add bitrate. Widths, not
		// heights, so a wide 1920x800 film still counts as 1080p.
		if it.Width != nil && *it.Width > 0 && i+1 < len(offered) && offered[i+1].Width >= *it.Width {
			continue
		}
		out = append(out, s)
	}
	if onCPU && len(out) > maxCPURenditions {
		out = []Step{out[0], out[len(out)/2]}
	}
	return out
}

// Playlist is the playlist a transcode of steps starts from: a master
// playlist naming one playlist per step for an adaptive stream, or the
// single stream's own.
func Playlist(steps []Step) string {
	if len(steps) > 1 {
		return "master.m3u8"
	}
	return "index.m3u8"
}

// renditions turns steps into what hwaccel builds encodes from.
func renditions(steps []Step) []hwaccel.Rendition {
	var out []hwaccel.Rendition
	for _, s := range steps {
		out = append(out, hwaccel.Rendition{MaxWidth: s.Width, MaxRate: s.VideoBPS})
	}
	return out
}

// fits says whether the original H.264 picture can be sent as it is at
// step, judging its bitrate from the file's size (an overestimate, since
// the size includes the audio).
func fits(it store.Item, step Step) bool {
	if it.VideoCodec != "h264" || it.Width == nil || *it.Width > step.Width || it.DurationMs == nil || *it.DurationMs <= 0 {
		return false
	}
	bps := it.Size * 8 * 1000 / *it.DurationMs
	return bps <= int64(step.VideoBPS)*115/100
}

// masterPlaylist lists one variant per step for it, so players know each
// one's bitrate and size before fetching any.
func masterPlaylist(it store.Item, steps []Step, audio bool) []byte {
	var b strings.Builder
	b.WriteString("#EXTM3U\n#EXT-X-VERSION:3\n")
	for i, s := range steps {
		bandwidth, codecs := s.VideoBPS, "avc1.640028"
		if audio {
			bandwidth += s.AudioBPS
			codecs += ",mp4a.40.2"
		}
		fmt.Fprintf(&b, "#EXT-X-STREAM-INF:BANDWIDTH=%d,CODECS=\"%s\"", bandwidth, codecs)
		if w, h, ok := size(it, s.Width); ok {
			fmt.Fprintf(&b, ",RESOLUTION=%dx%d", w, h)
		}
		fmt.Fprintf(&b, ",NAME=\"%s\"\nindex_%d.m3u8\n", s.Label, i)
	}
	return []byte(b.String())
}

// size is the picture size it comes out at when capped to maxWidth, the
// way FFmpeg's scale='min(w,iw)':-2 works it out.
func size(it store.Item, maxWidth int) (w, h int, ok bool) {
	if it.Width == nil || it.Height == nil || *it.Width <= 0 || *it.Height <= 0 {
		return 0, 0, false
	}
	w, h = *it.Width, *it.Height
	if w > maxWidth {
		h = (h*maxWidth/w + 1) / 2 * 2
		w = maxWidth
	}
	return w, h, true
}
