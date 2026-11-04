// Package stream serves media files directly or through on-the-fly HLS transcodes.
package stream

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yeixio/matinee/internal/hwaccel"
	"github.com/yeixio/matinee/internal/store"
)

const (
	segmentSeconds = 4
	maxWidth       = 1920
	idleTimeout    = 2 * time.Minute
	startupWait    = 20 * time.Second
	hwStartupWait  = 10 * time.Second
)

// ErrSuperseded means a newer Start for the same viewer and item replaced this
// one before it was ready, as happens while dragging the seek bar.
var ErrSuperseded = errors.New("replaced by a newer transcode")

var hlsFileRe = regexp.MustCompile(`^(index\.m3u8|seg\d{5}\.(ts|m4s)|init\.mp4|master\.m3u8|index_\d\.m3u8|seg_\d_\d{5}\.ts)$`)

// Transcoder runs one FFmpeg process per playback session and serves its HLS output.
type Transcoder struct {
	FFmpeg string
	Dir    string
	Log    *slog.Logger
	// HW picks the hardware accelerator, if any; nil transcodes on the CPU.
	HW Accelerator

	mu       sync.Mutex
	sessions map[string]*session
	// failed remembers items whose transcodes failed on the GPU, so their
	// next ones go straight to a mode that works.
	failed map[int64]failure
	stop   chan struct{}

	// For admin alerts and metrics: transcodes started since the server
	// started, and those that failed to start or broke off partway.
	started, failures atomic.Int64
}

// Counts is how many transcodes have started since the server started, and
// how many of them failed: those that never got going (after any retries
// on the CPU) and those whose FFmpeg broke off partway through.
func (t *Transcoder) Counts() (started, failed int64) {
	return t.started.Load(), t.failures.Load()
}

// Accelerator says which hardware accelerator new transcodes use (see
// hwaccel.Manager); nil means the CPU.
type Accelerator interface {
	Active() *hwaccel.Accel
}

type failure struct {
	kind hwaccel.Kind
	mode hwaccel.Mode
}

type session struct {
	id     string
	userID int64
	itemID int64
	// group is set for gapless streams (see StartGapless): the album.
	group    string
	dir      string
	cmd      *exec.Cmd
	done     chan struct{}
	lastSeen time.Time
	// superseded is set (under Transcoder.mu) when a newer session replaces this one.
	superseded bool
	// killed is set (under Transcoder.mu) when Matinee stops FFmpeg itself.
	killed bool
	// ready is set (under Transcoder.mu) once the player has its first segment.
	ready bool
	// For the admin dashboard (see Sessions).
	started   time.Time
	startSec  float64
	accel     *hwaccel.Accel // nil on the CPU
	mode      hwaccel.Mode
	quality   []string
	copyVideo bool
	// why says why the picture is converted rather than copied (see videoPlan).
	why string
	// toneMapped: an HDR picture is converted to SDR (see ToneMap).
	toneMapped bool
	audioOnly  bool
	burnSub    bool
	// live is a live TV channel (see StartLive): players start near its
	// newest piece, and old pieces are gone.
	live bool
}

func NewTranscoder(ffmpeg, dir string, hw Accelerator, log *slog.Logger) *Transcoder {
	// Leftovers from a previous run are never valid.
	os.RemoveAll(dir)
	os.MkdirAll(dir, 0o755)
	t := &Transcoder{FFmpeg: ffmpeg, Dir: dir, Log: log, HW: hw, sessions: map[string]*session{}, failed: map[int64]failure{}, stop: make(chan struct{})}
	go t.reap()
	return t
}

// Start begins a transcode of item from startSec and returns the session id
// once the first segment is ready. A user watching an item has at most one
// session for it, so seeking far ahead replaces the old one. burnSub is an
// embedded subtitle stream to draw into the video, or -1 for none. audio is
// which of the file's audio streams to play (FFmpeg's 0:a:N), 0 for the first.
//
// steps picks the picture quality. None keeps the original as far as it
// can (video that's already H.264 is copied, anything else re-encoded at
// up to 1080p). One makes that quality. Several make an adaptive stream
// of the ones that suit the file (see Ladder), with a master playlist;
// Playlist says which playlist the session starts from.
//
// With a hardware accelerator, a transcode that fails to start on the GPU
// starts again with less of it on the GPU, and finally on the CPU alone.
func (t *Transcoder) Start(ctx context.Context, userID int64, it store.Item, startSec float64, burnSub, audio int, steps ...Step) (string, error) {
	return t.StartFor(ctx, userID, it, startSec, Request{BurnSub: burnSub, Audio: audio}, steps...)
}

// Request is what a player asks of a transcode besides where to start and
// the quality.
type Request struct {
	// BurnSub is an embedded subtitle stream to draw into the video, or -1
	// for none, and Audio which audio stream to play (see Start).
	BurnSub, Audio int
	// Plays is what the player takes as it is: video it decodes is copied
	// rather than converted (see videoPlan). Picture is what's known about
	// the file's HDR.
	Plays   Plays
	Picture Picture
	// ToneMap is how an HDR picture that's converted is made SDR: one of
	// ToneMapAlgorithms, or ToneMapOff.
	ToneMap string
	// AudioDelayMs moves the sound against the picture, for lip sync: later
	// when positive, earlier when negative (see AudioDelayFilter).
	AudioDelayMs int
	// BurnFile, when set, is a picture subtitle file next to the video
	// (VobSub .idx, Blu-ray .sup) to draw in, instead of an embedded stream.
	BurnFile string
	// Styled draws ASS/SSA subtitles with their styling into the video,
	// for players that can't draw them: the track's file (the video itself
	// for an embedded one) and its subtitle stream there.
	Styled *StyledSub
}

// StyledSub is an ASS/SSA track to draw with libass.
type StyledSub struct {
	Path   string
	Stream int
}

// MaxAudioDelayMs is how far the sound can be moved either way.
const MaxAudioDelayMs = 2000

// AudioDelayFilter is the audio filter that moves the sound by ms against
// the picture, or "" for none. Later pads the start with silence; earlier
// drops that much of the start, so the rest arrives sooner.
func AudioDelayFilter(ms int) string {
	ms = max(-MaxAudioDelayMs, min(MaxAudioDelayMs, ms))
	switch {
	case ms > 0:
		return "adelay=delays=" + strconv.Itoa(ms) + ":all=1"
	case ms < 0:
		return "atrim=start=" + strconv.FormatFloat(float64(-ms)/1000, 'f', 3, 64) + ",asetpts=PTS-STARTPTS"
	}
	return ""
}

// StartFor is Start for a player that says what it plays: a picture it
// decodes (HEVC or AV1 at 4K in HDR, say) is repackaged as it is, and
// only what it can't take, usually the sound, is converted.
func (t *Transcoder) StartFor(ctx context.Context, userID int64, it store.Item, startSec float64, req Request, steps ...Step) (string, error) {
	burnSub, audio := req.BurnSub, req.Audio
	t.mu.Lock()
	for id, s := range t.sessions {
		if s.userID == userID && s.itemID == it.ID && s.group == "" {
			s.superseded = true
			t.killLocked(id)
		}
	}
	t.mu.Unlock()

	var accel *hwaccel.Accel
	if t.HW != nil {
		accel = t.HW.Active()
	}
	video := hwaccel.Video{Codec: it.VideoCodec, BurnSub: burnSub, MaxWidth: maxWidth, KeyframeSeconds: segmentSeconds}
	if req.BurnFile != "" && it.VideoCodec != "" {
		video.BurnSub, video.BurnInput, burnSub = 0, 1, 0
	}
	drawn := burnSub
	if req.Styled != nil && it.VideoCodec != "" {
		video.BurnSub, drawn = -1, max(burnSub, 0)
		video.Styled = StyledFilter(req.Styled.Path, req.Styled.Stream, startSec)
	}
	copyVideo, why := videoPlan(it, req.Picture, drawn, steps, req.Plays)
	if !copyVideo && it.VideoCodec != "" && req.Picture.HDR != "" {
		video.ToneMap, video.ToneMapInput = ToneMap(req.Picture, req.ToneMap, t.ToneTools())
	}
	modes := t.plan(accel, it.ID, video, copyVideo)
	for i, mode := range modes {
		id, err := t.start(ctx, userID, it, startSec, accel, mode, video, audio, req.AudioDelayMs, req.BurnFile, copyVideo, why, steps)
		if err == nil || i == len(modes)-1 || errors.Is(err, ErrSuperseded) || ctx.Err() != nil {
			switch {
			case err == nil:
				t.started.Add(1)
			case !errors.Is(err, ErrSuperseded) && ctx.Err() == nil:
				t.started.Add(1)
				t.failures.Add(1)
			}
			return id, err
		}
		t.Log.Warn("transcode failed on the GPU; trying again with less on it",
			"item", it.ID, "accel", accel.Kind, "mode", mode, "next", modes[i+1], "err", err)
		t.hwFailed(it.ID, accel.Kind, mode)
	}
	panic("unreachable")
}

// plan lists the modes to try, leaving out the ones that already failed for
// this item on this accelerator (so seeking doesn't retry them each time).
func (t *Transcoder) plan(accel *hwaccel.Accel, itemID int64, video hwaccel.Video, copyVideo bool) []hwaccel.Mode {
	if accel == nil || video.Codec == "" || copyVideo {
		// No GPU, or audio or video that's copied: nothing to put on one.
		return []hwaccel.Mode{hwaccel.CPU}
	}
	modes := hwaccel.Plan(accel, video)
	t.mu.Lock()
	f, ok := t.failed[itemID]
	t.mu.Unlock()
	if ok && f.kind == accel.Kind {
		modes = slices.DeleteFunc(modes, func(m hwaccel.Mode) bool { return m >= f.mode })
	}
	return modes
}

// hwFailed remembers that item failed in mode on the accelerator kind.
func (t *Transcoder) hwFailed(itemID int64, kind hwaccel.Kind, mode hwaccel.Mode) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if f, ok := t.failed[itemID]; !ok || f.kind != kind || mode < f.mode {
		t.failed[itemID] = failure{kind, mode}
	}
}

func (t *Transcoder) start(ctx context.Context, userID int64, it store.Item, startSec float64, accel *hwaccel.Accel, mode hwaccel.Mode, video hwaccel.Video, audio, audioDelayMs int, burnFile string, copyVideo bool, why string, steps []Step) (string, error) {
	b := make([]byte, 12)
	rand.Read(b)
	id := hex.EncodeToString(b)
	dir := filepath.Join(t.Dir, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}

	args := []string{"-hide_banner", "-loglevel", "error", "-nostdin"}
	if audio < 0 {
		audio = 0
	}
	audioMap := "0:a:" + strconv.Itoa(audio)
	// The audio's bitrate follows the picture's: lighter steps get lighter sound.
	audioRate := func(i int) string {
		if i < len(steps) {
			return strconv.Itoa(steps[i].AudioBPS)
		}
		return "192k"
	}
	adaptive := len(steps) > 1 && it.VideoCodec != ""
	if adaptive {
		steps = Ladder(it, steps, mode == hwaccel.CPU)
		video.Renditions = renditions(steps)
	} else if len(steps) == 1 {
		video.MaxWidth = steps[0].Width
		video.Renditions = renditions(steps)
	}
	var output []string
	switch {
	case it.VideoCodec == "":
		output = []string{"-vn", "-map", audioMap, "-c:a", "aac", "-b:a", "256k"}
	case copyVideo:
		// Video the player decodes: repackage instead of re-encoding. Far
		// cheaper, and 4K and HDR stay as they are.
		output = []string{"-map", "0:v:0", "-c:v", "copy"}
		if it.VideoCodec == "hevc" {
			// Apple's players only take HEVC tagged hvc1 (parameter sets in
			// the sample description, not hev1's in the stream).
			output = append(output, "-tag:v", "hvc1")
		}
	default:
		var input []string
		input, output = hwaccel.Args(accel, mode, video)
		args = append(args, input...)
	}
	// An adaptive stream has an audio stream per step (see -var_stream_map),
	// so it only maps audio the file is known to have.
	withAudio := !adaptive || it.AudioCodec != ""
	switch {
	case adaptive && withAudio:
		for i := range steps {
			output = append(output, "-map", audioMap, "-b:a:"+strconv.Itoa(i), audioRate(i))
		}
		output = append(output, "-sn", "-c:a", "aac", "-ac", "2")
	case it.VideoCodec != "":
		output = append(output, "-map", audioMap+"?", "-sn", "-c:a", "aac", "-ac", "2", "-b:a", audioRate(0))
	}
	if f := AudioDelayFilter(audioDelayMs); f != "" && withAudio && it.VideoCodec != "" {
		output = append(output, "-af", f)
	}
	if startSec > 0 {
		args = append(args, "-ss", strconv.FormatFloat(startSec, 'f', 3, 64))
	}
	args = append(args, Input(it)...)
	if burnFile != "" && video.BurnInput == 1 {
		// The subtitle file is read from its start and moved to the video's
		// moment, so a line already showing then still shows.
		if off := subtitleFileStart(burnFile) - startSec; off != 0 {
			args = append(args, "-itsoffset", strconv.FormatFloat(off, 'f', 3, 64))
		}
		args = append(args, "-i", burnFile)
	}
	args = append(args, output...)
	args = append(args, "-f", "hls", "-hls_time", strconv.Itoa(segmentSeconds), "-hls_list_size", "0", "-hls_playlist_type", "event")
	// HEVC and AV1 go in fMP4 pieces, which is how HLS carries them.
	ext := ".ts"
	if fragmented(it.VideoCodec, copyVideo) {
		ext = ".m4s"
		args = append(args, "-hls_segment_type", "fmp4", "-hls_fmp4_init_filename", "init.mp4")
	}
	firstSegments := []string{filepath.Join(dir, "seg00000"+ext)}
	if adaptive {
		// One playlist and set of segments per step. The master playlist
		// naming them is written here rather than by FFmpeg, which can
		// only guess each one's bitrate once it has encoded some.
		var streams []string
		firstSegments = nil
		for i := range steps {
			s := "v:" + strconv.Itoa(i)
			if withAudio {
				s += ",a:" + strconv.Itoa(i)
			}
			streams = append(streams, s)
			firstSegments = append(firstSegments, filepath.Join(dir, fmt.Sprintf("seg_%d_00000.ts", i)))
		}
		if err := os.WriteFile(filepath.Join(dir, "master.m3u8"), masterPlaylist(it, steps, withAudio), 0o644); err != nil {
			os.RemoveAll(dir)
			return "", err
		}
		args = append(args, "-var_stream_map", strings.Join(streams, " "),
			"-hls_segment_filename", filepath.Join(dir, "seg_%v_%05d.ts"), filepath.Join(dir, "index_%v.m3u8"))
	} else {
		args = append(args, "-hls_segment_filename", filepath.Join(dir, "seg%05d"+ext), filepath.Join(dir, "index.m3u8"))
	}

	cmd := exec.Command(t.FFmpeg, args...)
	cmd.Stderr = &logWriter{log: t.Log, session: id}
	if err := cmd.Start(); err != nil {
		os.RemoveAll(dir)
		return "", fmt.Errorf("start ffmpeg: %w", err)
	}
	s := &session{id: id, userID: userID, itemID: it.ID, dir: dir, cmd: cmd, done: make(chan struct{}), lastSeen: time.Now(),
		started: time.Now(), startSec: startSec, mode: mode, copyVideo: copyVideo, why: why, toneMapped: video.ToneMap != "", audioOnly: it.VideoCodec == "", burnSub: video.BurnSub >= 0 || video.Styled != ""}
	var kind hwaccel.Kind
	if mode != hwaccel.CPU {
		kind = accel.Kind
		s.accel = accel
	}
	go func() {
		err := cmd.Wait()
		t.mu.Lock()
		killed, ready := s.killed, s.ready
		t.mu.Unlock()
		if err != nil && !killed && ready {
			t.failures.Add(1)
		}
		// A GPU that fails partway through (rather than at the start) leaves
		// the player stuck; when it starts again, this item skips that mode.
		if err != nil && !killed && kind != "" {
			t.Log.Warn("transcode failed on the GPU partway through", "session", id, "item", it.ID, "accel", kind, "mode", mode, "err", err)
			t.hwFailed(it.ID, kind, mode)
		}
		close(s.done)
	}()
	t.mu.Lock()
	t.sessions[id] = s
	t.mu.Unlock()
	var quality []string
	for _, s := range steps {
		quality = append(quality, s.ID)
	}
	t.mu.Lock()
	s.quality = quality
	t.mu.Unlock()
	t.Log.Info("transcode started", "session", id, "item", it.ID, "start", startSec,
		"videoCopy", copyVideo, "convertWhy", why, "burnSub", video.BurnSub, "audio", audio, "accel", kind, "mode", mode, "quality", quality)

	// Wait for the first segment so the player can start right away. A GPU
	// that hasn't made one in a while is stuck, and the CPU gets a turn.
	wait := startupWait
	if kind != "" {
		wait = hwStartupWait
	}
	for _, seg := range firstSegments {
		if err := waitFor(ctx, s, seg, wait); err != nil {
			t.mu.Lock()
			superseded := s.superseded
			t.killLocked(id)
			t.mu.Unlock()
			if superseded {
				return "", ErrSuperseded
			}
			return "", err
		}
	}
	t.mu.Lock()
	s.ready = true
	t.mu.Unlock()
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
	allowAnyOrigin(w)
	path := filepath.Join(s.dir, name)
	if !strings.HasSuffix(name, ".m3u8") {
		if err := waitFor(r.Context(), s, path, 15*time.Second); err != nil {
			http.Error(w, "segment not ready", http.StatusServiceUnavailable)
			return
		}
		switch filepath.Ext(name) {
		case ".m4s":
			w.Header().Set("Content-Type", "video/iso.segment")
		case ".mp4":
			w.Header().Set("Content-Type", "video/mp4")
		default:
			w.Header().Set("Content-Type", "video/mp2t")
		}
		w.Header().Set("Cache-Control", "private, max-age=3600")
	} else {
		b, err := os.ReadFile(path)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		if !s.live {
			b = startAtBeginning(b)
		}
		// A player given the playlist with ?token= (an app's native player, or
		// a cast device) fetches segments without its headers or cookies, so
		// the segment links carry the token too.
		if token := r.URL.Query().Get("token"); token != "" {
			b = signPlaylist(b, token)
		}
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		w.Header().Set("Cache-Control", "no-cache")
		w.Write(b)
		return
	}
	http.ServeFile(w, r, path)
}

// startAtBeginning tells players to start a playlist that is still growing
// from its first segment, rather than from its newest as for live TV.
func startAtBeginning(playlist []byte) []byte {
	const head = "#EXTM3U\n"
	if !bytes.HasPrefix(playlist, []byte(head)) || bytes.Contains(playlist, []byte("#EXT-X-START:")) {
		return playlist
	}
	return append([]byte(head+"#EXT-X-START:TIME-OFFSET=0,PRECISE=YES\n"), playlist[len(head):]...)
}

// signPlaylist adds ?token= to each segment in an HLS playlist, and to the
// fMP4 init piece its #EXT-X-MAP names.
func signPlaylist(playlist []byte, token string) []byte {
	lines := strings.Split(string(playlist), "\n")
	q := "?token=" + url.QueryEscape(token)
	for i, l := range lines {
		switch {
		case l != "" && !strings.HasPrefix(l, "#"):
			lines[i] = l + q
		case strings.HasPrefix(l, "#EXT-X-MAP:"):
			lines[i] = mapURIRe.ReplaceAllString(l, `URI="${1}`+strings.ReplaceAll(q, "$", "$$")+`"`)
		}
	}
	return []byte(strings.Join(lines, "\n"))
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

var (
	segmentRe = regexp.MustCompile(`^(seg(?:_\d_)?)(\d{5})(\.ts|\.m4s)$`)
	mapURIRe  = regexp.MustCompile(`URI="([^"?]*)[^"]*"`)
)

func nextSegment(file string) string {
	m := segmentRe.FindStringSubmatch(filepath.Base(file))
	if m == nil {
		return ""
	}
	n, _ := strconv.Atoi(m[2])
	return filepath.Join(filepath.Dir(file), fmt.Sprintf("%s%05d%s", m[1], n+1, m[3]))
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
	s.killed = true
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
