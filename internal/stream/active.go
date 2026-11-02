package stream

import "sort"

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
	// Accel names the hardware doing the work, such as "VAAPI"; empty for
	// the CPU. Mode is how much of it is on the GPU: cpu, encode or full.
	Accel     string   `json:"accel,omitempty"`
	AccelID   string   `json:"accelId,omitempty"`
	Mode      string   `json:"mode"`
	Quality   []string `json:"quality,omitempty"`
	BurnSub   bool     `json:"burnSub,omitempty"`
	Adaptive  bool     `json:"adaptive,omitempty"`
	FFmpegPID int      `json:"pid,omitempty"`
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
			info.Kind = "video"
		}
		if s.accel != nil {
			info.Accel, info.AccelID = s.accel.Name, string(s.accel.Kind)
		}
		if s.cmd != nil && s.cmd.Process != nil {
			info.FFmpegPID = s.cmd.Process.Pid
		}
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Started < out[j].Started })
	return out
}
