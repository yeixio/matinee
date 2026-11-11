package api

import (
	"crypto/rand"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/yeixio/matinee/internal/diagnose"
	"github.com/yeixio/matinee/internal/store"
	"github.com/yeixio/matinee/internal/stream"
)

// Why playback buffers (see internal/diagnose): players report a stall
// with what they measured, the server adds what it knows, and the cause
// comes back in plain words for the player and for Server health.

// playbackReport is what a player says about a moment of playback.
type playbackReport struct {
	// Kind is stall (it stopped to buffer) or error (it gave up).
	Kind       string `json:"kind"`
	PositionMs int64  `json:"positionMs"`
	// DownloadBps is how fast pieces were arriving, as the player measured
	// it, and BufferMs how much it had buffered when it stopped.
	DownloadBps int64 `json:"downloadBps"`
	BufferMs    int64 `json:"bufferMs"`
	// NeededBps is the bitrate of what it plays, when it knows (an
	// adaptive stream's current step, say).
	NeededBps int64 `json:"neededBps"`
	// Code and Message are the player's own error, for the log.
	Code    string `json:"code"`
	Message string `json:"message"`
}

// How long the server looks for causes before answering, and how long a
// cause is shown for a stream.
const (
	readProbeLimit = 3 * time.Second
	causeShownFor  = 5 * time.Minute
)

func (s *Server) causes() *diagnose.Log {
	s.causesOnce.Do(func() { s.causeLog = &diagnose.Log{} })
	return s.causeLog
}

// reportPlayback takes a player's report of a stall and answers why it
// happened.
func (s *Server) reportPlayback(w http.ResponseWriter, r *http.Request) {
	it, ok := s.loadItem(w, r)
	if !ok {
		return
	}
	var in playbackReport
	if err := readJSON(r, &in); err != nil || (in.Kind != "stall" && in.Kind != "error") {
		writeError(w, http.StatusBadRequest, "kind must be stall or error")
		return
	}
	u := currentUser(r)
	f := diagnose.Facts{DownloadBps: max(in.DownloadBps, 0), NeededBps: max(in.NeededBps, 0), Remote: !s.fromHome(r)}
	if q, err := s.qualitySettings(r.Context()); err == nil {
		f.Adaptive = q.Adaptive
	}
	if f.NeededBps == 0 {
		f.NeededBps = s.itemBitrate(r, it)
	}
	if s.Transcoder != nil {
		f.NoSpace = s.Transcoder.OutOfSpace(u.ID, it.ID)
		if t, ok := s.Transcoder.SessionFor(u.ID, it.ID); ok && t.Kind == "video" {
			f.Converting, f.Why, f.Speed, f.GPU = true, t.Why, t.Speed, t.Accel != ""
			// What's sent is the conversion, not the file.
			if in.NeededBps == 0 {
				f.NeededBps = convertedBitrate(t.Quality)
			}
		}
	}
	if !store.IsRemote(it.Path) {
		offset := int64(0)
		if it.DurationMs != nil && *it.DurationMs > 0 {
			offset = it.Size * min(max(in.PositionMs, 0), *it.DurationMs) / *it.DurationMs
		}
		if ms, bps, ok := stream.ReadProbe(it.Path, offset, readProbeLimit); ok {
			f.ReadMs, f.ReadBps = ms, bps
		}
	}
	c := s.causes().Add(u.ID, it.ID, diagnose.Explain(f))
	s.Log.Info("playback "+in.Kind, "item", it.ID, "user", u.Username, "cause", c.Code, "converted", c.Converted,
		"downloadBps", f.DownloadBps, "neededBps", f.NeededBps, "bufferMs", in.BufferMs, "speed", f.Speed,
		"readMs", f.ReadMs, "code", in.Code, "message", in.Message)
	writeJSON(w, http.StatusOK, map[string]any{"cause": forViewer(c, u)})
}

// playbackWhy is the latest cause for this person's playback of the item.
func (s *Server) playbackWhy(w http.ResponseWriter, r *http.Request) {
	it, ok := s.loadItem(w, r)
	if !ok {
		return
	}
	u := currentUser(r)
	var out *diagnose.Cause
	if c, ok := s.causes().Latest(u.ID, it.ID, causeShownFor); ok {
		c = forViewer(c, u)
		out = &c
	}
	writeJSON(w, http.StatusOK, map[string]any{"cause": out})
}

// forViewer leaves what an admin could change out for everyone else.
func forViewer(c diagnose.Cause, u store.User) diagnose.Cause {
	if !u.IsAdmin {
		c.Tip = ""
	}
	return c
}

// itemBitrate is how many bits a second the file takes, from its analysis
// or its size.
func (s *Server) itemBitrate(r *http.Request, it store.Item) int64 {
	if mi, _ := s.mediaInfo(r, it); mi != nil && mi.Bitrate > 0 {
		return mi.Bitrate
	}
	if it.DurationMs != nil && *it.DurationMs > 0 {
		return it.Size * 8 * 1000 / *it.DurationMs
	}
	return 0
}

// convertedBitrate is the most a conversion at these quality steps sends:
// the one step's rate, or for an adaptive stream its lowest, which a
// player falls back to. The original quality is capped at 10 Mbps.
func convertedBitrate(quality []string) int64 {
	var out int64
	for _, id := range quality {
		if st, ok := stream.StepByID(id); ok {
			if bps := int64(st.VideoBPS + st.AudioBPS); out == 0 || bps < out {
				out = bps
			}
		}
	}
	if out == 0 {
		out = 10_000_000
	}
	return out
}

// speedTestMax is the most a connection test downloads.
const speedTestMax = 100 << 20

var (
	noiseOnce sync.Once
	noise     []byte
)

// speedTest sends bytes of noise (that nothing on the way can compress),
// so a player can time how fast this device gets data from the server.
// bytes=0 answers at once, for timing the round trip.
func (s *Server) speedTest(w http.ResponseWriter, r *http.Request) {
	n := int64(10 << 20)
	if v := r.URL.Query().Get("bytes"); v != "" {
		b, err := strconv.ParseInt(v, 10, 64)
		if err != nil || b < 0 || b > speedTestMax {
			writeError(w, http.StatusBadRequest, "bytes must be between 0 and 104857600")
			return
		}
		n = b
	}
	noiseOnce.Do(func() {
		noise = make([]byte, 1<<20)
		rand.Read(noise)
	})
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Length", strconv.FormatInt(n, 10))
	for n > 0 {
		chunk := noise[:min(n, int64(len(noise)))]
		if _, err := w.Write(chunk); err != nil {
			return
		}
		n -= int64(len(chunk))
	}
}
