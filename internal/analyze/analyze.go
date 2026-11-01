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

	"github.com/yeixio/matinee/internal/intro"
	"github.com/yeixio/matinee/internal/probe"
	"github.com/yeixio/matinee/internal/store"
	"github.com/yeixio/matinee/internal/thumbs"
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

const (
	settingMode   = "mediaAnalysis"
	settingIntros = "introDetection" // "off" turns it off
)

// State says what the analysis is doing right now.
const (
	StateAnalyzing = "analyzing"
	StateWaiting   = "waiting"    // for playback to finish
	StateSlowDisk  = "slowDisk"   // files kept timing out; resting before trying again
	StateIntros    = "intros"     // looking for the intros of a season's episodes
	StateThumbs    = "thumbnails" // making a video's seek bar pictures
	StateIdle      = "idle"       // everything is analyzed
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
	// Intros, when set, looks for TV episodes' intros once every file is
	// analyzed, at the same pace and priority.
	Intros *intro.Detector
	// Thumbs, when set, makes the seek bar's pictures of every video after
	// that, when the admin has them made for all videos (see thumbs.Mode).
	Thumbs *thumbs.Generator

	mu         sync.Mutex
	mode       Mode
	loaded     bool
	intros     bool
	introsSet  bool
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
	// Intros is how far looking for TV episodes' intros is; absent when
	// this server doesn't.
	Intros *IntroStatus `json:"intros,omitempty"`
	// Thumbnails is how far making the seek bar's pictures is; absent when
	// this server doesn't.
	Thumbnails *ThumbStatus `json:"thumbnails,omitempty"`
}

// ThumbStatus counts videos: all of them, and the ones with seek bar
// pictures (or that couldn't be read for them).
type ThumbStatus struct {
	Mode  thumbs.Mode `json:"mode"`
	Total int         `json:"total"`
	Done  int         `json:"done"`
}

// IntroStatus counts video episodes: all of them, the ones looked at for an
// intro, and the ones an intro was found in.
type IntroStatus struct {
	On    bool `json:"on"`
	Total int  `json:"total"`
	Done  int  `json:"done"`
	Found int  `json:"found"`
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

// IntrosOn says whether to look for intros, an admin setting, on by default.
func (a *Analyzer) IntrosOn(ctx context.Context) (bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.introsSet {
		v, err := a.Store.Setting(ctx, settingIntros)
		if err != nil {
			return true, err
		}
		a.intros, a.introsSet = v != "off", true
	}
	return a.intros, nil
}

func (a *Analyzer) SetIntrosOn(ctx context.Context, on bool) error {
	v := "on"
	if !on {
		v = "off"
	}
	if err := a.Store.SetSetting(ctx, settingIntros, v); err != nil {
		return err
	}
	a.mu.Lock()
	a.intros, a.introsSet = on, true
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
	var is *IntroStatus
	if a.Intros != nil {
		on, err := a.IntrosOn(ctx)
		if err != nil {
			return Status{}, err
		}
		is = &IntroStatus{On: on}
		if is.Total, is.Done, is.Found, err = a.Store.IntroCounts(ctx, intro.Version); err != nil {
			return Status{}, err
		}
	}
	var ts *ThumbStatus
	if a.Thumbs != nil {
		m, err := a.Thumbs.Mode(ctx)
		if err != nil {
			return Status{}, err
		}
		ts = &ThumbStatus{Mode: m}
		if ts.Total, ts.Done, err = a.Store.ThumbnailCounts(ctx, thumbs.Version); err != nil {
			return Status{}, err
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	st := Status{Mode: mode, State: a.state, Total: total, Done: done, Unreadable: unreadable, Current: a.current, Intros: is, Thumbnails: ts}
	if st.State == "" {
		st.State = StateIdle
	}
	if mode == Off {
		st.State, st.Current = StateOff, ""
	}
	return st, nil
}

// Reanalyze has one file looked at again soon, whatever its state, its
// intro looked for again and its seek bar pictures made again.
func (a *Analyzer) Reanalyze(ctx context.Context, id int64) error {
	if err := a.Store.ClearMediaKey(ctx, id); err != nil {
		return err
	}
	if err := a.Store.ClearIntro(ctx, id); err != nil {
		return err
	}
	if err := a.Store.ClearThumbnails(ctx, id); err != nil {
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
			if a.findIntros(ctx) > 0 || a.makeThumbs(ctx) > 0 {
				continue // files may have changed meanwhile
			}
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

// findIntros has the intro detector look at every season that needs it,
// one season at a time, pausing like the analysis does. It returns how
// many episodes it looked at.
func (a *Analyzer) findIntros(ctx context.Context) int {
	if a.Intros == nil {
		return 0
	}
	if on, _ := a.IntrosOn(ctx); !on {
		return 0
	}
	first := true
	n, err := a.Intros.Pass(ctx, func(e store.IntroEpisode) bool {
		mode, _ := a.Mode(ctx)
		if on, _ := a.IntrosOn(ctx); !on || mode == Off {
			return false
		}
		if !first && mode == Gentle {
			a.sleep(ctx, gentlePause)
		}
		first = false
		for mode == Gentle && a.Busy != nil && a.Busy() {
			a.setState(StateWaiting, "")
			a.sleep(ctx, busyCheck)
			if ctx.Err() != nil {
				return false
			}
			if mode, _ = a.Mode(ctx); mode == Off {
				return false
			}
		}
		a.setState(StateIntros, a.seasonName(ctx, e))
		return ctx.Err() == nil
	})
	if err != nil && ctx.Err() == nil {
		a.Log.Warn("intro detection", "err", err)
	}
	if n > 0 {
		a.Log.Info("intro detection pass done", "episodes", n)
	}
	return n
}

// makeThumbs has the thumbnail generator make the seek bar's pictures of
// every video that needs them, one at a time, pausing like the analysis
// does. It returns how many videos it made pictures of.
func (a *Analyzer) makeThumbs(ctx context.Context) int {
	if a.Thumbs == nil {
		return 0
	}
	if m, _ := a.Thumbs.Mode(ctx); m != thumbs.All {
		return 0
	}
	first := true
	n, err := a.Thumbs.Pass(ctx, func(f store.ThumbnailFile) bool {
		mode, _ := a.Mode(ctx)
		if m, _ := a.Thumbs.Mode(ctx); m != thumbs.All || mode == Off {
			return false
		}
		// Making every video's pictures takes days on a big library; new
		// files are analyzed first, and the pass picks up after.
		if len(a.wake) > 0 {
			return false
		}
		if !first && mode == Gentle {
			a.sleep(ctx, gentlePause)
		}
		first = false
		for mode == Gentle && a.Busy != nil && a.Busy() {
			a.setState(StateWaiting, "")
			a.sleep(ctx, busyCheck)
			if ctx.Err() != nil {
				return false
			}
			if mode, _ = a.Mode(ctx); mode == Off {
				return false
			}
		}
		a.setState(StateThumbs, f.Title)
		return ctx.Err() == nil
	})
	if err != nil && ctx.Err() == nil {
		a.Log.Warn("thumbnails", "err", err)
	}
	if n > 0 {
		a.Log.Info("thumbnails pass done", "videos", n)
	}
	return n
}

// seasonName names a season the way people do: "Show, Season 2".
func (a *Analyzer) seasonName(ctx context.Context, e store.IntroEpisode) string {
	season, err := a.Store.Item(ctx, 0, e.SeasonID)
	if err != nil {
		return e.Title
	}
	if season.ParentID != nil {
		if show, err := a.Store.Item(ctx, 0, *season.ParentID); err == nil {
			return show.Title + " · " + season.Title
		}
	}
	return season.Title
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
