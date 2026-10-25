// Package scan walks library folders and keeps the items table in sync with disk.
package scan

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"path/filepath"
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
	// AfterScan runs once a library scan finishes, e.g. to fetch metadata.
	AfterScan func(ctx context.Context, lib store.Library)

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

	kind, ok := KindByName(lib.Kind)
	if !ok {
		return Stats{}, fmt.Errorf("unknown library kind %q", lib.Kind)
	}
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
			if strings.HasPrefix(d.Name(), ".") || !kind.Wants(path) {
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
			if err := s.indexFile(ctx, kind, lib, root, path, info); err != nil {
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
	if s.AfterScan != nil {
		s.AfterScan(ctx, lib)
	}
	s.Log.Info("scan done", "library", lib.Name, "added", st.Added, "updated", st.Updated,
		"unchanged", st.Unchanged, "removed", st.Removed, "failed", st.Failed, "took", st.Took.Round(time.Millisecond))
	return st, nil
}

func (s *Scanner) indexFile(ctx context.Context, kind Kind, lib store.Library, root, path string, info fs.FileInfo) error {
	// A file ffprobe can't read is still listed; playback will tell the user.
	pr, err := s.Prober.Probe(ctx, path)
	if err != nil {
		s.Log.Debug("probe failed", "path", path, "err", err)
		pr = probe.Result{Tags: map[string]string{}}
	}
	it, err := kind.Index(ctx, s.Store, File{Library: lib, Root: root, Path: path, Info: info, Probe: pr})
	if err != nil {
		return err
	}
	it.LibraryID, it.Path, it.Size, it.Mtime = lib.ID, path, info.Size(), info.ModTime().Unix()
	it.Container, it.VideoCodec, it.AudioCodec = pr.Container, pr.VideoCodec, pr.AudioCodec
	if pr.DurationMs > 0 {
		it.DurationMs = &pr.DurationMs
	}
	if pr.Width > 0 {
		it.Width, it.Height = &pr.Width, &pr.Height
	}
	if err == nil && pr.VideoCodec != "" {
		subs, _ := json.Marshal(pr.SubtitleList())
		it.SubtitleStreams = string(subs)
	}
	_, err = s.Store.UpsertFile(ctx, it)
	return err
}
