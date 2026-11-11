package stream

import (
	"os"
	"regexp"
	"sort"
	"time"
)

var firstStreamSegment = regexp.MustCompile(`^seg(_0_)?\d{5}\.(ts|m4s)$`)

// Active is how many transcodes are running, so background work can wait
// until nobody is watching.
func (t *Transcoder) Active() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.sessions)
}

// SessionInfo describes a running transcode, for the admin dashboard.
type SessionInfo struct {
	ID     string `json:"id"`
	UserID int64  `json:"userId"`
	ItemID int64  `json:"itemId"`
	// Started is when FFmpeg started (milliseconds), StartSec where in the
	// file it started, and LastSeen when a player last asked for a piece.
	Started  int64   `json:"started"`
	StartSec float64 `json:"startSec"`
	LastSeen int64   `json:"lastSeen"`
	// Kind is what FFmpeg does: "video" converts the picture, "copy" only
	// repackages it (the sound may still be converted), "audio" converts
	// sound alone.
	Kind string `json:"kind"`
	// Why says why the picture is converted: codec (the player doesn't
	// decode it), hdr (its screen doesn't show the file's HDR), subtitle
	// (one is drawn in) or quality (a lower one was asked for). Empty
	// when the picture is copied, or for sound.
	Why string `json:"why,omitempty"`
	// ToneMapped says an HDR picture is converted to SDR.
	ToneMapped bool `json:"toneMapped,omitempty"`
	// Accel names the hardware doing the work, such as "VAAPI"; empty for
	// the CPU. Mode is how much of it is on the GPU: cpu, encode or full.
	Accel     string   `json:"accel,omitempty"`
	AccelID   string   `json:"accelId,omitempty"`
	Mode      string   `json:"mode"`
	Quality   []string `json:"quality,omitempty"`
	BurnSub   bool     `json:"burnSub,omitempty"`
	Adaptive  bool     `json:"adaptive,omitempty"`
	FFmpegPID int      `json:"pid,omitempty"`
	// Speed is how fast FFmpeg makes the stream, as a multiple of real
	// time: below 1 it can't keep up. 0 once it has finished, or before
	// it has made anything.
	Speed float64 `json:"speed,omitempty"`
	// Saving says the transcode folder hasn't room for all of it, so it
	// keeps only the minutes around where it plays (see space.go); Waiting
	// says FFmpeg is paused, far enough ahead of the player.
	Saving  bool `json:"saving,omitempty"`
	Waiting bool `json:"waiting,omitempty"`
}

// speed is how many seconds of video s has made for each second it's been
// running, from its pieces; 0 when it's done or has made none. t.mu is held.
func (s *session) speed(now time.Time) float64 {
	if s.dir == "" || isDone(s) {
		return 0
	}
	// Time paused to save space (see space.go) isn't time converting.
	paused := s.pausedFor
	if s.paused {
		paused += now.Sub(s.pausedAt)
	}
	elapsed := (now.Sub(s.started) - paused).Seconds()
	entries, err := os.ReadDir(s.dir)
	if err != nil || elapsed <= 0 {
		return 0
	}
	// The newest of one stream's pieces (the only one, or an adaptive
	// stream's first): older ones may be cleared to save space.
	n := 0
	for _, e := range entries {
		if firstStreamSegment.MatchString(e.Name()) {
			if i, ok := segmentNumber(e.Name()); ok {
				n = max(n, i+1)
			}
		}
	}
	if n == 0 {
		return 0
	}
	return float64(n*segmentSeconds) / elapsed
}

// SessionFor is the running transcode of item for user, if there is one.
func (t *Transcoder) SessionFor(user, item int64) (SessionInfo, bool) {
	for _, s := range t.Sessions() {
		if s.UserID == user && s.ItemID == item {
			return s, true
		}
	}
	return SessionInfo{}, false
}

// Sessions lists the running transcodes, oldest first.
func (t *Transcoder) Sessions() []SessionInfo {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]SessionInfo, 0, len(t.sessions))
	for _, s := range t.sessions {
		info := SessionInfo{ID: s.id, UserID: s.userID, ItemID: s.itemID, Started: s.started.UnixMilli(), StartSec: s.startSec,
			LastSeen: s.lastSeen.UnixMilli(), Mode: s.mode.String(), Quality: s.quality, BurnSub: s.burnSub, Adaptive: len(s.quality) > 1}
		switch {
		case s.audioOnly:
			info.Kind = "audio"
		case s.copyVideo:
			info.Kind = "copy"
		default:
			info.Kind, info.Why, info.ToneMapped = "video", s.why, s.toneMapped
		}
		if s.accel != nil {
			info.Accel, info.AccelID = s.accel.Name, string(s.accel.Kind)
		}
		if s.cmd != nil && s.cmd.Process != nil {
			info.FFmpegPID = s.cmd.Process.Pid
		}
		if !s.audioOnly && !s.live {
			info.Speed = s.speed(time.Now())
		}
		info.Saving, info.Waiting = s.saving, s.paused
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Started < out[j].Started })
	return out
}
