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
