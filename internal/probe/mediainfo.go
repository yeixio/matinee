package probe

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"time"
)

// MediaInfoVersion goes up when Analyze learns something new, so files
// analyzed before are looked at again.
const MediaInfoVersion = 2

// MediaInfo is everything worth knowing about a file's streams, from a
// slower and more thorough look than the scanner's.
type MediaInfo struct {
	Container  string        `json:"container"`
	Size       int64         `json:"size"`
	DurationMs int64         `json:"durationMs"`
	Bitrate    int64         `json:"bitrate,omitempty"` // bits per second, all streams
	Video      []VideoStream `json:"video"`
	Audio      []AudioStream `json:"audio"`
	Subtitles  []Subtitle    `json:"subtitles"`
	Chapters   int           `json:"chapters,omitempty"`
	// CreditsMs is where the closing credits start, when a chapter says so.
	// Players offer the next episode from there.
	CreditsMs  int64  `json:"creditsMs,omitempty"`
	Error      string `json:"error,omitempty"` // set when ffprobe couldn't read the file
	AnalyzedAt int64  `json:"analyzedAt"`      // Unix seconds
}

type VideoStream struct {
	Index          int     `json:"index"` // among the file's video streams, as in FFmpeg's 0:v:N
	Codec          string  `json:"codec"`
	Profile        string  `json:"profile,omitempty"`
	Width          int     `json:"width"`
	Height         int     `json:"height"`
	Resolution     string  `json:"resolution"` // 4K, 1080p…, see ResolutionLabel
	FrameRate      float64 `json:"frameRate,omitempty"`
	Bitrate        int64   `json:"bitrate,omitempty"`
	BitDepth       int     `json:"bitDepth,omitempty"`
	PixFmt         string  `json:"pixFmt,omitempty"`
	ColorPrimaries string  `json:"colorPrimaries,omitempty"`
	ColorTransfer  string  `json:"colorTransfer,omitempty"`
	ColorSpace     string  `json:"colorSpace,omitempty"`
	// HDR is "" for SDR, else dolby-vision, hdr10+, hdr10 or hlg. A Dolby
	// Vision file says which HDR10 or HLG layer it falls back to in HDRBase.
	HDR       string `json:"hdr,omitempty"`
	HDRBase   string `json:"hdrBase,omitempty"`
	DVProfile int    `json:"dvProfile,omitempty"`
	DVLevel   int    `json:"dvLevel,omitempty"`
	Language  string `json:"language,omitempty"`
	Title     string `json:"title,omitempty"`
	Default   bool   `json:"default,omitempty"`
}

type AudioStream struct {
	Index         int    `json:"index"` // among the file's audio streams, as in FFmpeg's 0:a:N
	Codec         string `json:"codec"`
	Profile       string `json:"profile,omitempty"`
	Channels      int    `json:"channels"`
	ChannelLayout string `json:"channelLayout,omitempty"`
	SampleRate    int    `json:"sampleRate,omitempty"`
	BitDepth      int    `json:"bitDepth,omitempty"`
	Bitrate       int64  `json:"bitrate,omitempty"`
	Atmos         bool   `json:"atmos,omitempty"`
	DTSX          bool   `json:"dtsx,omitempty"`
	Language      string `json:"language,omitempty"`
	Title         string `json:"title,omitempty"`
	Default       bool   `json:"default,omitempty"`
	Forced        bool   `json:"forced,omitempty"`
	Commentary    bool   `json:"commentary,omitempty"`
	Descriptive   bool   `json:"descriptive,omitempty"` // audio description for the blind
}

// Analyze reads a file's streams in full detail. It runs one ffprobe for the
// streams and, for PQ video only, a second that decodes nothing but reads
// the first frame's side data to tell HDR10+ from HDR10.
func (p Prober) Analyze(ctx context.Context, file string) (MediaInfo, error) {
	out, err := p.run(ctx, file, "-show_format", "-show_streams", "-show_chapters")
	if err != nil {
		return MediaInfo{}, err
	}
	mi, err := parseMediaInfo(out)
	if err != nil {
		return MediaInfo{}, err
	}
	for i, v := range mi.Video {
		if v.ColorTransfer != "smpte2084" || i > 0 {
			continue
		}
		frame, err := p.run(ctx, file, "-select_streams", "v:0", "-read_intervals", "%+#1", "-show_entries", "frame=side_data_list")
		if err == nil {
			applyFrameSideData(&mi.Video[0], frame)
		} else if ctx.Err() != nil {
			return MediaInfo{}, err
		}
	}
	return mi, nil
}

// run starts ffprobe on file with the given extra arguments and returns its
// JSON output. A LowPriority prober gives way to everything else on the
// machine, for CPU and for disk.
func (p Prober) run(ctx context.Context, file string, args ...string) ([]byte, error) {
	timeout := p.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, p.Path, append(append([]string{"-v", "error", "-print_format", "json"}, args...), file)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("ffprobe %s: %w", file, err)
	}
	if p.LowPriority {
		lowerPriority(cmd.Process.Pid)
	}
	if err := cmd.Wait(); err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("ffprobe %s: %w", file, ctx.Err())
		}
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return nil, fmt.Errorf("ffprobe %s: %w: %s", file, err, firstLine(msg))
		}
		return nil, fmt.Errorf("ffprobe %s: %w", file, err)
	}
	return stdout.Bytes(), nil
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}

type sideData struct {
	Type      string `json:"side_data_type"`
	DVProfile int    `json:"dv_profile"`
	DVLevel   int    `json:"dv_level"`
	DVCompat  int    `json:"dv_bl_signal_compatibility_id"`
}

type analyzeOutput struct {
	Format struct {
		FormatName string `json:"format_name"`
		Duration   string `json:"duration"`
		Size       string `json:"size"`
		BitRate    string `json:"bit_rate"`
	} `json:"format"`
	Streams []struct {
		CodecType        string            `json:"codec_type"`
		CodecName        string            `json:"codec_name"`
		CodecTag         string            `json:"codec_tag_string"`
		Profile          string            `json:"profile"`
		Width            int               `json:"width"`
		Height           int               `json:"height"`
		PixFmt           string            `json:"pix_fmt"`
		SampleFmt        string            `json:"sample_fmt"`
		BitsPerRawSample string            `json:"bits_per_raw_sample"`
		BitsPerSample    int               `json:"bits_per_sample"`
		ColorPrimaries   string            `json:"color_primaries"`
		ColorTransfer    string            `json:"color_transfer"`
		ColorSpace       string            `json:"color_space"`
		AvgFrameRate     string            `json:"avg_frame_rate"`
		RFrameRate       string            `json:"r_frame_rate"`
		BitRate          string            `json:"bit_rate"`
		SampleRate       string            `json:"sample_rate"`
		Channels         int               `json:"channels"`
		ChannelLayout    string            `json:"channel_layout"`
		Disposition      map[string]int    `json:"disposition"`
		Tags             map[string]string `json:"tags"`
		SideData         []sideData        `json:"side_data_list"`
	} `json:"streams"`
	Chapters []chapter `json:"chapters"`
}

type chapter struct {
	StartTime string            `json:"start_time"`
	Tags      map[string]string `json:"tags"`
}

func parseMediaInfo(data []byte) (MediaInfo, error) {
	var o analyzeOutput
	if err := json.Unmarshal(data, &o); err != nil {
		return MediaInfo{}, err
	}
	mi := MediaInfo{Video: []VideoStream{}, Audio: []AudioStream{}, Subtitles: []Subtitle{}, Chapters: len(o.Chapters)}
	mi.Container, _, _ = strings.Cut(o.Format.FormatName, ",")
	if d, err := strconv.ParseFloat(o.Format.Duration, 64); err == nil {
		mi.DurationMs = int64(d * 1000)
	}
	mi.Size, _ = strconv.ParseInt(o.Format.Size, 10, 64)
	mi.CreditsMs = creditsStart(o.Chapters, mi.DurationMs)
	mi.Bitrate, _ = strconv.ParseInt(o.Format.BitRate, 10, 64)
	for _, s := range o.Streams {
		switch s.CodecType {
		case "video":
			if s.Disposition["attached_pic"] == 1 {
				continue // cover art
			}
			v := VideoStream{
				Index: len(mi.Video), Codec: s.CodecName, Profile: s.Profile,
				Width: s.Width, Height: s.Height, Resolution: ResolutionLabel(s.Width, s.Height),
				FrameRate: frameRate(s.AvgFrameRate, s.RFrameRate),
				Bitrate:   streamBitrate(s.BitRate, s.Tags),
				BitDepth:  bitDepth(s.BitsPerRawSample, s.PixFmt),
				PixFmt:    s.PixFmt, ColorPrimaries: s.ColorPrimaries, ColorTransfer: s.ColorTransfer, ColorSpace: s.ColorSpace,
				Language: tag(s.Tags, "language"), Title: tag(s.Tags, "title"),
				Default: s.Disposition["default"] == 1,
			}
			switch v.ColorTransfer {
			case "smpte2084":
				v.HDR = "hdr10"
			case "arib-std-b67":
				v.HDR = "hlg"
			}
			for _, sd := range s.SideData {
				if sd.Type == "DOVI configuration record" {
					v.DVProfile, v.DVLevel = sd.DVProfile, sd.DVLevel
					// Profile 8.1 falls back to HDR10, 8.4 to HLG; 5 has no fallback at all.
					v.HDRBase = map[int]string{1: "hdr10", 4: "hlg", 6: "hdr10"}[sd.DVCompat]
					v.HDR = "dolby-vision"
				}
			}
			// MP4s may only say so in the codec tag.
			if v.HDR != "dolby-vision" && slices.Contains([]string{"dvh1", "dvhe", "dva1", "dvav", "dav1"}, s.CodecTag) {
				v.HDRBase, v.HDR = v.HDR, "dolby-vision"
			}
			mi.Video = append(mi.Video, v)
		case "audio":
			title := tag(s.Tags, "title")
			a := AudioStream{
				Index: len(mi.Audio), Codec: s.CodecName, Profile: s.Profile,
				Channels: s.Channels, ChannelLayout: s.ChannelLayout,
				Bitrate:  streamBitrate(s.BitRate, s.Tags),
				Language: tag(s.Tags, "language"), Title: title,
				Default:     s.Disposition["default"] == 1,
				Forced:      s.Disposition["forced"] == 1,
				Commentary:  s.Disposition["comment"] == 1 || strings.Contains(strings.ToLower(title), "commentary"),
				Descriptive: s.Disposition["visual_impaired"] == 1 || s.Disposition["descriptions"] == 1,
			}
			a.SampleRate, _ = strconv.Atoi(s.SampleRate)
			if b, _ := strconv.Atoi(s.BitsPerRawSample); b > 0 {
				a.BitDepth = b
			} else if s.BitsPerSample > 0 {
				a.BitDepth = s.BitsPerSample
			} else if strings.HasPrefix(s.SampleFmt, "s32") && lossless(s.CodecName) {
				a.BitDepth = 24 // FLAC and ALAC decode 24-bit audio to 32-bit samples
			}
			// FFmpeg names these in the profile; muxers often only in the title.
			p, t := strings.ToLower(s.Profile), strings.ToLower(title)
			a.Atmos = strings.Contains(p, "atmos") || strings.Contains(t, "atmos")
			a.DTSX = strings.Contains(p, "dts:x") || strings.Contains(t, "dts:x") || strings.Contains(t, "dts-x")
			mi.Audio = append(mi.Audio, a)
		case "subtitle":
			mi.Subtitles = append(mi.Subtitles, Subtitle{
				Index:    len(mi.Subtitles),
				Codec:    s.CodecName,
				Language: tag(s.Tags, "language"),
				Title:    tag(s.Tags, "title"),
				Default:  s.Disposition["default"] == 1,
				Forced:   s.Disposition["forced"] == 1,
				SDH:      s.Disposition["hearing_impaired"] == 1,
			})
		}
	}
	return mi, nil
}

// applyFrameSideData looks at the first frame's side data for what a stream
// header doesn't say: HDR10+ dynamic metadata, and Dolby Vision in files
// whose container has no configuration record.
func applyFrameSideData(v *VideoStream, data []byte) {
	var o struct {
		Frames []struct {
			SideData []sideData `json:"side_data_list"`
		} `json:"frames"`
	}
	if json.Unmarshal(data, &o) != nil || len(o.Frames) == 0 {
		return
	}
	for _, sd := range o.Frames[0].SideData {
		t := strings.ToLower(sd.Type)
		switch {
		case strings.Contains(t, "smpte2094-40") || strings.Contains(t, "hdr10+"):
			if v.HDR == "dolby-vision" {
				v.HDRBase = "hdr10+"
			} else {
				v.HDR = "hdr10+"
			}
		case strings.Contains(t, "dolby vision") && v.HDR != "dolby-vision":
			v.HDRBase, v.HDR = v.HDR, "dolby-vision"
		}
	}
}

// ResolutionLabel names a picture size the way people talk about it. Width
// counts as much as height, so a 1920×800 widescreen film is still 1080p.
func ResolutionLabel(w, h int) string {
	switch {
	case w == 0 || h == 0:
		return ""
	case w >= 6000 || h >= 3400:
		return "8K"
	case w >= 3200 || h >= 1800:
		return "4K"
	case w >= 1800 || h >= 1000:
		return "1080p"
	case w >= 1200 || h >= 700:
		return "720p"
	case h >= 560:
		return "576p"
	case h >= 470:
		return "480p"
	}
	return "SD"
}

func frameRate(avg, r string) float64 {
	for _, s := range []string{avg, r} {
		num, den, ok := strings.Cut(s, "/")
		n, _ := strconv.ParseFloat(num, 64)
		d := 1.0
		if ok {
			d, _ = strconv.ParseFloat(den, 64)
		}
		if n > 0 && d > 0 {
			return float64(int(n/d*1000+0.5)) / 1000
		}
	}
	return 0
}

// streamBitrate reads a stream's bitrate, or the statistics tag mkvmerge
// writes for the many MKV streams that don't carry one.
func streamBitrate(s string, tags map[string]string) int64 {
	if n, err := strconv.ParseInt(s, 10, 64); err == nil && n > 0 {
		return n
	}
	for _, k := range []string{"BPS", "BPS-eng"} {
		if n, err := strconv.ParseInt(tag(tags, k), 10, 64); err == nil && n > 0 {
			return n
		}
	}
	return 0
}

func bitDepth(raw, pixFmt string) int {
	if n, _ := strconv.Atoi(raw); n > 0 {
		return n
	}
	switch {
	case pixFmt == "":
		return 0
	case strings.Contains(pixFmt, "12"):
		return 12
	case strings.Contains(pixFmt, "10"):
		return 10
	}
	return 8
}

func lossless(codec string) bool {
	return codec == "flac" || codec == "alac" || codec == "wavpack" || codec == "ape" || codec == "tta" ||
		codec == "truehd" || codec == "mlp" || strings.HasPrefix(codec, "pcm_")
}

// Tags sums a file up as short keys that lists can filter on: resolution
// (4k, 1080p…), hdr plus its kind, the video codec, atmos or dts-x, the most
// audio channels (7.1, 5.1, stereo, mono), and lossless or hi-res for music.
// A file ffprobe couldn't read is tagged unreadable.
func (mi MediaInfo) Tags() []string {
	if mi.Error != "" {
		return []string{"unreadable"}
	}
	var tags []string
	add := func(t string) {
		if t != "" && !slices.Contains(tags, t) {
			tags = append(tags, t)
		}
	}
	if len(mi.Video) > 0 {
		v := mi.Video[0]
		add(strings.ToLower(v.Resolution))
		if v.HDR != "" {
			add("hdr")
			add(v.HDR)
		}
		add(v.Codec)
	}
	channels := 0
	for _, a := range mi.Audio {
		if a.Atmos {
			add("atmos")
		}
		if a.DTSX {
			add("dts-x")
		}
		channels = max(channels, a.Channels)
		if len(mi.Video) == 0 {
			if lossless(a.Codec) {
				add("lossless")
				if a.BitDepth > 16 || a.SampleRate > 48000 {
					add("hi-res")
				}
			}
		}
	}
	if len(mi.Audio) > 0 {
		add(ChannelLabel(channels))
	}
	return tags
}

// ChannelLabel names a channel count: mono, stereo, 5.1, 7.1.
func ChannelLabel(n int) string {
	switch {
	case n <= 0:
		return ""
	case n == 1:
		return "mono"
	case n == 2:
		return "stereo"
	case n >= 3 && n <= 5:
		return fmt.Sprintf("%d.0", n)
	}
	return fmt.Sprintf("%d.1", n-1)
}

// creditsStart finds the chapter where the closing credits begin, or 0. It
// goes by the chapter's title and only looks in the second half of the
// file, so opening credits and intros don't count.
func creditsStart(chapters []chapter, durationMs int64) int64 {
	if durationMs <= 0 {
		return 0
	}
	for _, c := range chapters {
		start, err := strconv.ParseFloat(c.StartTime, 64)
		if err != nil {
			continue
		}
		ms := int64(start * 1000)
		if ms < durationMs/2 || ms >= durationMs {
			continue
		}
		if isCreditsTitle(c.Tags["title"]) {
			return ms
		}
	}
	return 0
}

func isCreditsTitle(title string) bool {
	t := strings.ToLower(strings.TrimSpace(title))
	if strings.Contains(t, "opening") || strings.Contains(t, "intro") {
		return false
	}
	for _, w := range []string{"credit", "outro", "end titles"} {
		if strings.Contains(t, w) {
			return true
		}
	}
	// Anime names the ending theme's chapter ED or Ending.
	return slices.Contains([]string{"ed", "end", "ending", "ending theme", "ending song"}, t)
}
