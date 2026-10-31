// Package scan walks library folders and keeps the items table in sync with disk.
package scan

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
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
	// Added runs for each file a scan adds, e.g. to tell plugins.
	Added func(ctx context.Context, lib store.Library, itemID int64)

	mu      sync.Mutex
	running map[int64]bool
}

type Stats struct {
	Added, Updated, Unchanged, Removed, Failed int
	Unplayable                                 int // files with no playable stream, left out
	Took                                       time.Duration
}

var ErrAlreadyRunning = errors.New("scan already running")

var (
	errUnplayable   = errors.New("not playable")
	errProbeTimeout = errors.New("probe timed out")
)

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
	return s.scan(ctx, lib, nil)
}

// ScanPaths brings just these files and folders of a library in line with
// disk: what's there is added or updated, and what's listed under a path
// that's gone is removed. The watcher uses it to pick up changes without
// walking the whole library.
func (s *Scanner) ScanPaths(ctx context.Context, lib store.Library, paths []string) (Stats, error) {
	if len(paths) == 0 {
		return Stats{}, nil
	}
	return s.scan(ctx, lib, paths)
}

// target is where a scan walks from, inside one of the library's folders.
type target struct{ root, start string }

func (s *Scanner) scan(ctx context.Context, lib store.Library, paths []string) (Stats, error) {
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
	if sk, ok := kind.(Session); ok {
		defer sk.BeginScan(ctx, lib)()
	}
	// Trailers are found when someone opens a title (see trailers.go), not listed as titles.
	video := kind.Name() == "movies" || kind.Name() == "tv"
	partial := paths != nil
	var targets []target
	if partial {
		targets = targetsIn(lib.Paths, paths, video)
	} else {
		for _, root := range lib.Paths {
			targets = append(targets, target{root, root})
		}
	}
	start := time.Now()
	var st Stats
	seen := map[string]bool{}
	// A folder that can't be read, such as an unmounted network share, must
	// not look like every file in it was deleted, so its items are kept.
	var unreadable []string
	rootOK := map[string]bool{}
	for _, t := range targets {
		root := t.root
		ok, checked := rootOK[root]
		if !checked {
			// An empty folder is usually a share that isn't mounted yet, too.
			entries, err := os.ReadDir(root)
			if ok = err == nil && len(entries) > 0; !ok {
				s.Log.Warn("library folder missing or empty; keeping its items", "library", lib.Name, "path", root, "err", err)
			}
			rootOK[root] = ok
		}
		if !ok {
			unreadable = append(unreadable, t.start)
			continue
		}
		if _, err := os.Lstat(t.start); errors.Is(err, fs.ErrNotExist) {
			continue // deleted: the cleanup below removes what was listed under it
		}
		err := filepath.WalkDir(t.start, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				s.Log.Warn("walk", "path", path, "err", err)
				unreadable = append(unreadable, path)
				return nil
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if d.IsDir() {
				if path != root && (strings.HasPrefix(d.Name(), ".") || video && IsTrailerDir(d.Name())) {
					return filepath.SkipDir
				}
				return nil
			}
			if strings.HasPrefix(d.Name(), ".") || !kind.Wants(path) || video && IsTrailerFile(d.Name()) {
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
			newID, err := s.indexFile(ctx, kind, lib, root, path, info)
			if err != nil {
				switch {
				case errors.Is(err, errUnplayable):
					// Leave it out; if it was listed before, the cleanup below removes it.
					s.Log.Debug("not playable", "path", path, "err", err)
					delete(seen, path)
					st.Unplayable++
				case errors.Is(err, errProbeTimeout):
					// A slow share isn't a broken file: keep what's listed and retry next scan.
					s.Log.Warn("index file", "path", path, "err", err)
					if id == 0 {
						delete(seen, path)
					}
					st.Failed++
				default:
					s.Log.Warn("index file", "path", path, "err", err)
					st.Failed++
				}
				return nil
			}
			if id == 0 {
				st.Added++
				if s.Added != nil {
					s.Added(ctx, lib, newID)
				}
			} else {
				st.Updated++
			}
			return nil
		})
		if err != nil {
			return st, err
		}
	}
	var scanned []string
	for _, t := range targets {
		scanned = append(scanned, t.start)
	}
	removed, err := s.Store.RemoveMissing(ctx, lib.ID, func(p string) bool {
		// A partial scan only answers for what it looked at.
		return seen[p] || under(p, unreadable) || partial && !under(p, scanned)
	})
	if err != nil {
		return st, err
	}
	st.Removed = removed
	if fk, ok := kind.(Finisher); ok && (!partial || st.Added+st.Updated+st.Removed > 0) {
		if err := fk.FinishScan(ctx, s.Store, lib); err != nil {
			return st, err
		}
	}
	if lib.Kind == "music" && (!partial || st.Added+st.Updated+st.Removed > 0) {
		if err := s.readAlbumTags(ctx, lib); err != nil {
			return st, err
		}
	}
	st.Took = time.Since(start)
	changed := st.Added+st.Updated+st.Removed > 0
	if s.AfterScan != nil && (!partial || changed) {
		s.AfterScan(ctx, lib)
	}
	if !partial || changed || st.Failed > 0 {
		s.Log.Info("scan done", "library", lib.Name, "paths", len(paths), "added", st.Added, "updated", st.Updated,
			"unchanged", st.Unchanged, "removed", st.Removed, "unplayable", st.Unplayable, "failed", st.Failed, "took", st.Took.Round(time.Millisecond))
	}
	return st, nil
}

// targetsIn places each path in the library folder it belongs to. Paths
// outside every folder, inside hidden or trailer folders, or under another
// path in the list are dropped.
func targetsIn(roots, paths []string, video bool) []target {
	var out []target
	for _, p := range paths {
		p = filepath.Clean(p)
		root := ""
		for _, r := range roots {
			if (p == r || under(p, []string{r})) && len(r) > len(root) {
				root = r
			}
		}
		if root == "" || ignoredBetween(root, p, video) {
			continue
		}
		out = append(out, target{root, p})
	}
	// Shortest first, so a folder comes before what's in it.
	slices.SortFunc(out, func(a, b target) int { return len(a.start) - len(b.start) })
	kept := out[:0]
	for _, t := range out {
		dup := false
		for _, k := range kept {
			if t.start == k.start || under(t.start, []string{k.start}) {
				dup = true
				break
			}
		}
		if !dup {
			kept = append(kept, t)
		}
	}
	return kept
}

// ignoredBetween reports whether a folder between root and path is one a
// scan skips: hidden, or a trailers folder in a video library.
func ignoredBetween(root, path string, video bool) bool {
	rel, err := filepath.Rel(root, filepath.Dir(path))
	if err != nil || rel == "." {
		return false
	}
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		if strings.HasPrefix(part, ".") || video && IsTrailerDir(part) {
			return true
		}
	}
	return false
}

// under reports whether p is one of dirs or inside one of them.
func under(p string, dirs []string) bool {
	for _, d := range dirs {
		if p == d || strings.HasPrefix(p, d+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func (s *Scanner) indexFile(ctx context.Context, kind Kind, lib store.Library, root, path string, info fs.FileInfo) (int64, error) {
	if info.Size() == 0 {
		return 0, fmt.Errorf("%w: empty file", errUnplayable)
	}
	var pr probe.Result
	var err error
	if _, ok := kind.(Unprobed); !ok {
		pr, err = s.Prober.Probe(ctx, path)
	}
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return 0, fmt.Errorf("%w: %v", errProbeTimeout, err)
	case errors.Is(err, context.Canceled):
		return 0, err
	case err != nil:
		return 0, fmt.Errorf("%w: %v", errUnplayable, err)
	case !isUnprobed(kind) && !kind.Playable(pr):
		return 0, fmt.Errorf("%w: no playable stream", errUnplayable)
	}
	it, err := kind.Index(ctx, s.Store, File{Library: lib, Root: root, Path: path, Info: info, Probe: pr})
	if err != nil {
		return 0, err
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
	id, err := s.Store.UpsertFile(ctx, it)
	if err == nil && it.Details != nil {
		err = s.Store.ApplyMetadata(ctx, id, it.Details)
	}
	return id, err
}

func isUnprobed(k Kind) bool {
	_, ok := k.(Unprobed)
	return ok
}
