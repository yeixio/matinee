// Package slowfs keeps requests from waiting on a network share that
// stopped answering. When an SMB or NFS server goes quiet, a stat or a
// directory listing on it can block for minutes while the kernel tries to
// reconnect, and nothing can cancel it. A page that reads the share as it
// loads (a title's extras, a cover in its folder) would hang with it.
//
// Do runs such a call with a deadline. If the call doesn't come back in
// time, the request goes on without it, and the share counts as not
// answering until the call returns: later calls on it give up at once
// instead of piling up behind the first. Stuck lists the shares that are
// not answering, for Server health.
package slowfs

import (
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// Wait is how long a quick call, such as a stat or a folder listing, may
// take before the request goes on without it. Reading a file, as FFmpeg
// does, gets longer; see DoWithin.
var Wait = 3 * time.Second

// Log hears when a share stops and starts answering again, which Server
// health then lists among its warnings. Nil logs nothing.
var Log *slog.Logger

// ErrNotAnswering is a call on a share that didn't answer in time, or that
// is still busy with one that didn't.
var ErrNotAnswering = errors.New("the network share isn't answering")

var (
	mu    sync.Mutex
	stuck = map[string]*share{}
)

type share struct {
	since time.Time
	calls int // calls that timed out and haven't come back yet
}

// Share is a share that isn't answering, and since when.
type Share struct {
	Root  string
	Since time.Time
}

// Root is the part of path that stands for its share: its first two
// folders, such as /media/movies or \\nas\media, which is where a share is
// usually mounted.
func Root(path string) string {
	clean := filepath.ToSlash(path)
	lead := ""
	switch {
	case strings.HasPrefix(clean, "//"): // a UNC path, \\nas\share
		lead = "//"
	case strings.HasPrefix(clean, "/"):
		lead = "/"
	}
	clean = strings.TrimPrefix(filepath.ToSlash(filepath.Clean(clean)), "/")
	clean = strings.TrimPrefix(clean, "/")
	parts := strings.SplitN(clean, "/", 3)
	if len(parts) > 2 {
		parts = parts[:2]
	}
	return filepath.FromSlash(lead + strings.Join(parts, "/"))
}

// Do runs fn, a quick call on the share path is on, giving up after Wait.
// It gives up at once while an earlier call on the same share hasn't come
// back.
func Do[T any](path string, fn func() (T, error)) (T, error) {
	return DoWithin(path, Wait, fn)
}

// DoWithin is Do for a call that may take up to wait on a share that's
// answering, such as FFmpeg reading part of a video.
func DoWithin[T any](path string, wait time.Duration, fn func() (T, error)) (T, error) {
	var zero T
	root := Root(path)
	mu.Lock()
	if stuck[root] != nil {
		mu.Unlock()
		return zero, ErrNotAnswering
	}
	mu.Unlock()
	type result struct {
		v   T
		err error
	}
	done := make(chan result, 1)
	timedOut := false // guarded by mu once the timer fires
	go func() {
		v, err := fn()
		mu.Lock()
		if timedOut {
			if sh := stuck[root]; sh != nil {
				if sh.calls--; sh.calls <= 0 {
					delete(stuck, root)
					if Log != nil {
						Log.Info("network share is answering again", "share", root, "after", time.Since(sh.since).Round(time.Second))
					}
				}
			}
		}
		mu.Unlock()
		done <- result{v, err}
	}()
	t := time.NewTimer(wait)
	defer t.Stop()
	select {
	case r := <-done:
		return r.v, r.err
	case <-t.C:
	}
	mu.Lock()
	defer mu.Unlock()
	select {
	case r := <-done: // came back just now
		return r.v, r.err
	default:
	}
	timedOut = true
	sh := stuck[root]
	if sh == nil {
		sh = &share{since: time.Now()}
		stuck[root] = sh
		if Log != nil {
			Log.Warn("network share isn't answering; pages that need it go on without it until it does", "share", root, "path", path)
		}
	}
	sh.calls++
	return zero, ErrNotAnswering
}

// Stuck lists the shares that aren't answering, by root.
func Stuck() []Share {
	mu.Lock()
	defer mu.Unlock()
	var out []Share
	for root, sh := range stuck {
		out = append(out, Share{Root: root, Since: sh.since})
	}
	slices.SortFunc(out, func(a, b Share) int { return strings.Compare(a.Root, b.Root) })
	return out
}

// Answering reports whether the share path is on answers a quick stat now.
// A background job asks after a file timed out, to tell a damaged file
// from a share that stalled.
func Answering(path string) bool {
	_, err := Do(path, func() (struct{}, error) { _, err := os.Stat(path); return struct{}{}, err })
	return !errors.Is(err, ErrNotAnswering)
}

// GiveUpAfter is how many times in a row reading a file may time out, with
// its share answering each time, before a background job takes it for
// damaged (say, a video whose seek index was never written, which FFmpeg
// crawls through on every jump) and leaves it alone until it changes. Once:
// a background job looks at each file once, and again only when it
// changes, so a timeout with the share answering is that look's answer. A
// count kept in memory would also start over with every restart, and never
// reach more.
const GiveUpAfter = 1

// Timeouts counts, per file, reads that timed out while the file's share
// was answering. The zero value is ready to use.
type Timeouts struct {
	mu    sync.Mutex
	count map[int64]int
}

// Add counts a timeout on file id at path, unless the share isn't
// answering, and reports whether the file has now timed out GiveUpAfter
// times and should be left alone.
func (t *Timeouts) Add(id int64, path string) (giveUp bool) {
	if !Answering(path) {
		return false // the share's fault, not the file's
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.count == nil {
		t.count = map[int64]int{}
	}
	t.count[id]++
	if t.count[id] >= GiveUpAfter {
		delete(t.count, id)
		return true
	}
	return false
}
