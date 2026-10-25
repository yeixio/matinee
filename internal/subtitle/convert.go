package subtitle

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"time"
	"unicode/utf8"
)

// Reading embedded subtitles means reading the whole video, which can be
// slow for a large file on network storage.
const extractTimeout = 10 * time.Minute

// Converter turns subtitle tracks into WebVTT and caches the results, so
// each track is converted once per version of its file.
type Converter struct {
	FFmpeg string
	Dir    string

	mu       sync.Mutex
	inflight map[string]*sync.Mutex
}

func NewConverter(ffmpeg, dir string) *Converter {
	os.MkdirAll(dir, 0o755)
	return &Converter{FFmpeg: ffmpeg, Dir: dir, inflight: map[string]*sync.Mutex{}}
}

// Serve writes track t of the video at videoPath as WebVTT. all lists the
// video's tracks: the first request for an embedded track extracts every
// embedded text track in one pass, since that costs one read of the file.
func (c *Converter) Serve(w http.ResponseWriter, r *http.Request, videoPath string, t Track, all []Track) {
	if t.BurnIn {
		http.Error(w, "picture-based subtitles are burned in during playback", http.StatusUnprocessableEntity)
		return
	}
	path, err := c.vtt(videoPath, t, all)
	if err != nil {
		http.Error(w, "subtitles unavailable", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/vtt; charset=utf-8")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	http.ServeFile(w, r, path)
}

func (c *Converter) vtt(videoPath string, t Track, all []Track) (string, error) {
	if t.External {
		info, err := os.Stat(t.Path)
		if err != nil {
			return "", err
		}
		dst := c.cachePath(t.Path, info)
		return dst, c.once(dst, func() error { return c.convertFile(t, dst) })
	}
	info, err := os.Stat(videoPath)
	if err != nil {
		return "", err
	}
	key := func(stream int) string { return c.cachePath(videoPath+"#"+strconv.Itoa(stream), info) }
	dst := key(t.Stream)
	// Locked per video, so a second track's request waits for the pass that makes it.
	return dst, c.once(c.cachePath(videoPath, info), func() error {
		if fileExists(dst) {
			return nil
		}
		var outs []extractOut
		for _, o := range all {
			if !o.External && !o.BurnIn && !fileExists(key(o.Stream)) {
				outs = append(outs, extractOut{o.Stream, key(o.Stream)})
			}
		}
		if err := c.extract(videoPath, outs); err != nil && len(outs) > 1 {
			// One bad track shouldn't hide the rest: try just the one asked for.
			return c.extract(videoPath, []extractOut{{t.Stream, dst}})
		} else if err != nil {
			return err
		}
		return nil
	})
}

func (c *Converter) cachePath(src string, info os.FileInfo) string {
	sum := sha1.Sum(fmt.Appendf(nil, "%s|%d|%d", src, info.Size(), info.ModTime().UnixNano()))
	name := hex.EncodeToString(sum[:])
	return filepath.Join(c.Dir, name[:2], name+".vtt")
}

// once runs fill unless key's file exists; concurrent callers with the same
// key wait for the first.
func (c *Converter) once(key string, fill func() error) error {
	if fileExists(key) {
		return nil
	}
	c.mu.Lock()
	m, ok := c.inflight[key]
	if !ok {
		m = &sync.Mutex{}
		c.inflight[key] = m
	}
	c.mu.Unlock()
	m.Lock()
	defer m.Unlock()
	if fileExists(key) {
		return nil
	}
	return fill()
}

type extractOut struct {
	stream int
	dst    string
}

func (c *Converter) extract(videoPath string, outs []extractOut) error {
	if len(outs) == 0 {
		return nil
	}
	args := []string{"-v", "error", "-nostdin", "-y", "-i", videoPath}
	for _, o := range outs {
		if err := os.MkdirAll(filepath.Dir(o.dst), 0o755); err != nil {
			return err
		}
		args = append(args, "-map", "0:s:"+strconv.Itoa(o.stream), "-c:s", "webvtt", "-f", "webvtt", o.dst+".tmp")
	}
	if err := c.run(args, extractTimeout); err != nil {
		for _, o := range outs {
			os.Remove(o.dst + ".tmp")
		}
		return err
	}
	for _, o := range outs {
		if err := os.Rename(o.dst+".tmp", o.dst); err != nil {
			return err
		}
	}
	return nil
}

func (c *Converter) convertFile(t Track, dst string) error {
	data, err := os.ReadFile(t.Path)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	if t.Codec == "webvtt" && utf8.Valid(data) && bytes.HasPrefix(data, []byte("WEBVTT")) {
		// Already what browsers want; FFmpeg would only drop cue settings.
		return writeAtomic(dst, data)
	}
	args := []string{"-v", "error", "-nostdin", "-y"}
	if !utf8.Valid(data) && !isUTF16(data) {
		// Older subtitle files are mostly Windows-1252.
		args = append(args, "-sub_charenc", "CP1252")
	}
	args = append(args, "-i", t.Path, "-map", "0:s:0", "-c:s", "webvtt", "-f", "webvtt", dst+".tmp")
	if err := c.run(args, time.Minute); err != nil {
		os.Remove(dst + ".tmp")
		return err
	}
	return os.Rename(dst+".tmp", dst)
}

func (c *Converter) run(args []string, timeout time.Duration) error {
	// Not tied to the request: a viewer who gives up shouldn't waste a long extraction.
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, c.FFmpeg, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg subtitles: %w: %s", err, bytes.TrimSpace(out))
	}
	return nil
}

func isUTF16(b []byte) bool {
	return bytes.HasPrefix(b, []byte{0xff, 0xfe}) || bytes.HasPrefix(b, []byte{0xfe, 0xff})
}

func writeAtomic(dst string, data []byte) error {
	if err := os.WriteFile(dst+".tmp", data, 0o644); err != nil {
		return err
	}
	return os.Rename(dst+".tmp", dst)
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }
