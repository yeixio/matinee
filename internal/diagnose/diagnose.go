// Package diagnose works out why playback stalled, from what the player
// saw (how fast pieces arrived, how much it had buffered) and what the
// server knows (how fast it converts, how fast the file reads), so it can
// be said in plain words instead of guessed at.
package diagnose

import (
	"strconv"
	"sync"
	"time"
)

// Facts are what's known about one playback when it stalled. Zero means
// not known.
type Facts struct {
	// DownloadBps is how fast the player was getting the stream, as it
	// measured it, and NeededBps what the stream it plays takes.
	DownloadBps int64
	NeededBps   int64
	// Converting: the server converts the picture (not only the sound),
	// and Why says why (stream.Convert*). Speed is how fast it converts,
	// as a multiple of real time; GPU says a GPU does it.
	Converting bool
	Why        string
	Speed      float64
	GPU        bool
	// ReadMs is how long reading a piece of the file took the server just
	// now, and ReadBps how fast it read.
	ReadMs  int64
	ReadBps int64
	// Adaptive: the player can step the quality down (and may have).
	Adaptive bool
	// Remote: the player is away from home.
	Remote bool
}

// The causes Explain finds, most specific first.
const (
	// The server reads the file too slowly, as from a busy network share.
	CauseSlowDisk = "slowDisk"
	// The server can't convert as fast as the video plays.
	CauseSlowConvert = "slowConvert"
	// The connection between the player and the server is slower than the stream.
	CauseSlowConnection = "slowConnection"
	// Nothing found on the server's side or in the speed; Wi-Fi dropping
	// for a moment, say.
	CauseUnknown = "unknown"
)

// Cause is why playback stalled. Values fill in its wording, such as
// "have" and "need" in Mbps. Tip is what an admin could change, for
// admins only: gpu, lowerQuality, adaptive, disk, ""
type Cause struct {
	Code   string            `json:"code"`
	Values map[string]string `json:"values,omitempty"`
	Tip    string            `json:"tip,omitempty"`
	// Converted says why the server converts this video at all (codec,
	// hdr, subtitle, quality), which is worth knowing whatever the cause.
	Converted string `json:"converted,omitempty"`
	At        int64  `json:"at"` // milliseconds
}

// Limits a reading is judged against.
const (
	slowReadMs = 2000
	// The stream has to arrive at least this much faster than it plays.
	headroom = 1.1
	// A conversion slower than this (× real time) can't keep up.
	minSpeed = 1.0
)

// Explain finds the likeliest cause of a stall from f.
func Explain(f Facts) Cause {
	c := Cause{Code: CauseUnknown, Values: map[string]string{}}
	if f.Converting {
		c.Converted = f.Why
	}
	mbps := func(bps int64) string {
		v := float64(bps) / 1e6
		if v >= 10 {
			return strconv.FormatFloat(v, 'f', 0, 64)
		}
		return strconv.FormatFloat(v, 'f', 1, 64)
	}
	switch {
	case f.ReadMs >= slowReadMs || f.ReadBps > 0 && f.NeededBps > 0 && float64(f.ReadBps) < float64(f.NeededBps)*headroom:
		c.Code, c.Tip = CauseSlowDisk, "disk"
		c.Values["seconds"] = strconv.FormatFloat(float64(f.ReadMs)/1000, 'f', 1, 64)
	case f.Converting && f.Speed > 0 && f.Speed < minSpeed:
		c.Code = CauseSlowConvert
		c.Values["speed"] = strconv.FormatFloat(f.Speed, 'f', 1, 64)
		c.Tip = "lowerQuality"
		if !f.GPU {
			c.Tip = "gpu"
		}
	case f.DownloadBps > 0 && f.NeededBps > 0 && float64(f.DownloadBps) < float64(f.NeededBps)*headroom:
		c.Code = CauseSlowConnection
		c.Values["have"], c.Values["need"] = mbps(f.DownloadBps), mbps(f.NeededBps)
		if !f.Adaptive {
			c.Tip = "adaptive"
		}
	}
	return c
}

// Log keeps the latest cause for each person and item for a while, so a
// player's "why" and Server health can show it.
type Log struct {
	// Keep is how long a cause is kept; zero is an hour.
	Keep time.Duration

	mu     sync.Mutex
	causes map[key]Cause
	now    func() time.Time
}

type key struct{ user, item int64 }

func (l *Log) clock() time.Time {
	if l.now != nil {
		return l.now()
	}
	return time.Now()
}

func (l *Log) keep() time.Duration {
	if l.Keep > 0 {
		return l.Keep
	}
	return time.Hour
}

// Add records c for user's playback of item, stamping it now.
func (l *Log) Add(user, item int64, c Cause) Cause {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.causes == nil {
		l.causes = map[key]Cause{}
	}
	now := l.clock()
	c.At = now.UnixMilli()
	l.causes[key{user, item}] = c
	// Forget old ones as new ones come.
	for k, v := range l.causes {
		if now.Sub(time.UnixMilli(v.At)) > l.keep() {
			delete(l.causes, k)
		}
	}
	return c
}

// Latest is the last cause for user's playback of item within within (or
// Keep when zero).
func (l *Log) Latest(user, item int64, within time.Duration) (Cause, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	c, ok := l.causes[key{user, item}]
	if within <= 0 {
		within = l.keep()
	}
	if !ok || l.clock().Sub(time.UnixMilli(c.At)) > within {
		return Cause{}, false
	}
	return c, true
}
