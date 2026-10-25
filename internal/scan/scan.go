// Package scan walks library folders and keeps the items table in sync with disk.
package scan

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/yeixio/matinee/internal/probe"
	"github.com/yeixio/matinee/internal/store"
)

type Scanner struct {
	Store  *store.Store
	Prober probe.Prober
	Log    *slog.Logger

	mu      sync.Mutex
	running map[int64]bool
}

type Stats struct {
	Added, Updated, Unchanged, Removed, Failed int
	Took                                       time.Duration
}

var ErrAlreadyRunning = errors.New("scan already running")

// Running reports whether a library is being scanned right now.
func (s *Scanner) Running(libraryID int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running[libraryID]
}

// ScanAll scans every library, one at a time.
func (s *Scanner) ScanAll(ctx context.Context) {
	libs, err := s.Store.Libraries(ctx)
	if err != nil {
		s.Log.Error("list libraries", "err", err)
		return
	}
	for _, lib := range libs {
		if _, err := s.Scan(ctx, lib); err != nil && !errors.Is(err, ErrAlreadyRunning) {
			s.Log.Error("scan failed", "library", lib.Name, "err", err)
		}
	}
}

// Scan brings one library in line with its folders. Unchanged files (same
// size and mtime) are skipped, so rescans are cheap.
func (s *Scanner) Scan(ctx context.Context, lib store.Library) (Stats, error) {
	s.mu.Lock()
	if s.running == nil {
		s.running = map[int64]bool{}
	}
	if s.running[lib.ID] {
		s.mu.Unlock()
		return Stats{}, ErrAlreadyRunning
	}
	s.running[lib.ID] = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.running, lib.ID)
		s.mu.Unlock()
	}()

	start := time.Now()
	var st Stats
	seen := map[string]bool{}
	for _, root := range lib.Paths {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				s.Log.Warn("walk", "path", path, "err", err)
				return nil
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if d.IsDir() {
				if path != root && strings.HasPrefix(d.Name(), ".") {
					return filepath.SkipDir
				}
				return nil
			}
			if !s.wants(lib.Kind, path) {
				return nil
			}
			seen[path] = true
			info, err := d.Info()
			if err != nil {
				return nil
			}
			id, size, mtime, err := s.Store.FileStat(ctx, path)
			switch {
			case err == nil && size == info.Size() && mtime == info.ModTime().Unix():
				st.Unchanged++
				return nil
			case err != nil && !errors.Is(err, store.ErrNotFound):
				return err
			}
			if err := s.indexFile(ctx, lib, root, path, info); err != nil {
				s.Log.Warn("index file", "path", path, "err", err)
				st.Failed++
				return nil
			}
			if id == 0 {
				st.Added++
			} else {
				st.Updated++
			}
			return nil
		})
		if err != nil {
			return st, err
		}
	}
	removed, err := s.Store.RemoveMissing(ctx, lib.ID, seen)
	if err != nil {
		return st, err
	}
	st.Removed = removed
	st.Took = time.Since(start)
	s.Log.Info("scan done", "library", lib.Name, "added", st.Added, "updated", st.Updated,
		"unchanged", st.Unchanged, "removed", st.Removed, "failed", st.Failed, "took", st.Took.Round(time.Millisecond))
	return st, nil
}

func (s *Scanner) wants(kind, path string) bool {
	if strings.HasPrefix(filepath.Base(path), ".") {
		return false
	}
	if kind == "music" {
		return isAudio(path)
	}
	return isVideo(path) && !strings.Contains(strings.ToLower(filepath.Base(path)), "sample")
}

func (s *Scanner) indexFile(ctx context.Context, lib store.Library, root, path string, info fs.FileInfo) error {
	// A file ffprobe can't read is still listed; playback will tell the user.
	pr, err := s.Prober.Probe(ctx, path)
	if err != nil {
		s.Log.Debug("probe failed", "path", path, "err", err)
		pr = probe.Result{Tags: map[string]string{}}
	}
	it := store.Item{
		LibraryID:  lib.ID,
		Path:       path,
		Size:       info.Size(),
		Mtime:      info.ModTime().Unix(),
		Container:  pr.Container,
		VideoCodec: pr.VideoCodec,
		AudioCodec: pr.AudioCodec,
	}
	if pr.DurationMs > 0 {
		it.DurationMs = &pr.DurationMs
	}
	if pr.Width > 0 {
		it.Width, it.Height = &pr.Width, &pr.Height
	}

	switch lib.Kind {
	case "movies":
		mi := ParseMovie(path)
		it.Kind, it.Title, it.Year = "movie", mi.Title, nonZero(mi.Year)
	case "tv":
		ep := ParseEpisode(root, path)
		if !ep.OK {
			// Not an episode name; file it under its folder as season 0 (specials).
			ep = EpisodeInfo{Show: cleanTitle(filepath.Base(filepath.Dir(path))), Season: 0, Title: cleanTitle(stem(path))}
		}
		showID, err := s.Store.EnsureContainer(ctx, lib.ID, nil, "show", ep.Show, nil, nil)
		if err != nil {
			return err
		}
		seasonID, err := s.Store.EnsureContainer(ctx, lib.ID, &showID, "season", seasonTitle(ep.Season), &ep.Season, nil)
		if err != nil {
			return err
		}
		it.Kind, it.ParentID = "episode", &seasonID
		it.IndexNumber = nonZero(ep.Episode)
		it.Title = ep.Title
		if it.Title == "" {
			it.Title = "Episode " + strconv.Itoa(ep.Episode)
		}
	case "music":
		tr := trackFromTags(pr.Tags, ParseTrackPath(root, path))
		artistID, err := s.Store.EnsureContainer(ctx, lib.ID, nil, "artist", tr.Artist, nil, nil)
		if err != nil {
			return err
		}
		albumID, err := s.Store.EnsureContainer(ctx, lib.ID, &artistID, "album", tr.Album, nil, nonZero(tr.Year))
		if err != nil {
			return err
		}
		it.Kind, it.ParentID, it.Title = "track", &albumID, tr.Title
		it.IndexNumber, it.DiscNumber, it.Year = nonZero(tr.Track), nonZero(tr.Disc), nonZero(tr.Year)
	}
	_, err = s.Store.UpsertFile(ctx, it)
	return err
}

// trackFromTags prefers embedded tags and falls back to the folder layout.
func trackFromTags(tags map[string]string, fromPath TrackInfo) TrackInfo {
	t := fromPath
	pick := func(dst *string, keys ...string) {
		for _, k := range keys {
			if v := strings.TrimSpace(tags[k]); v != "" {
				*dst = v
				return
			}
		}
	}
	pick(&t.Artist, "album_artist", "albumartist", "artist")
	pick(&t.Album, "album")
	pick(&t.Title, "title")
	if n := probe.LeadingInt(tags["track"]); n > 0 {
		t.Track = n
	}
	if n := probe.LeadingInt(tags["disc"]); n > 0 {
		t.Disc = n
	}
	if n := probe.LeadingInt(tags["date"]); n > 1000 {
		t.Year = n
	}
	if t.Artist == "" {
		t.Artist = "Unknown Artist"
	}
	if t.Album == "" {
		t.Album = "Unknown Album"
	}
	return t
}

func seasonTitle(n int) string {
	if n == 0 {
		return "Specials"
	}
	return "Season " + strconv.Itoa(n)
}

func nonZero(n int) *int {
	if n == 0 {
		return nil
	}
	return &n
}
