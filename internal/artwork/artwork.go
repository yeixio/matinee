// Package artwork serves posters and backdrops from local files, a cache of
// remote images, or pictures embedded in audio files.
package artwork

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/yeixio/matinee/internal/slowfs"
)

type Cache struct {
	Dir    string
	FFmpeg string
	Client *http.Client
	// Chosen loads a cover an admin picked, for a "chosen:" source; nil
	// has none.
	Chosen func(ctx context.Context, src string) ([]byte, error)

	mu       sync.Mutex
	inflight map[string]*sync.Mutex
}

func NewCache(dir, ffmpeg string) *Cache {
	os.MkdirAll(dir, 0o755)
	return &Cache{Dir: dir, FFmpeg: ffmpeg, Client: &http.Client{Timeout: 30 * time.Second}, inflight: map[string]*sync.Mutex{}}
}

// Serve writes the image src points at: a local path, an http(s) URL
// (downloaded once, then served from the cache), "embedded:<audio file>",
// or "chosen:…" for a cover an admin picked.
// Further sources are tried in turn when one can't be had.
func (c *Cache) Serve(w http.ResponseWriter, r *http.Request, src string, fallbacks ...string) {
	for _, s := range append([]string{src}, fallbacks...) {
		path, err := c.resolve(r.Context(), s)
		if err != nil {
			continue
		}
		w.Header().Set("Cache-Control", "private, max-age=86400")
		http.ServeFile(w, r, path)
		return
	}
	http.Error(w, "image unavailable", http.StatusNotFound)
}

var tmdbSize = regexp.MustCompile(`^(https://image\.tmdb\.org/t/p/)w\d+/`)

// Large is src at the size a 4K TV shows it: TMDB's original for backdrops
// and its largest sized poster. Other sources are already as big as they get.
func Large(src, kind string) string {
	size := "w780"
	if kind == "backdrop" {
		size = "original"
	}
	return tmdbSize.ReplaceAllString(src, "${1}"+size+"/")
}

func (c *Cache) resolve(ctx context.Context, src string) (string, error) {
	switch {
	case strings.HasPrefix(src, "http://"), strings.HasPrefix(src, "https://"):
		ext := strings.ToLower(filepath.Ext(src))
		if ext != ".png" && ext != ".webp" {
			ext = ".jpg"
		}
		return c.cached(src, ext, func(dst string) error { return c.download(ctx, src, dst) })
	case strings.HasPrefix(src, "chosen:") && c.Chosen != nil:
		return c.cached(src, "", func(dst string) error {
			data, err := c.Chosen(ctx, src)
			if err != nil {
				return err
			}
			return os.WriteFile(dst, data, 0o644)
		})
	case strings.HasPrefix(src, "frame:"):
		// frame:<seconds>:<changed>:<video> is a still from a video, such as
		// an extra; <changed> makes a new one when the file changes.
		parts := strings.SplitN(strings.TrimPrefix(src, "frame:"), ":", 3)
		if len(parts) != 3 {
			return "", fmt.Errorf("bad frame source %q", src)
		}
		return c.cached(src, ".jpg", func(dst string) error {
			return onShare(parts[2], 30*time.Second, func() error { return c.frame(ctx, parts[2], parts[0], dst) })
		})
	case strings.HasPrefix(src, "embedded:"):
		audio := strings.TrimPrefix(src, "embedded:")
		return c.cached(src, ".jpg", func(dst string) error {
			return onShare(audio, 20*time.Second, func() error { return c.extract(ctx, audio, dst) })
		})
	default:
		// A cover in a library folder, which may be on a network share.
		if err := onShare(src, slowfs.Wait, func() error { _, err := os.Stat(src); return err }); err != nil {
			return "", err
		}
		return src, nil
	}
}

// cached returns the cache file for key, making it with fill the first time.
// Concurrent requests for the same image wait for one fill.
func (c *Cache) cached(key, ext string, fill func(dst string) error) (string, error) {
	sum := sha1.Sum([]byte(key))
	name := hex.EncodeToString(sum[:])
	dst := filepath.Join(c.Dir, name[:2], name+ext)
	if _, err := os.Stat(dst); err == nil {
		return dst, nil
	}

	c.mu.Lock()
	m, ok := c.inflight[name]
	if !ok {
		m = &sync.Mutex{}
		c.inflight[name] = m
	}
	c.mu.Unlock()
	m.Lock()
	defer m.Unlock()
	if _, err := os.Stat(dst); err == nil {
		return dst, nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", err
	}
	tmp := dst + ".tmp"
	if err := fill(tmp); err != nil {
		os.Remove(tmp)
		return "", err
	}
	return dst, os.Rename(tmp, dst)
}

func (c *Cache) download(ctx context.Context, url, dst string) error {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}
	// Wikimedia turns away requests without a descriptive User-Agent.
	req.Header.Set("User-Agent", "Matinee (https://github.com/yeixio/matinee)")
	res, err := c.Client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: HTTP %d", url, res.StatusCode)
	}
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, io.LimitReader(res.Body, 20<<20))
	return err
}

// frame takes one picture from a video, at sec seconds in, 640 wide.
func (c *Cache) frame(ctx context.Context, video, sec, dst string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, c.FFmpeg, "-v", "error", "-y", "-ss", sec, "-i", video,
		"-an", "-sn", "-map", "0:v:0", "-frames:v", "1", "-vf", "scale=640:-2", "-q:v", "3", "-f", "image2", dst).CombinedOutput()
	if err != nil {
		return fmt.Errorf("video still: %w: %s", err, out)
	}
	if info, err := os.Stat(dst); err != nil || info.Size() == 0 {
		return fmt.Errorf("video still: no picture at %ss", sec)
	}
	return nil
}

func (c *Cache) extract(ctx context.Context, audio, dst string) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, c.FFmpeg, "-v", "error", "-y", "-i", audio,
		"-an", "-map", "0:v:0", "-frames:v", "1", "-q:v", "2", "-f", "image2", dst).CombinedOutput()
	if err != nil {
		return fmt.Errorf("extract cover: %w: %s", err, out)
	}
	return nil
}

// onShare runs fn, which reads a file in a library, giving up after wait
// rather than making the request wait on a network share that stopped
// answering (see slowfs).
func onShare(path string, wait time.Duration, fn func() error) error {
	_, err := slowfs.DoWithin(path, wait, func() (struct{}, error) { return struct{}{}, fn() })
	return err
}
