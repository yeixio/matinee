// Package stream serves media files directly or through on-the-fly HLS transcodes.
package stream

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/yeixio/matinee/internal/store"
)

const (
	segmentSeconds = 4
	idleTimeout    = 2 * time.Minute
	startupWait    = 20 * time.Second
)

var hlsFileRe = regexp.MustCompile(`^(index\.m3u8|seg\d{5}\.ts)$`)

// Transcoder runs one FFmpeg process per playback session and serves its HLS output.
type Transcoder struct {
	FFmpeg string
	Dir    string
	Log    *slog.Logger

	mu       sync.Mutex
	sessions map[string]*session
	stop     chan struct{}
}

type session struct {
	id       string
	userID   int64
	itemID   int64
	dir      string
	cmd      *exec.Cmd
	done     chan struct{}
	lastSeen time.Time
}

func NewTranscoder(ffmpeg, dir string, log *slog.Logger) *Transcoder {
	// Leftovers from a previous run are never valid.
	os.RemoveAll(dir)
	os.MkdirAll(dir, 0o755)
	t := &Transcoder{FFmpeg: ffmpeg, Dir: dir, Log: log, sessions: map[string]*session{}, stop: make(chan struct{})}
	go t.reap()
	return t
}

// Start begins a transcode of item from startSec and returns the session id
// once the first segment is ready. A user watching an item has at most one
// session for it, so seeking far ahead replaces the old one.
func (t *Transcoder) Start(ctx context.Context, userID int64, it store.Item, startSec float64) (string, error) {
	t.mu.Lock()
	for id, s := range t.sessions {
		if s.userID == userID && s.itemID == it.ID {
			t.killLocked(id)
		}
	}
	t.mu.Unlock()

	b := make([]byte, 12)
	rand.Read(b)
	id := hex.EncodeToString(b)
	dir := filepath.Join(t.Dir, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}

	args := []string{"-hide_banner", "-loglevel", "error", "-nostdin"}
	if startSec > 0 {
		args = append(args, "-ss", strconv.FormatFloat(startSec, 'f', 3, 64))
	}
	args = append(args, "-i", it.Path)
	if it.VideoCodec != "" {
		args = append(args, "-map", "0:v:0", "-map", "0:a:0?", "-sn")
		if it.VideoCodec == "h264" {
			// Already browser-friendly video: repackage instead of re-encoding. Far cheaper.
			args = append(args, "-c:v", "copy")
		} else {
			args = append(args, "-c:v", "libx264", "-preset", "veryfast", "-crf", "22",
				"-maxrate", "10M", "-bufsize", "20M", "-pix_fmt", "yuv420p",
				"-vf", "scale='min(1920,iw)':-2",
				"-force_key_frames", fmt.Sprintf("expr:gte(t,n_forced*%d)", segmentSeconds))
		}
		args = append(args, "-c:a", "aac", "-ac", "2", "-b:a", "192k")
	} else {
		args = append(args, "-vn", "-map", "0:a:0", "-c:a", "aac", "-b:a", "256k")
	}
	args = append(args,
		"-f", "hls", "-hls_time", strconv.Itoa(segmentSeconds), "-hls_list_size", "0",
		"-hls_playlist_type", "event", "-hls_segment_filename", filepath.Join(dir, "seg%05d.ts"),
		filepath.Join(dir, "index.m3u8"))

	cmd := exec.Command(t.FFmpeg, args...)
	cmd.Stderr = &logWriter{log: t.Log, session: id}
	if err := cmd.Start(); err != nil {
		os.RemoveAll(dir)
		return "", fmt.Errorf("start ffmpeg: %w", err)
	}
	s := &session{id: id, userID: userID, itemID: it.ID, dir: dir, cmd: cmd, done: make(chan struct{}), lastSeen: time.Now()}
	go func() {
		cmd.Wait()
		close(s.done)
	}()
	t.mu.Lock()
	t.sessions[id] = s
	t.mu.Unlock()
	t.Log.Info("transcode started", "session", id, "item", it.ID, "start", startSec, "videoCopy", it.VideoCodec == "h264")

	// Wait for the first segment so the player can start right away.
	if err := waitFor(ctx, s, filepath.Join(dir, "seg00000.ts"), startupWait); err != nil {
		t.Kill(id)
		return "", err
	}
	return id, nil
}

// ServeFile serves a playlist or segment, waiting briefly for segments FFmpeg
// hasn't written yet.
func (t *Transcoder) ServeFile(w http.ResponseWriter, r *http.Request, userID int64, sessionID, name string) {
	if !hlsFileRe.MatchString(name) {
		http.NotFound(w, r)
		return
	}
	t.mu.Lock()
	s, ok := t.sessions[sessionID]
	if ok && s.userID == userID {
		s.lastSeen = time.Now()
	}
	t.mu.Unlock()
	if !ok || s.userID != userID {
		http.NotFound(w, r)
		return
	}
	path := filepath.Join(s.dir, name)
	if name != "index.m3u8" {
		if err := waitFor(r.Context(), s, path, 15*time.Second); err != nil {
			http.Error(w, "segment not ready", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "video/mp2t")
		w.Header().Set("Cache-Control", "private, max-age=3600")
	} else {
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		w.Header().Set("Cache-Control", "no-cache")
	}
	http.ServeFile(w, r, path)
}

// waitFor returns once file exists and FFmpeg has moved past it (or exited).
func waitFor(ctx context.Context, s *session, file string, limit time.Duration) error {
	deadline := time.Now().Add(limit)
	next := nextSegment(file)
	for {
		if _, err := os.Stat(file); err == nil {
			// A segment is complete once the next one exists or FFmpeg is finished.
			if next == "" || fileExists(next) || isDone(s) {
				return nil
			}
		} else if isDone(s) {
			return errors.New("transcode ended before the segment was written")
		}
		if time.Now().After(deadline) {
			return errors.New("timed out waiting for transcode")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func nextSegment(file string) string {
	base := filepath.Base(file)
	if len(base) != len("seg00000.ts") {
		return ""
	}
	n, err := strconv.Atoi(base[3:8])
	if err != nil {
		return ""
	}
	return filepath.Join(filepath.Dir(file), fmt.Sprintf("seg%05d.ts", n+1))
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

func isDone(s *session) bool {
	select {
	case <-s.done:
		return true
	default:
		return false
	}
}

// Kill stops a session and deletes its files.
func (t *Transcoder) Kill(id string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.killLocked(id)
}

func (t *Transcoder) killLocked(id string) {
	s, ok := t.sessions[id]
	if !ok {
		return
	}
	delete(t.sessions, id)
	if s.cmd.Process != nil {
		s.cmd.Process.Kill()
	}
	go func() {
		<-s.done
		os.RemoveAll(s.dir)
	}()
}

func (t *Transcoder) reap() {
	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-t.stop:
			return
		case <-tick.C:
			t.mu.Lock()
			for id, s := range t.sessions {
				if time.Since(s.lastSeen) > idleTimeout {
					t.Log.Info("transcode idle, stopping", "session", id)
					t.killLocked(id)
				}
			}
			t.mu.Unlock()
		}
	}
}

// Close stops every session.
func (t *Transcoder) Close() {
	close(t.stop)
	t.mu.Lock()
	for id := range t.sessions {
		t.killLocked(id)
	}
	t.mu.Unlock()
}

type logWriter struct {
	log     *slog.Logger
	session string
}

func (l *logWriter) Write(p []byte) (int, error) {
	l.log.Warn("ffmpeg", "session", l.session, "msg", string(p))
	return len(p), nil
}
