package stream

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/yeixio/matinee/internal/hwaccel"
	"github.com/yeixio/matinee/internal/store"
)

// MaxGapless is the most songs one gapless stream joins.
const MaxGapless = 100

// gaplessRate is the sample rate of a gapless stream. Songs are resampled
// to it, so ones recorded at other rates still meet seamlessly.
const gaplessRate = 44100

// StartGapless begins one continuous stream of songs, so they play without
// a gap between them, the way a DJ mix or a live album is meant to. FFmpeg
// decodes each file exactly, without the silence encoders pad MP3 and AAC
// files with, and joins the sound sample to sample, from startSec into the
// first song. A user has at most one such stream per album (group).
func (t *Transcoder) StartGapless(ctx context.Context, userID int64, group string, tracks []store.Item, startSec float64) (string, error) {
	if len(tracks) == 0 || len(tracks) > MaxGapless {
		return "", fmt.Errorf("a gapless stream joins 1 to %d songs", MaxGapless)
	}
	t.mu.Lock()
	for id, s := range t.sessions {
		if s.userID == userID && s.group == group {
			s.superseded = true
			t.killLocked(id)
		}
	}
	t.mu.Unlock()

	length := -startSec
	for _, tr := range tracks {
		if tr.DurationMs == nil || *tr.DurationMs <= 0 {
			return "", fmt.Errorf("song %d has no known length", tr.ID)
		}
		length += float64(*tr.DurationMs) / 1000
	}
	if length <= 0 {
		return "", errors.New("starts past the end")
	}
	b := make([]byte, 12)
	rand.Read(b)
	id := hex.EncodeToString(b)
	dir := filepath.Join(t.Dir, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	// Players get the whole playlist at once, as for a file, so they start
	// at the top and can go anywhere in it: one still growing reads as live
	// to some, which jump ahead to its newest part. Its pieces come in far
	// faster than they play, and the player waits for any that haven't.
	playlist, padTo := vodPlaylist(length)
	if err := os.WriteFile(filepath.Join(dir, "index.m3u8"), playlist, 0o644); err != nil {
		os.RemoveAll(dir)
		return "", err
	}
	args := append([]string{"-hide_banner", "-loglevel", "error", "-nostdin"}, gaplessArgs(tracks, startSec, padTo)...)
	args = append(args, "-f", "hls", "-hls_time", strconv.Itoa(segmentSeconds), "-hls_list_size", "0",
		"-hls_segment_filename", filepath.Join(dir, "seg%05d.ts"), filepath.Join(dir, "ffmpeg.m3u8"))

	cmd := exec.Command(t.FFmpeg, args...)
	cmd.Stderr = &logWriter{log: t.Log, session: id}
	if err := cmd.Start(); err != nil {
		os.RemoveAll(dir)
		return "", fmt.Errorf("start ffmpeg: %w", err)
	}
	s := &session{id: id, userID: userID, itemID: tracks[0].ID, group: group, dir: dir, cmd: cmd, done: make(chan struct{}),
		lastSeen: time.Now(), started: time.Now(), startSec: startSec, mode: hwaccel.CPU, audioOnly: true}
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
	t.Log.Info("gapless stream started", "session", id, "group", group, "songs", len(tracks), "first", tracks[0].ID, "start", startSec)

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
		return "", errors.Join(errors.New("gapless stream didn't start"), err)
	}
	t.mu.Lock()
	s.ready = true
	t.mu.Unlock()
	return id, nil
}

// vodPlaylist lists the pieces of a stream of length seconds before FFmpeg
// has made them, and says how long to make the stream so it fills them:
// a song's length as read from its file runs a little long (encoders pad
// it), so the stream is padded with silence to the end of the playlist,
// never short of it, which would leave a piece missing. The last piece is
// kept clear of a piece's length, so it doesn't spill into one more.
func vodPlaylist(length float64) ([]byte, float64) {
	seg := float64(segmentSeconds)
	n := int(math.Ceil(length / seg))
	padTo := length
	if padTo > float64(n)*seg-0.1 {
		n++
	}
	padTo = max(padTo, float64(n-1)*seg+0.5)
	var b strings.Builder
	fmt.Fprintf(&b, "#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:%d\n#EXT-X-MEDIA-SEQUENCE:0\n#EXT-X-PLAYLIST-TYPE:VOD\n", segmentSeconds)
	for i := range n {
		d := seg
		if i == n-1 {
			d = padTo - float64(n-1)*seg
		}
		fmt.Fprintf(&b, "#EXTINF:%.6f,\nseg%05d.ts\n", d, i)
	}
	b.WriteString("#EXT-X-ENDLIST\n")
	return []byte(b.String()), padTo
}

// gaplessArgs are FFmpeg's inputs and output for a gapless stream: each
// song an input, the first from startSec, their sound made alike and
// joined by the concat filter, padded with silence to padTo seconds, as
// AAC.
func gaplessArgs(tracks []store.Item, startSec, padTo float64) []string {
	var args []string
	var filter strings.Builder
	for i, tr := range tracks {
		args = append(args, Input(tr)...)
		fmt.Fprintf(&filter, "[%d:a:0]", i)
		// Seeking in an MP3 starts with a moment of silence while the decoder
		// catches up; decoding from the top and cutting is exact, and sound
		// decodes hundreds of times faster than it plays.
		if i == 0 && startSec > 0 {
			fmt.Fprintf(&filter, "atrim=start=%s,asetpts=PTS-STARTPTS,", strconv.FormatFloat(startSec, 'f', 3, 64))
		}
		fmt.Fprintf(&filter, "aformat=sample_fmts=fltp:sample_rates=%d:channel_layouts=stereo[a%d];", gaplessRate, i)
	}
	for i := range tracks {
		fmt.Fprintf(&filter, "[a%d]", i)
	}
	fmt.Fprintf(&filter, "concat=n=%d:v=0:a=1,apad=whole_dur=%s[out]", len(tracks), strconv.FormatFloat(padTo, 'f', 3, 64))
	return append(args, "-filter_complex", filter.String(), "-map", "[out]", "-vn", "-sn", "-dn", "-c:a", "aac", "-b:a", "256k")
}

// Offsets says where each song starts in a gapless stream that begins at
// startSec into the first one, in seconds on the stream's clock (the first
// is 0, or less when the stream starts partway into it), from the songs'
// lengths.
func Offsets(tracks []store.Item, startSec float64) []float64 {
	out := make([]float64, len(tracks))
	at := -startSec
	for i, tr := range tracks {
		out[i] = at
		if tr.DurationMs != nil {
			at += float64(*tr.DurationMs) / 1000
		}
	}
	return out
}
