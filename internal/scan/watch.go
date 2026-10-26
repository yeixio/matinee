package scan

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/yeixio/matinee/internal/store"
)

// Watching picks up new, changed and removed files a few seconds after they
// land, and scans just those paths (see Scanner.ScanPaths). There are two
// ways to notice them:
//
//   - Change notifications (inotify and friends, through fsnotify). Instant
//     and free, but only for changes made on this machine's own disks.
//   - Polling: every PollInterval each folder is stat'ed, and the ones whose
//     modification time moved are listed again and compared. Network shares
//     (SMB, NFS) and FUSE mounts need this, because a file someone copies to
//     the share from another computer never sends this machine a notification.
//
// Neither sees a file rewritten in place under the same name on a share, so
// the periodic full rescan (MATINEE_SCAN_INTERVAL) stays on as the backstop.

// WatchMode says how library folders are watched.
type WatchMode string

const (
	// WatchAuto uses change notifications on local disks and polling on
	// network shares.
	WatchAuto WatchMode = "auto"
	// WatchPoll polls every folder, for disks that look local but aren't,
	// like a share mounted inside a container's own filesystem.
	WatchPoll WatchMode = "poll"
	// WatchOff leaves it to the periodic rescan.
	WatchOff WatchMode = "off"
)

// SettingWatch holds the WatchMode.
const SettingWatch = "library_watch"

// How a folder is being watched, for FolderWatch.Method.
const (
	MethodEvents  = "events"
	MethodPolling = "polling"
	MethodOff     = "off"
)

// FolderWatch tells admins how one library folder is watched.
type FolderWatch struct {
	Path   string `json:"path"`
	Method string `json:"method"`
	// Reason says why a folder is polled: "network share (cifs)", or that
	// change notifications couldn't be set up.
	Reason string `json:"reason,omitempty"`
}

type Watcher struct {
	Scanner *Scanner
	Log     *slog.Logger
	// PollInterval is how often polled folders are checked. A pass that takes
	// long (a big library on a slow share) waits longer, so polling never
	// keeps the share busy.
	PollInterval time.Duration
	// Quiet is how long a path must hold still before it's scanned, so a
	// file still being copied isn't read half way.
	Quiet time.Duration

	reload chan struct{}
	mu     sync.Mutex
	status map[int64][]FolderWatch
}

func NewWatcher(s *Scanner, log *slog.Logger) *Watcher {
	return &Watcher{Scanner: s, Log: log, PollInterval: 30 * time.Second, Quiet: 2 * time.Second,
		reload: make(chan struct{}, 1), status: map[int64][]FolderWatch{}}
}

// Mode reads the admin's WatchMode.
func (w *Watcher) Mode(ctx context.Context) (WatchMode, error) {
	v, err := w.Scanner.Store.Setting(ctx, SettingWatch)
	switch m := WatchMode(v); {
	case err != nil:
		return WatchAuto, err
	case m == WatchPoll || m == WatchOff:
		return m, nil
	default:
		return WatchAuto, nil
	}
}

func (w *Watcher) SetMode(ctx context.Context, m WatchMode) error {
	if m != WatchAuto && m != WatchPoll && m != WatchOff {
		return fmt.Errorf("unknown watch mode %q", m)
	}
	if err := w.Scanner.Store.SetSetting(ctx, SettingWatch, string(m)); err != nil {
		return err
	}
	w.Reload()
	return nil
}

// Reload starts over with the current libraries and mode. Call it when
// either changes.
func (w *Watcher) Reload() {
	select {
	case w.reload <- struct{}{}:
	default:
	}
}

// Status says how each folder of a library is watched; nil until the
// watcher has set it up.
func (w *Watcher) Status(libraryID int64) []FolderWatch {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.status[libraryID]
}

func (w *Watcher) setStatus(libraryID int64, fw FolderWatch) {
	w.mu.Lock()
	defer w.mu.Unlock()
	list := slices.DeleteFunc(w.status[libraryID], func(f FolderWatch) bool { return f.Path == fw.Path })
	w.status[libraryID] = append(list, fw)
}

// Run watches every library until ctx ends.
func (w *Watcher) Run(ctx context.Context) {
	for {
		runCtx, cancel := context.WithCancel(ctx)
		var wg sync.WaitGroup
		if err := w.start(runCtx, &wg); err != nil {
			w.Log.Error("watch libraries", "err", err)
		}
		select {
		case <-ctx.Done():
		case <-w.reload:
		}
		cancel()
		wg.Wait()
		if ctx.Err() != nil {
			return
		}
	}
}

func (w *Watcher) start(ctx context.Context, wg *sync.WaitGroup) error {
	w.mu.Lock()
	w.status = map[int64][]FolderWatch{}
	w.mu.Unlock()
	mode, err := w.Mode(ctx)
	if err != nil {
		return err
	}
	libs, err := w.Scanner.Store.Libraries(ctx)
	if err != nil {
		return err
	}
	for _, lib := range libs {
		if mode == WatchOff {
			for _, p := range lib.Paths {
				w.setStatus(lib.ID, FolderWatch{Path: p, Method: MethodOff})
			}
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			w.watchLibrary(ctx, lib, mode)
		}()
	}
	return nil
}

func (w *Watcher) watchLibrary(ctx context.Context, lib store.Library, mode WatchMode) {
	kind, ok := KindByName(lib.Kind)
	if !ok {
		return
	}
	changes := make(chan string, 256)
	var wg sync.WaitGroup
	defer wg.Wait()
	var polled []string
	for _, root := range lib.Paths {
		reason := ""
		if mode == WatchPoll {
			reason = "set to check on a timer"
		} else if fsType, network := networkFS(root); network {
			reason = "network share (" + fsType + ")"
		} else {
			fw, err := watchTree(root)
			if err == nil {
				w.setStatus(lib.ID, FolderWatch{Path: root, Method: MethodEvents})
				wg.Add(1)
				go func() {
					defer wg.Done()
					w.events(ctx, fw, root, changes)
				}()
				continue
			}
			// Usually the inotify limit (fs.inotify.max_user_watches) on a big library.
			w.Log.Warn("can't watch folder for changes; checking it on a timer instead", "path", root, "err", err)
			reason = "change notifications unavailable: " + err.Error()
		}
		w.setStatus(lib.ID, FolderWatch{Path: root, Method: MethodPolling, Reason: reason})
		polled = append(polled, root)
	}
	if len(polled) > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w.poll(ctx, polled, changes)
		}()
	}
	w.collect(ctx, lib, kind, changes)
}

// pendingPath is a changed path waiting to hold still.
type pendingPath struct {
	last time.Time
	sig  string // what it looked like at the last check; "" before the first
}

// collect gathers changed paths and scans each once it has held still for
// Quiet: no new change, and the same size and times twice in a row.
func (w *Watcher) collect(ctx context.Context, lib store.Library, kind Kind, changes <-chan string) {
	pending := map[string]*pendingPath{}
	tick := time.NewTicker(w.Quiet / 3)
	defer tick.Stop()
	video := kind.Name() == "movies" || kind.Name() == "tv"
	for {
		select {
		case <-ctx.Done():
			return
		case p := <-changes:
			if !wanted(kind, video, p) {
				continue
			}
			if e := pending[p]; e != nil {
				e.last, e.sig = time.Now(), ""
			} else {
				pending[p] = &pendingPath{last: time.Now()}
			}
		case now := <-tick.C:
			var ready []string
			for p, e := range pending {
				if now.Sub(e.last) < w.Quiet {
					continue
				}
				sig := signature(p)
				if sig != e.sig {
					e.last, e.sig = now, sig
					continue
				}
				ready = append(ready, p)
			}
			if len(ready) == 0 {
				continue
			}
			_, err := w.Scanner.ScanPaths(ctx, lib, ready)
			if errors.Is(err, ErrAlreadyRunning) {
				for _, p := range ready {
					pending[p].last = now // try again once the scan is done
				}
				continue
			}
			if err != nil && ctx.Err() == nil {
				w.Log.Warn("scan changed files", "library", lib.Name, "err", err)
			}
			for _, p := range ready {
				delete(pending, p)
			}
		}
	}
}

// wanted reports whether a changed path could matter to the library: a
// folder, a file the library plays, or something that's gone.
func wanted(kind Kind, video bool, p string) bool {
	name := filepath.Base(p)
	if strings.HasPrefix(name, ".") {
		return false
	}
	info, err := os.Lstat(p)
	if err != nil {
		return true // removed or renamed away
	}
	if info.IsDir() {
		return !(video && IsTrailerDir(name))
	}
	return kind.Wants(p) && !(video && IsTrailerFile(name))
}

// signature sums up a path's size and times, to tell when a copy into it
// has finished.
func signature(p string) string {
	info, err := os.Lstat(p)
	if err != nil {
		return "gone"
	}
	if !info.IsDir() {
		return fmt.Sprint(info.Size(), info.ModTime().UnixNano())
	}
	var files, size int64
	var newest time.Time
	filepath.WalkDir(p, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if fi, err := d.Info(); err == nil {
			files, size = files+1, size+fi.Size()
			if fi.ModTime().After(newest) {
				newest = fi.ModTime()
			}
		}
		return nil
	})
	return fmt.Sprint(files, size, newest.UnixNano())
}

// watchTree sets up change notifications for root and every folder in it.
func watchTree(root string) (*fsnotify.Watcher, error) {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	if err := addTree(fw, root); err != nil {
		fw.Close()
		return nil, err
	}
	return fw, nil
}

func addTree(fw *fsnotify.Watcher, dir string) error {
	return filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if path == dir {
				return err
			}
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		if path != dir && strings.HasPrefix(d.Name(), ".") {
			return filepath.SkipDir
		}
		return fw.Add(path)
	})
}

func (w *Watcher) events(ctx context.Context, fw *fsnotify.Watcher, root string, changes chan<- string) {
	defer fw.Close()
	send := func(p string) {
		select {
		case changes <- p:
		case <-ctx.Done():
		}
	}
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-fw.Events:
			if !ok {
				return
			}
			if ev.Op == fsnotify.Chmod {
				continue
			}
			if ev.Has(fsnotify.Create) {
				if info, err := os.Lstat(ev.Name); err == nil && info.IsDir() && !strings.HasPrefix(info.Name(), ".") {
					// Files can land in a new folder before it's watched; scanning
					// the folder itself covers them.
					if err := addTree(fw, ev.Name); err != nil {
						w.Log.Warn("watch new folder", "path", ev.Name, "err", err)
					}
				}
			}
			send(ev.Name)
		case err, ok := <-fw.Errors:
			if !ok {
				return
			}
			w.Log.Warn("watching folder", "path", root, "err", err)
			if errors.Is(err, fsnotify.ErrEventOverflow) {
				// Some changes were dropped; look at everything.
				send(root)
			}
		}
	}
}

// polledDir is what a polled folder looked like at the last check.
type polledDir struct {
	mtime   time.Time
	entries map[string]bool // name → is a folder
}

func (w *Watcher) poll(ctx context.Context, roots []string, changes chan<- string) {
	dirs := map[string]*polledDir{}
	for _, root := range roots {
		dirs[root] = nil // listed on the first pass, without reporting what's there
		w.pollDir(ctx, root, dirs, nil)
	}
	wait := w.PollInterval
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		start := time.Now()
		for _, dir := range slices.Sorted(maps.Keys(dirs)) {
			if ctx.Err() != nil {
				return
			}
			if _, still := dirs[dir]; !still {
				continue // dropped with a removed parent this pass
			}
			w.pollDir(ctx, dir, dirs, changes)
		}
		took := time.Since(start)
		wait = max(w.PollInterval, 4*took)
		if wait > w.PollInterval {
			w.Log.Debug("polling library folders is slow; checking less often", "folders", len(dirs), "took", took, "next", wait)
		}
	}
}

// pollDir checks one folder and reports what was added to or removed from
// it since the last check. Without a channel it just records the folder and
// everything under it.
func (w *Watcher) pollDir(ctx context.Context, dir string, dirs map[string]*polledDir, changes chan<- string) {
	info, err := os.Stat(dir)
	if err != nil {
		if _, parentKnown := dirs[filepath.Dir(dir)]; !parentKnown {
			return // a library folder that's missing; check again next time
		}
		dropDir(dirs, dir) // its parent reports it gone
		return
	}
	old := dirs[dir]
	if old != nil && info.ModTime().Equal(old.mtime) {
		return
	}
	list, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	now := &polledDir{mtime: info.ModTime(), entries: map[string]bool{}}
	for _, e := range list {
		if !strings.HasPrefix(e.Name(), ".") {
			now.entries[e.Name()] = e.IsDir()
		}
	}
	dirs[dir] = now
	report := func(p string) {
		if changes == nil {
			return
		}
		select {
		case changes <- p:
		case <-ctx.Done():
		}
	}
	var before map[string]bool
	if old != nil {
		before = old.entries
	}
	for name, isDir := range now.entries {
		p := filepath.Join(dir, name)
		if wasDir, had := before[name]; had && wasDir == isDir {
			continue
		}
		// Everything in a folder seen for the first time is new, unless this
		// is the first look at the library.
		report(p)
		if isDir {
			if _, known := dirs[p]; !known {
				dirs[p] = nil
				w.pollDir(ctx, p, dirs, nil)
			}
		}
	}
	if old == nil {
		return
	}
	for name, wasDir := range old.entries {
		if isDir, has := now.entries[name]; has && isDir == wasDir {
			continue
		}
		p := filepath.Join(dir, name)
		if _, has := now.entries[name]; !has {
			report(p)
		}
		if wasDir {
			dropDir(dirs, p)
		}
	}
}

// dropDir forgets a folder and every folder under it.
func dropDir(dirs map[string]*polledDir, dir string) {
	for d := range dirs {
		if under(d, []string{dir}) {
			delete(dirs, d)
		}
	}
}
