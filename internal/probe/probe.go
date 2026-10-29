// Package probe reads media facts (duration, codecs, tags) with ffprobe.
package probe

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type Result struct {
	DurationMs int64
	Container  string // first name ffprobe gives, e.g. "matroska" or "mov"
	VideoCodec string
	AudioCodec string
	Width      int
	Height     int
	HasCover   bool              // an embedded picture, like album art in a FLAC or MP3
	Tags       map[string]string // lower-cased keys: title, artist, album_artist, album, track, disc, date
	Subtitles  []Subtitle        // embedded subtitle streams, in file order
	Chapters   []Chapter         // marked chapters, such as an audiobook's, in order
}

// Chapter is a marked part of a file, with its own title.
type Chapter struct {
	Title   string `json:"title,omitempty"`
	StartMs int64  `json:"startMs"`
	EndMs   int64  `json:"endMs"`
}

// Subtitle is an embedded subtitle stream.
type Subtitle struct {
	Index    int    `json:"index"` // among the file's subtitle streams, as in FFmpeg's 0:s:N
	Codec    string `json:"codec"`
	Language string `json:"language,omitempty"` // as tagged, usually ISO 639-2 like "eng"
	Title    string `json:"title,omitempty"`
	Default  bool   `json:"default,omitempty"`
	Forced   bool   `json:"forced,omitempty"`
	SDH      bool   `json:"sdh,omitempty"` // for the deaf and hard of hearing
}

type Prober struct {
	Path    string
	Timeout time.Duration
	// LowPriority runs ffprobe at the lowest CPU and disk priority, for work
	// in the background that nobody is waiting on.
	LowPriority bool
}

func (p Prober) Probe(ctx context.Context, file string) (Result, error) {
	timeout := p.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, p.Path, "-v", "error", "-print_format", "json",
		"-show_format", "-show_streams", "-show_chapters", file).Output()
	if err != nil {
		if ctx.Err() != nil {
			// Timed out or cancelled: says nothing about the file itself.
			return Result{}, fmt.Errorf("ffprobe %s: %w", file, ctx.Err())
		}
		return Result{}, fmt.Errorf("ffprobe %s: %w", file, err)
	}
	return parse(out)
}

type ffprobeOutput struct {
	Format struct {
		FormatName string            `json:"format_name"`
		Duration   string            `json:"duration"`
		Tags       map[string]string `json:"tags"`
	} `json:"format"`
	Streams []struct {
		CodecType   string            `json:"codec_type"`
		CodecName   string            `json:"codec_name"`
		Width       int               `json:"width"`
		Height      int               `json:"height"`
		Disposition map[string]int    `json:"disposition"`
		Tags        map[string]string `json:"tags"`
	} `json:"streams"`
	Chapters []struct {
		StartTime string            `json:"start_time"`
		EndTime   string            `json:"end_time"`
		Tags      map[string]string `json:"tags"`
	} `json:"chapters"`
}

func parse(data []byte) (Result, error) {
	var o ffprobeOutput
	if err := json.Unmarshal(data, &o); err != nil {
		return Result{}, err
	}
	r := Result{Tags: map[string]string{}}
	r.Container, _, _ = strings.Cut(o.Format.FormatName, ",")
	if d, err := strconv.ParseFloat(o.Format.Duration, 64); err == nil {
		r.DurationMs = int64(d * 1000)
	}
	for k, v := range o.Format.Tags {
		r.Tags[strings.ToLower(k)] = v
	}
	for _, s := range o.Streams {
		switch s.CodecType {
		case "video":
			// Cover art in music files shows up as a video stream; skip it.
			if s.Disposition["attached_pic"] == 1 {
				r.HasCover = true
			} else if r.VideoCodec == "" {
				r.VideoCodec, r.Width, r.Height = s.CodecName, s.Width, s.Height
			}
		case "subtitle":
			r.Subtitles = append(r.Subtitles, Subtitle{
				Index:    len(r.Subtitles),
				Codec:    s.CodecName,
				Language: tag(s.Tags, "language"),
				Title:    tag(s.Tags, "title"),
				Default:  s.Disposition["default"] == 1,
				Forced:   s.Disposition["forced"] == 1,
				SDH:      s.Disposition["hearing_impaired"] == 1,
			})
		case "audio":
			if r.AudioCodec == "" {
				r.AudioCodec = s.CodecName
			}
			// Ogg and Opus keep tags on the stream rather than the container.
			for k, v := range s.Tags {
				if _, ok := r.Tags[strings.ToLower(k)]; !ok {
					r.Tags[strings.ToLower(k)] = v
				}
			}
		}
	}
	for _, c := range o.Chapters {
		start, err1 := strconv.ParseFloat(c.StartTime, 64)
		end, err2 := strconv.ParseFloat(c.EndTime, 64)
		if err1 != nil || err2 != nil || end <= start {
			continue
		}
		r.Chapters = append(r.Chapters, Chapter{Title: tag(c.Tags, "title"), StartMs: int64(start * 1000), EndMs: int64(end * 1000)})
	}
	return r, nil
}

// SubtitleList is Subtitles, never nil, so it encodes as [] rather than null.
func (r Result) SubtitleList() []Subtitle {
	if r.Subtitles == nil {
		return []Subtitle{}
	}
	return r.Subtitles
}

// tag reads a stream tag whatever its case; MKV and MP4 muxers differ.
func tag(tags map[string]string, key string) string {
	for k, v := range tags {
		if strings.EqualFold(k, key) {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// LeadingInt reads "3/12" or "03" as 3, the way track and disc tags are written.
func LeadingInt(s string) int {
	s = strings.TrimSpace(s)
	end := 0
	for end < len(s) && s[end] >= '0' && s[end] <= '9' {
		end++
	}
	n, _ := strconv.Atoi(s[:end])
	return n
}
