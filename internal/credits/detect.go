// Package credits finds where the closing credits of episodes and movies
// start, so players can offer the next episode, or to skip the credits,
// from there instead of from the very end.
//
// A chapter named Credits, when a file has one, comes first (see probe).
// Otherwise episodes of a season usually share their credits' music, so the
// package fingerprints the end of each episode's soundtrack with the intro
// detector's fingerprint (internal/intro) and takes the longest stretch two
// episodes have in common there. Movies, and episodes that share nothing,
// are looked at instead: credits are mostly writing on black, so the last
// long stretch of dark pictures near the end is the credits.
package credits

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/yeixio/matinee/internal/intro"
	"github.com/yeixio/matinee/internal/probe"
	"github.com/yeixio/matinee/internal/slowfs"
	"github.com/yeixio/matinee/internal/store"
)

// Version goes up when the detector changes enough that videos it looked at
// should be looked at again.
const Version = 1

// Where a result came from, as the API says.
const (
	FromChapters = "chapters"
	FromAudio    = "audio"
	FromVideo    = "video"
)

// Timings and sizes, variables so tests can shorten them.
var (
	// Episodes' credits are looked for in their last 30%, at least three
	// minutes and at most ten; movies' in their last 20%, at least four
	// minutes and at most fifteen, since a film's credits run long.
	episodeShare, episodeMin, episodeMax = 0.3, 3 * time.Minute, 10 * time.Minute
	movieShare, movieMin, movieMax       = 0.2, 4 * time.Minute, 15 * time.Minute
	// MinShared is the shortest stretch of music episodes must share at
	// their end to count as credits; shorter is a sting or a channel ident.
	MinShared     = 15 * time.Second
	fileTimeout   = 5 * time.Minute // to read one video's end, sound or picture
	retryTimedOut = time.Hour
	partners      = 4 // other episodes to compare one with before giving up
	batch         = 300
	// toEnd: a shared stretch reaching this close to the end runs to it.
	toEnd = 3 * time.Second
)

// Detector finds where the end credits of episodes and movies start, a
// season or a movie at a time, and stores it (see store.SetEndCredits).
type Detector struct {
	Store  *store.Store
	FFmpeg string
	Log    *slog.Logger
	// LowPriority is called with each FFmpeg's process id.
	LowPriority func(pid int)

	mu         sync.Mutex
	retryAfter map[int64]time.Time
	timeouts   slowfs.Timeouts
}

// Pass looks at every season and movie with a video the detector hasn't
// looked at as it is now. Before each one it calls next with its first
// video, which returns false to stop the pass (paused, or shutting down).
// It returns how many videos it looked at.
func (d *Detector) Pass(ctx context.Context, next func(first store.CreditsVideo) bool) (int, error) {
	n := 0
	var after int64
	for {
		vs, err := d.Store.CreditsVideos(ctx, after, batch)
		if err != nil || len(vs) == 0 {
			return n, err
		}
		for len(vs) > 0 {
			end := 1
			for end < len(vs) && vs[end].Group == vs[0].Group {
				end++
			}
			group := vs[:end]
			vs, after = vs[end:], group[0].Group
			if !d.needsLook(group) {
				continue
			}
			if !next(group[0]) || ctx.Err() != nil {
				return n, ctx.Err()
			}
			n += d.Group(ctx, group)
		}
	}
}

func key(v store.CreditsVideo) string { return store.MediaKey(Version, v.Size, v.Mtime) }

func (d *Detector) todo(v store.CreditsVideo) bool { return v.Key != key(v) && !d.retryLater(v.ID) }

func (d *Detector) needsLook(group []store.CreditsVideo) bool {
	return slices.ContainsFunc(group, d.todo)
}

func (d *Detector) retryLater(id int64) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	t, ok := d.retryAfter[id]
	if ok && time.Now().After(t) {
		delete(d.retryAfter, id)
		return false
	}
	return ok
}

// timedOut has a file that timed out tried again later, or, once it keeps
// timing out with its share answering or its share keeps stopping while
// it's read, stored as unreadable until it changes (see slowfs.Timeouts).
// key is the file's version, see store.MediaKey.
func (d *Detector) timedOut(ctx context.Context, id int64, path, key string, durationMs int64) {
	var stalls slowfs.Stalls
	if d.Store != nil {
		stalls = d.Store
	}
	switch d.timeouts.Add(ctx, stalls, id, path, key) {
	case slowfs.CheckFile:
		if probe.Damaged(ctx, d.FFmpeg, path, durationMs, d.LowPriority) {
			d.Log.Warn("credits detection: file keeps timing out; leaving it until it changes (it may be damaged)", "path", path)
			return
		}
	case slowfs.SetAside:
		d.Log.Warn("credits detection: the network share keeps stopping while this file is read; leaving it until it changes (it may be damaged)", "path", path)
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.retryAfter == nil {
		d.retryAfter = map[int64]time.Time{}
	}
	d.retryAfter[id] = time.Now().Add(retryTimedOut)
}

// tail is how much of a video's end to look at.
func tail(v store.CreditsVideo) time.Duration {
	share, lo, hi := episodeShare, episodeMin, episodeMax
	if v.Kind == "movie" {
		share, lo, hi = movieShare, movieMin, movieMax
	}
	dur := time.Duration(v.DurationMs) * time.Millisecond
	w := min(max(time.Duration(float64(dur)*share), lo), hi)
	return min(w, dur)
}

// tailStart is where in the file the looked-at end begins.
func tailStart(v store.CreditsVideo) time.Duration {
	return time.Duration(v.DurationMs)*time.Millisecond - tail(v)
}

type found struct {
	start, end time.Duration // end 0: to the end
	source     string
}

// Group finds the end credits of one season's episodes, or one movie, that
// need looking at. It returns how many videos it stored a result for.
func (d *Detector) Group(ctx context.Context, group []store.CreditsVideo) int {
	got := map[int64]found{}
	failed := map[int64]bool{}
	fail := func(v store.CreditsVideo, what string, err error) {
		failed[v.ID] = true
		if ctx.Err() != nil {
			return
		}
		d.Log.Warn("credits detection: can't read "+what, "path", v.Path, "err", err)
		if errors.Is(err, context.DeadlineExceeded) {
			d.timedOut(ctx, v.ID, v.Path, store.MediaKey(0, v.Size, v.Mtime), v.DurationMs)
		}
	}
	// Chapters first: they need no listening or looking.
	for _, v := range group {
		var mi struct{ CreditsMs int64 }
		if v.MediaInfo != "" && json.Unmarshal([]byte(v.MediaInfo), &mi) == nil && mi.CreditsMs > 0 {
			got[v.ID] = found{start: time.Duration(mi.CreditsMs) * time.Millisecond, source: FromChapters}
		}
	}
	var todo []store.CreditsVideo
	for _, v := range group {
		if d.todo(v) {
			todo = append(todo, v)
		}
	}

	// Episodes: the music their ends share.
	prints := map[int64]intro.Print{}
	print := func(v store.CreditsVideo) (intro.Print, bool) {
		if p, ok := prints[v.ID]; ok {
			return p, true
		}
		if failed[v.ID] || v.DurationMs <= 0 {
			return intro.Print{}, false
		}
		fctx, cancel := context.WithTimeout(ctx, fileTimeout)
		p, err := intro.FingerprintFrom(fctx, d.FFmpeg, v.Path, tailStart(v), tail(v), d.LowPriority)
		cancel()
		if err != nil {
			fail(v, "audio", err)
			return intro.Print{}, false
		}
		prints[v.ID] = p
		return p, true
	}
	shared := func(v store.CreditsVideo, s intro.Segment) (found, bool) {
		start := tailStart(v) + time.Duration(s.StartMs())*time.Millisecond
		end := tailStart(v) + time.Duration(s.EndMs())*time.Millisecond
		dur := time.Duration(v.DurationMs) * time.Millisecond
		if dur-end > max(afterSlack, end-start) {
			return found{}, false // shared, but too far from the end to be the credits
		}
		if dur-end < toEnd {
			end = 0
		}
		return found{start, end, FromAudio}, true
	}
	if len(group) > 1 {
		for _, v := range todo {
			if _, ok := got[v.ID]; ok || ctx.Err() != nil {
				continue
			}
			pv, ok := print(v)
			if !ok {
				continue
			}
			others := slices.DeleteFunc(slices.Clone(group), func(o store.CreditsVideo) bool { return o.ID == v.ID })
			slices.SortStableFunc(others, func(a, b store.CreditsVideo) int { return abs(a.Index-v.Index) - abs(b.Index-v.Index) })
			tried := 0
			for _, o := range others {
				if tried >= partners || ctx.Err() != nil {
					break
				}
				po, ok := print(o)
				if !ok {
					continue
				}
				tried++
				inV, inO, ok := intro.MatchWithin(pv, po, MinShared, tail(v))
				if !ok {
					continue
				}
				fv, okV := shared(v, inV)
				fo, okO := shared(o, inO)
				if !okV || !okO {
					continue
				}
				got[v.ID] = fv
				if _, ok := got[o.ID]; !ok {
					got[o.ID] = fo
				}
				break
			}
		}
	}

	// Movies, and episodes that share nothing: the picture going dark.
	for _, v := range todo {
		if _, ok := got[v.ID]; ok || v.DurationMs <= 0 || ctx.Err() != nil {
			continue
		}
		fctx, cancel := context.WithTimeout(ctx, fileTimeout)
		frames, err := Darkness(fctx, d.FFmpeg, v.Path, tailStart(v), tail(v), d.LowPriority)
		cancel()
		if err != nil {
			fail(v, "video", err)
			continue
		}
		dur := time.Duration(v.DurationMs) * time.Millisecond
		if start, end, ok := FindDark(frames, dur); ok {
			got[v.ID] = found{start, end, FromVideo}
		}
	}
	if ctx.Err() != nil {
		return 0
	}

	n := 0
	for _, v := range group {
		f, hit := got[v.ID]
		isTodo := slices.ContainsFunc(todo, func(t store.CreditsVideo) bool { return t.ID == v.ID })
		switch {
		case isTodo && failed[v.ID] && !hit:
			if d.retryLater(v.ID) {
				continue // timed out: try again later
			}
			// Unreadable: store nothing found, until the file changes.
		case isTodo:
		case hit && f.source == FromAudio && v.Key == key(v) && v.StartMs <= 0:
			// Looked at before with nobody to compare with; now there is.
		default:
			continue
		}
		if err := d.Store.SetEndCredits(ctx, v.ID, f.start.Milliseconds(), f.end.Milliseconds(), f.source, key(v)); err != nil {
			d.Log.Warn("credits detection: save", "item", v.ID, "err", err)
			continue
		}
		if isTodo {
			n++
		}
	}
	return n
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
