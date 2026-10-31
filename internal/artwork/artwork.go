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
	case strings.HasPrefix(src, "embedded:"):
		audio := strings.TrimPrefix(src, "embedded:")
		return c.cached(src, ".jpg", func(dst string) error { return c.extract(ctx, audio, dst) })
	default:
		if _, err := os.Stat(src); err != nil {
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
