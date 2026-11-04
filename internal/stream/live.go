package stream

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"github.com/yeixio/matinee/internal/hwaccel"
)

// LiveInput is a live TV channel that doesn't stream as HLS, such as an
// MPEG-TS stream from a tuner.
type LiveInput struct {
	Channel   string // the channel's id, so a viewer has one stream per channel
	URL       string
	UserAgent string
	Referrer  string
}

// StartLive repackages a live channel's stream as HLS, so every player can
// play it: the picture is copied as it is and the sound made AAC. FFmpeg
// keeps the last minute or so, and stops once nobody asks for it.
func (t *Transcoder) StartLive(ctx context.Context, userID int64, in LiveInput) (string, error) {
	group := "live:" + in.Channel
	t.mu.Lock()
	for id, s := range t.sessions {
		if s.userID == userID && s.group == group {
			s.superseded = true
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
	args := []string{"-hide_banner", "-loglevel", "error", "-nostdin", "-user_agent", in.UserAgent}
	if in.Referrer != "" {
		args = append(args, "-referer", in.Referrer)
	}
	args = append(args, "-reconnect", "1", "-reconnect_streamed", "1", "-reconnect_on_network_error", "1", "-reconnect_delay_max", "10",
		"-i", in.URL, "-map", "0:v:0?", "-map", "0:a:0?", "-c:v", "copy", "-c:a", "aac", "-ac", "2", "-b:a", "192k", "-sn",
		"-f", "hls", "-hls_time", strconv.Itoa(segmentSeconds), "-hls_list_size", "15", "-hls_flags", "delete_segments+omit_endlist",
		"-hls_segment_filename", filepath.Join(dir, "seg%05d.ts"), filepath.Join(dir, "index.m3u8"))
	cmd := exec.Command(t.FFmpeg, args...)
	cmd.Stderr = &logWriter{log: t.Log, session: id}
	if err := cmd.Start(); err != nil {
		os.RemoveAll(dir)
		return "", fmt.Errorf("start ffmpeg: %w", err)
	}
	s := &session{id: id, userID: userID, group: group, dir: dir, cmd: cmd, done: make(chan struct{}),
		lastSeen: time.Now(), started: time.Now(), mode: hwaccel.CPU, copyVideo: true, live: true}
	go func() {
		err := cmd.Wait()
		t.mu.Lock()
		killed, ready := s.killed, s.ready
		t.mu.Unlock()
		if err != nil && !killed && ready {
			t.failures.Add(1)
		}
		close(s.done)
	}()
	t.mu.Lock()
	t.sessions[id] = s
	t.mu.Unlock()
	t.started.Add(1)
	t.Log.Info("live channel stream started", "session", id, "channel", in.Channel)

	// The playlist appears with the first piece.
	if err := waitFor(ctx, s, filepath.Join(dir, "seg00000.ts"), startupWait); err != nil {
		t.mu.Lock()
		superseded := s.superseded
		t.killLocked(id)
		t.mu.Unlock()
		if superseded {
			return "", ErrSuperseded
		}
		if ctx.Err() == nil {
			t.failures.Add(1)
		}
		return "", errors.Join(errors.New("the channel's stream didn't start"), err)
	}
	t.mu.Lock()
	s.ready = true
	t.mu.Unlock()
	return id, nil
}
