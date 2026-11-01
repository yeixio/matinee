package intro

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/yeixio/matinee/internal/store"
)

// Version goes up when the detector changes enough that episodes it looked
// at should be looked at again.
const Version = 1

// Timings and sizes, variables so tests can shorten them.
var (
	// The intro is looked for in the first part of each episode: 30% of it,
	// but at least three minutes and at most ten, which covers long cold
	// opens without reading whole files.
	searchShare   = 0.3
	searchMin     = 3 * time.Minute
	searchMax     = 10 * time.Minute
	fileTimeout   = 5 * time.Minute // to decode one episode's search window
	retryTimedOut = time.Hour
	partners      = 4 // other episodes to compare one with before giving up
	batch         = 300
)

// Detector finds the intros of TV episodes, a season at a time, and stores
// them (see store.SetIntro).
type Detector struct {
	Store  *store.Store
	FFmpeg string
	Log    *slog.Logger
	// LowPriority is called with each FFmpeg's process id.
	LowPriority func(pid int)

	mu         sync.Mutex
	retryAfter map[int64]time.Time
}

// Pass looks at every season with an episode the detector hasn't looked at
// as it is now. Before each season it calls next with the season's first
// episode, which returns false to stop the pass (paused, or shutting
// down). It returns how many episodes it looked at.
func (d *Detector) Pass(ctx context.Context, next func(first store.IntroEpisode) bool) (int, error) {
	n := 0
	var after int64
	for {
		eps, err := d.Store.IntroEpisodes(ctx, after, batch)
		if err != nil || len(eps) == 0 {
			return n, err
		}
		for len(eps) > 0 {
			end := 1
			for end < len(eps) && eps[end].SeasonID == eps[0].SeasonID {
				end++
			}
			season := eps[:end]
			eps, after = eps[end:], season[0].SeasonID
			if !d.needsLook(season) {
				continue
			}
			if !next(season[0]) || ctx.Err() != nil {
				return n, ctx.Err()
			}
			n += d.Season(ctx, season)
		}
	}
}

func key(e store.IntroEpisode) string { return store.MediaKey(Version, e.Size, e.Mtime) }

func (d *Detector) needsLook(season []store.IntroEpisode) bool {
	for _, e := range season {
		if e.Key != key(e) && !d.retryLater(e.ID) {
			return true
		}
	}
	return false
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

func (d *Detector) timedOut(id int64) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.retryAfter == nil {
		d.retryAfter = map[int64]time.Time{}
	}
	d.retryAfter[id] = time.Now().Add(retryTimedOut)
}

// searchWindow is how much of an episode's start to look in.
func searchWindow(durationMs int64) time.Duration {
	if durationMs <= 0 {
		return searchMax
	}
	w := time.Duration(float64(durationMs)*searchShare) * time.Millisecond
	return min(max(w, searchMin), searchMax)
}

// Season finds the intros of one season's episodes that need looking at,
// comparing each with the episodes nearest it until two share an intro.
// It returns how many episodes it stored a result for.
func (d *Detector) Season(ctx context.Context, season []store.IntroEpisode) int {
	prints := map[int64]Print{}
	failed := map[int64]bool{}
	print := func(e store.IntroEpisode) (Print, bool) {
		if p, ok := prints[e.ID]; ok {
			return p, true
		}
		if failed[e.ID] {
			return Print{}, false
		}
		fctx, cancel := context.WithTimeout(ctx, fileTimeout)
		p, err := Fingerprint(fctx, d.FFmpeg, e.Path, searchWindow(e.DurationMs), d.LowPriority)
		cancel()
		if err != nil {
			failed[e.ID] = true
			if ctx.Err() == nil {
				d.Log.Warn("intro detection: can't read audio", "path", e.Path, "err", err)
			}
			if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
				d.timedOut(e.ID)
			}
			return Print{}, false
		}
		prints[e.ID] = p
		return p, true
	}
	found := map[int64]Segment{}
	// Episodes whose chapters say where the intro is need no listening.
	chapters := map[int64][2]int64{}
	for _, e := range season {
		var mi struct{ IntroStartMs, IntroEndMs int64 }
		if e.MediaInfo != "" && json.Unmarshal([]byte(e.MediaInfo), &mi) == nil && mi.IntroEndMs > mi.IntroStartMs {
			chapters[e.ID] = [2]int64{mi.IntroStartMs, mi.IntroEndMs}
		}
	}
	var todo []store.IntroEpisode
	for _, e := range season {
		if e.Key != key(e) && !d.retryLater(e.ID) {
			todo = append(todo, e)
		}
	}
	for _, e := range todo {
		if _, ok := found[e.ID]; ok || ctx.Err() != nil {
			continue
		}
		if _, ok := chapters[e.ID]; ok {
			continue
		}
		pe, ok := print(e)
		if !ok {
			continue
		}
		// Nearest episodes first; ones known to have an intro before ones
		// known not to.
		others := slices.Clone(season)
		others = slices.DeleteFunc(others, func(o store.IntroEpisode) bool { return o.ID == e.ID })
		rank := func(o store.IntroEpisode) int {
			dist := abs(o.Index - e.Index)
			if o.Key == key(o) && o.EndMs <= o.StartMs {
				dist += 1000 // looked at before and nothing found
			}
			return dist
		}
		slices.SortStableFunc(others, func(a, b store.IntroEpisode) int { return rank(a) - rank(b) })
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
			inE, inO, ok := Match(pe, po)
			if !ok {
				continue
			}
			found[e.ID] = inE
			if _, ok := found[o.ID]; !ok {
				found[o.ID] = inO
			}
			break
		}
	}
	if ctx.Err() != nil {
		return 0
	}
	n := 0
	for _, e := range season {
		seg, hit := found[e.ID]
		isTodo := slices.ContainsFunc(todo, func(t store.IntroEpisode) bool { return t.ID == e.ID })
		switch {
		case isTodo && failed[e.ID] && !hit:
			if d.retryLater(e.ID) {
				continue // timed out: try again later
			}
			// Unreadable: store nothing found, until the file changes.
		case isTodo:
		case hit && e.Key == key(e) && e.EndMs <= e.StartMs:
			// Looked at before with nobody to compare with; now there is.
		default:
			continue
		}
		var start, end int64
		if c, ok := chapters[e.ID]; ok && isTodo {
			start, end = c[0], c[1]
		} else if hit {
			start, end = seg.StartMs(), seg.EndMs()
			if start < 2000 {
				start = 0 // from the very start
			}
		}
		if err := d.Store.SetIntro(ctx, e.ID, start, end, key(e)); err != nil {
			d.Log.Warn("intro detection: save", "item", e.ID, "err", err)
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
