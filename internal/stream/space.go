package stream

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/yeixio/matinee/internal/health"
	"github.com/yeixio/matinee/internal/store"
)

// Room for transcodes.
//
// A transcode writes the whole video into the transcode folder as it goes,
// and a repackaged 4K remux is as big as its file: a 100 GB movie needs
// 100 GB. Before FFmpeg starts, Start works out what the rest of the video
// will take. When that doesn't fit, the transcode saves space: it keeps only
// the minutes around where the player is (deleting pieces well behind it,
// and pausing FFmpeg once it's far enough ahead), so any length plays in a
// few gigabytes. Only when even that doesn't fit does Start refuse, with
// ErrNoSpace, which players show in plain words.
//
// While transcodes run, the room left is checked every few seconds, so ones
// that started with room (or something else filling the disk) switch to
// saving space before the disk is full rather than after. An FFmpeg that
// still runs out ("No space left on device") is stopped and remembered (see
// OutOfSpace), and Server health and the admin alerts say so.

// ErrNoSpace means the transcode folder hasn't room for even the few
// minutes of video a transcode that saves space keeps.
var ErrNoSpace = errors.New("not enough room in the transcode folder")

const (
	// spaceReserve is left free for everything else on the disk (the
	// database, the image cache), and as a margin before a Kubernetes
	// emptyDir's size limit evicts the pod.
	spaceReserve = 512 << 20
	// outOfSpaceKeep is how long a transcode stopped for room is remembered
	// for the player's "why" and Server health.
	outOfSpaceKeep = 30 * time.Minute
)

// Variables so tests can make them short.
var (
	// A transcode that saves space pauses FFmpeg once it's aheadSeconds
	// past the piece a player last asked for, starts it again once the
	// player is within resumeSeconds of that, and deletes pieces more than
	// behindSeconds behind the player. marginSeconds is added to what it
	// needs, for pieces being written and the moment before a pause.
	aheadSeconds  = 180
	resumeSeconds = 120
	behindSeconds = 120
	marginSeconds = 60
	// spaceTick is how often running transcodes' room is checked.
	spaceTick = 2 * time.Second
)

// SpaceLimit is a cap on the transcode folder below its disk's free space.
type SpaceLimit struct {
	// Size is how much the volume the transcode folder is on may hold,
	// when that's less than its disk: a Kubernetes emptyDir's sizeLimit,
	// which evicts the pod when it's passed. 0 means the disk itself.
	Size int64
	// Others is how much of Size things other than transcodes use (the
	// image cache, say); nil counts only transcodes.
	Others func() int64
}

// Space is the room transcodes have, for Server health.
type Space struct {
	// Free is what transcodes can still use (after the reserve kept for
	// everything else), Used what they use now, and Limit the cap from
	// MATINEE_CACHE_LIMIT, 0 for none.
	Free  int64 `json:"free"`
	Used  int64 `json:"used"`
	Limit int64 `json:"limit,omitempty"`
	// Short says running transcodes don't all fit: they keep only the
	// minutes around where they play.
	Short bool `json:"short,omitempty"`
	// OutOfSpace is when a transcode last couldn't start or stopped for
	// lack of room (milliseconds), 0 for not lately.
	OutOfSpace int64 `json:"outOfSpace,omitempty"`
}

// spaceState is the Transcoder's record of room; under Transcoder.mu.
type spaceState struct {
	short bool
	// out remembers viewers whose transcode couldn't start or stopped for
	// lack of room, and when.
	out     map[sessionKey]time.Time
	lastOut time.Time
}

// room is what transcodes can still write, after the reserve; ok is false
// when it can't be told (a system Matinee can't ask), and nothing is checked.
func (t *Transcoder) room() (free, used int64, ok bool) {
	if t.freeSpace != nil {
		free, ok = t.freeSpace()
		return free, dirSize(t.Dir), ok
	}
	d, ok := health.DiskUsage(t.Dir)
	if !ok {
		return 0, 0, false
	}
	used = dirSize(t.Dir)
	free = int64(d.Free)
	if t.Limit.Size > 0 {
		taken := used
		if t.Limit.Others != nil {
			taken += t.Limit.Others()
		}
		free = min(free, t.Limit.Size-taken)
	}
	return free - spaceReserve, used, true
}

// dirSize is how many bytes the files under dir take.
func dirSize(dir string) int64 {
	var n int64
	filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if info, err := d.Info(); err == nil {
				n += info.Size()
			}
		}
		return nil
	})
	return n
}

// DirSize is how many bytes the files under dir take, skipping the
// folders in skip and any folder on another disk (a volume mounted inside
// it), as a Kubernetes emptyDir counts what's in it.
func DirSize(dir string, skip ...string) int64 {
	root, err := os.Stat(dir)
	if err != nil {
		return 0
	}
	var n int64
	filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			for _, s := range skip {
				if path == s {
					return filepath.SkipDir
				}
			}
			if path != dir {
				if info, err := d.Info(); err == nil && !sameDevice(root, info) {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if info, err := d.Info(); err == nil {
			n += info.Size()
		}
		return nil
	})
	return n
}

// streamRate is about how many bytes a second a transcode of it writes.
// copyVideo repackages the picture as it is, so it's as big as the file;
// steps are the qualities it makes (all of them, for an adaptive stream).
func streamRate(it store.Item, copyVideo bool, steps []Step) float64 {
	const audio = 192_000.0 // bits a second
	switch {
	case it.VideoCodec == "":
		return 256_000.0 / 8
	case copyVideo:
		size, dur := fileSize(it), duration(it)
		if size > 0 && dur > 0 {
			return float64(size)/dur + audio/8
		}
		return 50e6 / 8 // a 4K remux's worth, when its size isn't known
	case len(steps) > 0:
		var bps float64
		for _, s := range steps {
			bps += float64(s.VideoBPS + s.AudioBPS)
		}
		return bps / 8
	default:
		// The original quality, re-encoded: up to 10 Mbps (see hwaccel's maxrate).
		return (10e6 + audio) / 8
	}
}

// fileSize is the size of it's file, or all its parts'.
func fileSize(it store.Item) int64 {
	if len(it.Parts) > 1 {
		var n int64
		for _, p := range it.Parts {
			n += p.Size
		}
		return n
	}
	if it.Size > 0 {
		return it.Size
	}
	if info, err := os.Stat(it.Path); err == nil {
		return info.Size()
	}
	return 0
}

// duration is it's length in seconds, 0 when it isn't known.
func duration(it store.Item) float64 {
	if it.DurationMs != nil && *it.DurationMs > 0 {
		return float64(*it.DurationMs) / 1000
	}
	return 0
}

// window is what a transcode that saves space keeps at most, at rate:
// the minutes behind and ahead of the player, and a minute's margin.
func window(rate float64) int64 {
	return int64(rate * float64(aheadSeconds+behindSeconds+marginSeconds))
}

// fit decides whether a transcode writing rate bytes a second for the
// rest of it from startSec fits: as it is, saving space, or not at all
// (ErrNoSpace), once pieces other transcodes are done with are cleared.
func (t *Transcoder) fit(it store.Item, startSec, rate float64) (saving bool, err error) {
	free, _, ok := t.room()
	if !ok {
		return false, nil
	}
	rest := duration(it) - startSec
	if rest <= 0 {
		// Unknown length: plan for a long film.
		rest = 3 * 60 * 60
	}
	// What the other running transcodes will still want, at least.
	t.mu.Lock()
	var others int64
	for _, s := range t.sessions {
		if s.rate > 0 {
			others += window(s.rate)
		}
	}
	t.mu.Unlock()
	if int64(rate*rest)+others <= free {
		return false, nil
	}
	need := window(rate)
	if need+others > free {
		// Running transcodes start saving space too, which may clear room.
		t.saveSpace()
		if free, _, _ = t.room(); need > free {
			t.Log.Warn("not enough room in the transcode folder to start a transcode",
				"item", it.ID, "free", free+spaceReserve, "needs", need+spaceReserve, "dir", t.Dir)
			return false, fmt.Errorf("%w: %s free, %s needed", ErrNoSpace, gigabytes(free+spaceReserve), gigabytes(need+spaceReserve))
		}
	}
	return true, nil
}

func gigabytes(n int64) string {
	return strconv.FormatFloat(float64(max(n, 0))/(1<<30), 'f', 1, 64) + " GB"
}

// watchSpace checks running transcodes' room every spaceTick until the
// transcoder closes.
func (t *Transcoder) watchSpace() {
	tick := time.NewTicker(spaceTick)
	defer tick.Stop()
	for {
		select {
		case <-t.stop:
			return
		case <-tick.C:
			t.checkSpace()
		}
	}
}

// checkSpace keeps running transcodes within their room: when they don't
// all fit, every one saves space; those that do clear old pieces and
// pause or go on.
func (t *Transcoder) checkSpace() {
	t.mu.Lock()
	var running []*session
	var need int64
	saving := false
	for _, s := range t.sessions {
		if s.rate > 0 && !isDone(s) {
			running = append(running, s)
			if !s.saving {
				need += window(s.rate)
			}
			saving = saving || s.saving
		}
	}
	if !saving {
		// Room again (or nobody watching): the next shortage is logged anew.
		t.space.short = false
	}
	t.mu.Unlock()
	if len(running) == 0 {
		return
	}
	if free, _, ok := t.room(); ok && free < need {
		t.saveSpace()
		return
	}
	for _, s := range running {
		t.keepWindow(s)
	}
}

// saveSpace makes every running transcode save space, and clears what
// they no longer need.
func (t *Transcoder) saveSpace() {
	t.mu.Lock()
	var running []*session
	newly := 0
	for _, s := range t.sessions {
		if s.rate > 0 && !isDone(s) {
			if !s.saving {
				s.saving = true
				newly++
			}
			running = append(running, s)
		}
	}
	if newly > 0 && !t.space.short {
		t.space.short = true
		t.Log.Warn("the transcode folder is short of room; transcodes keep only the minutes around where they play",
			"dir", t.Dir, "transcodes", len(running))
	}
	t.mu.Unlock()
	for _, s := range running {
		t.keepWindow(s)
	}
}

// keepWindow, for a transcode saving space, deletes pieces well behind its
// player and pauses FFmpeg while it's far enough ahead.
func (t *Transcoder) keepWindow(s *session) {
	t.mu.Lock()
	saving, at, kept := s.saving, s.lastSeg, s.keptFrom
	t.mu.Unlock()
	if !saving {
		return
	}
	if cut := at - behindSeconds/segmentSeconds; cut > kept {
		pruneSegments(s.dir, cut)
		t.mu.Lock()
		s.keptFrom = max(s.keptFrom, cut)
		t.mu.Unlock()
	}
	t.throttle(s)
}

// throttle pauses a saving transcode's FFmpeg once it has made aheadSeconds
// past its player, and starts it again once the player gets within
// resumeSeconds of its newest piece.
func (t *Transcoder) throttle(s *session) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if s.segFmt == "" || s.cmd == nil || s.cmd.Process == nil || isDone(s) || s.killed {
		return
	}
	switch {
	case !s.paused && s.saving && fileExists(fmt.Sprintf(s.segFmt, s.lastSeg+aheadSeconds/segmentSeconds)):
		if pauseProcess(s.cmd.Process) {
			s.paused, s.pausedAt = true, time.Now()
		}
	case s.paused && !fileExists(fmt.Sprintf(s.segFmt, s.lastSeg+resumeSeconds/segmentSeconds)):
		t.resumeLocked(s)
	}
}

// resumeLocked starts a paused FFmpeg again; t.mu is held.
func (t *Transcoder) resumeLocked(s *session) {
	if !s.paused {
		return
	}
	resumeProcess(s.cmd.Process)
	s.paused = false
	s.pausedFor += time.Since(s.pausedAt)
}

// pruneSegments deletes the pieces in dir numbered below cut, in every
// stream of an adaptive one.
func pruneSegments(dir string, cut int) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if m := segmentRe.FindStringSubmatch(e.Name()); m != nil {
			if n, _ := strconv.Atoi(m[2]); n < cut {
				os.Remove(filepath.Join(dir, e.Name()))
			}
		}
	}
}

// ranOut records that the viewer's transcode stopped, or couldn't start,
// for lack of room, and tells the admins (see OnNoSpace) at most hourly.
func (t *Transcoder) ranOut(k sessionKey, detail string) {
	t.mu.Lock()
	now := time.Now()
	if t.space.out == nil {
		t.space.out = map[sessionKey]time.Time{}
	}
	t.space.out[k] = now
	tell := now.Sub(t.space.lastOut) > time.Hour
	t.space.lastOut = now
	notify := t.OnNoSpace
	t.mu.Unlock()
	if tell && notify != nil {
		go notify(detail)
	}
}

// diskFull stops a transcode whose FFmpeg found the disk full, which it
// can't go on from, and clears room for the player to start again.
func (t *Transcoder) diskFull(s *session) {
	t.mu.Lock()
	s.noSpace = true
	k := s.key()
	// Stopped, but kept until the player moves on, so the pieces it asks
	// for next answer that the server is out of room (see ServeFile).
	if !s.killed && s.cmd.Process != nil {
		s.killed = true
		if s.paused {
			t.resumeLocked(s)
		}
		s.cmd.Process.Kill()
	}
	t.mu.Unlock()
	t.Log.Warn("the transcode folder is full; stopped the transcode", "session", s.id, "item", s.itemID, "dir", t.Dir)
	t.ranOut(k, "it filled up while a video played")
	t.saveSpace()
}

// roomAgain forgets that the viewer's transcode ran out of room, once one
// starts again.
func (t *Transcoder) roomAgain(k sessionKey) {
	t.mu.Lock()
	delete(t.space.out, k)
	t.mu.Unlock()
}

// OutOfSpace says whether user's transcode of item stopped, or couldn't
// start, for lack of room in the last half hour.
func (t *Transcoder) OutOfSpace(user, item int64) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	at, ok := t.space.out[sessionKey{user: user, item: item}]
	return ok && time.Since(at) < outOfSpaceKeep
}

// Space is the room transcodes have now, for Server health; ok is false
// when it can't be told.
func (t *Transcoder) Space() (Space, bool) {
	free, used, ok := t.room()
	if !ok {
		return Space{}, false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	sp := Space{Free: max(free, 0), Used: used, Limit: t.Limit.Size}
	for _, s := range t.sessions {
		if s.saving {
			sp.Short = true
		}
	}
	if time.Since(t.space.lastOut) < outOfSpaceKeep {
		sp.OutOfSpace = t.space.lastOut.UnixMilli()
	}
	return sp, true
}
