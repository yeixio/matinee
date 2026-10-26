// Package analyze slowly works through the libraries in the background,
// recording each file's full media info: resolution and HDR, codecs and
// bitrates, audio channels and languages, subtitles. It is built to go
// unnoticed: one file at a time at the lowest CPU and disk priority, with a
// pause between files and while anyone is watching something transcoded, so
// network shares and the server stay responsive. Progress lives in the
// database, so it picks up where it was after a restart, and a file is
// looked at again only when it changes.
package analyze

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/yeixio/matinee/internal/probe"
	"github.com/yeixio/matinee/internal/store"
)

// Mode is how hard the analysis works, an admin setting.
type Mode string

const (
	// Gentle analyzes a file every few seconds and waits while anyone is
	// watching something transcoded. The default.
	Gentle Mode = "gentle"
	// Normal analyzes files back to back, still at the lowest priority.
	Normal Mode = "normal"
	// Off pauses the analysis until it's turned back on.
	Off Mode = "off"
)

const settingMode = "mediaAnalysis"

// State says what the analysis is doing right now.
const (
	StateAnalyzing = "analyzing"
	StateWaiting   = "waiting"  // for playback to finish
	StateSlowDisk  = "slowDisk" // files kept timing out; resting before trying again
	StateIdle      = "idle"     // everything is analyzed
	StateOff       = "off"
)

// Timings, variables so tests can shorten them.
var (
	gentlePause   = 3 * time.Second  // between files in Gentle mode
	busyCheck     = 30 * time.Second // how often to look whether playback ended
	idleRecheck   = 30 * time.Minute // a full pass when nothing told us about changes
	retryTimedOut = time.Hour        // before trying a file that timed out again
	slowDiskRest  = 10 * time.Minute // after several timeouts in a row
	statTimeout   = 30 * time.Second // a stat that takes longer means a hung share
	batch         = 200
)

type Analyzer struct {
	Store  *store.Store
	Prober probe.Prober // should be LowPriority
	Log    *slog.Logger
	// Busy reports whether anyone is watching something the server is
	// transcoding right now. Gentle analysis waits until nobody is.
	Busy func() bool

	mu         sync.Mutex
	mode       Mode
	loaded     bool
	state      string
	current    string // title of the file being analyzed
	retryAfter map[int64]time.Time
	timeouts   int // in a row
	wake       chan struct{}
}

// Status is the analysis' progress, for admins.
type Status struct {
	Mode  Mode   `json:"mode"`
	State string `json:"state"`
	// Total is how many files the libraries have; Done how many are analyzed
	// as they are now, Unreadable among them the ones ffprobe couldn't read.
	Total      int    `json:"total"`
	Done       int    `json:"done"`
	Unreadable int    `json:"unreadable"`
	Current    string `json:"current,omitempty"`
}

func (a *Analyzer) init() {
	if a.wake == nil {
		a.wake = make(chan struct{}, 1)
		a.retryAfter = map[int64]time.Time{}
	}
}

// Wake has the analysis look for new or changed files now, after a scan
// or a setting change, instead of at its next regular pass.
func (a *Analyzer) Wake() {
	a.mu.Lock()
	a.init()
	a.mu.Unlock()
	select {
	case a.wake <- struct{}{}:
	default:
	}
}

// Mode returns the admin's setting, Gentle unless they changed it.
func (a *Analyzer) Mode(ctx context.Context) (Mode, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.loaded {
		v, err := a.Store.Setting(ctx, settingMode)
		if err != nil {
			return Gentle, err
		}
		a.mode, a.loaded = Mode(v), true
		if a.mode != Normal && a.mode != Off {
			a.mode = Gentle
		}
	}
	return a.mode, nil
}

func (a *Analyzer) SetMode(ctx context.Context, m Mode) error {
	if m != Gentle && m != Normal && m != Off {
		return fmt.Errorf("mode must be %s, %s or %s", Gentle, Normal, Off)
	}
	if err := a.Store.SetSetting(ctx, settingMode, string(m)); err != nil {
		return err
	}
	a.mu.Lock()
	a.mode, a.loaded = m, true
	a.mu.Unlock()
	a.Wake()
	return nil
}

func (a *Analyzer) Status(ctx context.Context) (Status, error) {
	mode, err := a.Mode(ctx)
	if err != nil {
		return Status{}, err
	}
	total, done, unreadable, err := a.Store.AnalysisCounts(ctx, probe.MediaInfoVersion)
	if err != nil {
		return Status{}, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	st := Status{Mode: mode, State: a.state, Total: total, Done: done, Unreadable: unreadable, Current: a.current}
	if st.State == "" {
		st.State = StateIdle
	}
	if mode == Off {
		st.State, st.Current = StateOff, ""
	}
	return st, nil
}

// Reanalyze has one file looked at again soon, whatever its state.
func (a *Analyzer) Reanalyze(ctx context.Context, id int64) error {
	if err := a.Store.ClearMediaKey(ctx, id); err != nil {
		return err
	}
	a.mu.Lock()
	a.init()
	delete(a.retryAfter, id)
	a.mu.Unlock()
	a.Wake()
	return nil
}

func (a *Analyzer) setState(state, current string) {
	a.mu.Lock()
	a.state, a.current = state, current
	a.mu.Unlock()
}

// Run analyzes files until ctx ends.
func (a *Analyzer) Run(ctx context.Context) {
	a.mu.Lock()
	a.init()
	a.mu.Unlock()
	for ctx.Err() == nil {
		mode, err := a.Mode(ctx)
		if err != nil {
			a.Log.Warn("media analysis setting", "err", err)
		}
		if mode == Off {
			a.setState(StateOff, "")
			a.sleep(ctx, 0)
			continue
		}
		n, err := a.pass(ctx)
		switch {
		case ctx.Err() != nil:
			return
		case err != nil:
			a.Log.Warn("media analysis", "err", err)
			a.sleep(ctx, time.Minute)
		case n == 0:
			a.setState(StateIdle, "")
			a.sleep(ctx, idleRecheck)
		default:
			a.Log.Info("media analysis pass done", "files", n)
		}
	}
}

// sleep waits for d (forever when 0), a Wake, or ctx to end. It reports
// whether it was woken.
func (a *Analyzer) sleep(ctx context.Context, d time.Duration) bool {
	var timer <-chan time.Time
	if d > 0 {
		t := time.NewTimer(d)
		defer t.Stop()
		timer = t.C
	}
	select {
	case <-ctx.Done():
	case <-timer:
	case <-a.wake:
		return true
	}
	return false
}

// pass goes over every file once, analyzing the ones that need it, and
// returns how many it analyzed.
func (a *Analyzer) pass(ctx context.Context) (int, error) {
	n := 0
	var after int64
	for {
		files, err := a.Store.AnalysisFiles(ctx, after, batch)
		if err != nil || len(files) == 0 {
			return n, err
		}
		for _, f := range files {
			after = f.ID
			if f.Key == store.MediaKey(probe.MediaInfoVersion, f.Size, f.Mtime) || a.retryLater(f.ID) {
				continue
			}
			// Settings can change mid-pass.
			mode, _ := a.Mode(ctx)
			if mode == Off {
				return n, nil
			}
			for mode == Gentle && a.Busy != nil && a.Busy() {
				a.setState(StateWaiting, "")
				a.sleep(ctx, busyCheck)
				if ctx.Err() != nil {
					return n, ctx.Err()
				}
				if mode, _ = a.Mode(ctx); mode == Off {
					return n, nil
				}
			}
			a.setState(StateAnalyzing, f.Title)
			if a.analyze(ctx, f) {
				n++
			}
			if ctx.Err() != nil {
				return n, ctx.Err()
			}
			a.mu.Lock()
			timeouts := a.timeouts
			a.mu.Unlock()
			switch {
			case timeouts >= 3:
				// The share is struggling; leave it alone for a while.
				a.Log.Warn("media analysis: files keep timing out, resting", "for", slowDiskRest)
				a.setState(StateSlowDisk, "")
				a.sleep(ctx, slowDiskRest)
				a.mu.Lock()
				a.timeouts = 0
				a.mu.Unlock()
			case mode == Gentle:
				a.sleep(ctx, gentlePause)
			}
			if ctx.Err() != nil {
				return n, ctx.Err()
			}
		}
	}
}

func (a *Analyzer) retryLater(id int64) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	t, ok := a.retryAfter[id]
	if ok && time.Now().After(t) {
		delete(a.retryAfter, id)
		return false
	}
	return ok
}

// analyze looks at one file and stores what it finds. It reports whether
// it stored anything.
func (a *Analyzer) analyze(ctx context.Context, f store.AnalysisFile) bool {
	info, err := statWithin(f.Path, statTimeout)
	if err != nil {
		// Gone or unreachable: the scanner decides what that means for the item.
		a.Log.Debug("media analysis: can't stat", "path", f.Path, "err", err)
		if errors.Is(err, context.DeadlineExceeded) {
			a.timedOut(f.ID)
		}
		return false
	}
	if info.Size() != f.Size || info.ModTime().Unix() != f.Mtime {
		// Changed since the last scan; analyzing it now would describe a
		// file the library doesn't list yet. The next scan updates it.
		return false
	}
	mi, err := a.Prober.Analyze(ctx, f.Path)
	switch {
	case errors.Is(err, context.Canceled):
		return false
	case errors.Is(err, context.DeadlineExceeded):
		a.Log.Warn("media analysis: ffprobe timed out", "path", f.Path)
		a.timedOut(f.ID)
		return false
	case err != nil:
		a.Log.Warn("media analysis: can't read file", "path", f.Path, "err", err)
		mi = probe.MediaInfo{Error: err.Error(), Video: []probe.VideoStream{}, Audio: []probe.AudioStream{}, Subtitles: []probe.Subtitle{}}
	}
	a.mu.Lock()
	a.timeouts = 0
	a.mu.Unlock()
	if mi.Size == 0 {
		mi.Size = f.Size
	}
	mi.AnalyzedAt = time.Now().Unix()
	b, err := json.Marshal(mi)
	if err != nil {
		return false
	}
	ok, err := a.Store.SetMediaInfo(ctx, f.ID, f.Size, f.Mtime, string(b), mi.Tags(), store.MediaKey(probe.MediaInfoVersion, f.Size, f.Mtime))
	if err != nil {
		a.Log.Warn("media analysis: save", "item", f.ID, "err", err)
		return false
	}
	return ok
}

// timedOut leaves a file that timed out alone for a while: a slow share
// isn't a broken file.
func (a *Analyzer) timedOut(id int64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.timeouts++
	a.retryAfter[id] = time.Now().Add(retryTimedOut)
}

// statWithin stats a file, giving up after d: a stat on a hung network
// share can block for minutes.
func statWithin(path string, d time.Duration) (os.FileInfo, error) {
	type result struct {
		info os.FileInfo
		err  error
	}
	c := make(chan result, 1)
	go func() {
		info, err := os.Stat(path)
		c <- result{info, err}
	}()
	select {
	case r := <-c:
		return r.info, r.err
	case <-time.After(d):
		return nil, fmt.Errorf("stat %s: %w", path, context.DeadlineExceeded)
	}
}
