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
	Tags       map[string]string // lower-cased keys: title, artist, album_artist, album, track, disc, date
}

type Prober struct {
	Path    string
	Timeout time.Duration
}

func (p Prober) Probe(ctx context.Context, file string) (Result, error) {
	timeout := p.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, p.Path, "-v", "error", "-print_format", "json",
		"-show_format", "-show_streams", file).Output()
	if err != nil {
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
			if r.VideoCodec == "" && s.Disposition["attached_pic"] == 0 {
				r.VideoCodec, r.Width, r.Height = s.CodecName, s.Width, s.Height
			}
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
	return r, nil
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
