package psremote

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"time"

	"github.com/pion/rtp"
	"github.com/pion/rtp/codecs"
)

// Packet kinds, as the game stream's (gamehost.KindVideo and KindAudio).
const (
	KindVideo = 0x10
	KindAudio = 0x11
)

// Hello is the picture the console sends.
type Hello struct {
	Width  int     `json:"width"`
	Height int     `json:"height"`
	FPS    float64 `json:"fps"`
}

// Stream is a console being played: its picture and sound as RTP packets,
// and the player's controller back to it.
type Stream struct {
	cmd   *exec.Cmd
	in    io.WriteCloser
	inMu  sync.Mutex
	out   *bufio.Reader
	log   *slog.Logger
	Hello Hello

	video, audio rtp.Packetizer
	start        time.Time
	audioTS      uint32
	keyframes    keyframer
	pending      []*rtp.Packet
	pendingKind  byte
	pad          pad
	closeOnce    sync.Once
	exited       chan struct{}
}

// Play connects to the console and waits until its stream starts. The
// console must be on (see WakeAndWait). height is 1080, 720 or 540 (a PS4
// that isn't a Pro sends 720 at most); fps 60 or 30.
func (b *Bridge) Play(ctx context.Context, c Console, account string, height, fps int, log *slog.Logger) (*Stream, error) {
	if !b.Available() {
		return nil, errors.New("this server can't play PlayStation games")
	}
	cmd := exec.Command(b.Path, "play", c.Host, ps(c.PS5), strconv.Itoa(height), strconv.Itoa(fps))
	cmd.Env = append(os.Environ(), "MATINEE_PS_ACCOUNT="+account, "MATINEE_PS_REGIST_KEY="+c.RegistKey, "MATINEE_PS_RP_KEY="+c.RPKey)
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = logWriter{log}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	ssrc := rand.Uint32()
	s := &Stream{cmd: cmd, in: in, out: bufio.NewReaderSize(out, 1<<20), log: log, start: time.Now(), exited: make(chan struct{}),
		video: rtp.NewPacketizer(1200, 96, ssrc, &codecs.H264Payloader{DisableStapA: true}, rtp.NewRandomSequencer(), 90000),
		audio: rtp.NewPacketizer(1200, 111, ssrc+1, &codecs.OpusPayloader{}, rtp.NewRandomSequencer(), 48000)}
	go func() {
		cmd.Wait()
		close(s.exited)
	}()

	// The console answers within a few seconds, or not at all.
	type hello struct {
		h   Hello
		err error
	}
	got := make(chan hello, 1)
	go func() {
		typ, b, err := s.readMsg()
		switch {
		case err != nil:
			got <- hello{err: errors.New("the PlayStation bridge stopped")}
		case typ == 'E':
			got <- hello{err: bridgeError(b)}
		case typ != 'I':
			got <- hello{err: fmt.Errorf("the PlayStation bridge sent %q first", typ)}
		default:
			var h Hello
			if json.Unmarshal(b, &h) != nil || h.Width <= 0 || h.Height <= 0 {
				got <- hello{err: errors.New("the PlayStation bridge sent something unexpected")}
				return
			}
			got <- hello{h: h}
		}
	}()
	select {
	case h := <-got:
		if h.err != nil {
			s.Close()
			return nil, h.err
		}
		s.Hello = h.h
	case <-time.After(45 * time.Second):
		s.Close()
		return nil, errors.New("the PlayStation didn't start Remote Play: is it already being played remotely?")
	case <-ctx.Done():
		s.Close()
		return nil, ctx.Err()
	}
	return s, nil
}

func bridgeError(b []byte) error {
	var e struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(b, &e) == nil && e.Error != "" {
		return errors.New("PlayStation: " + e.Error)
	}
	return errors.New("the PlayStation stopped the stream")
}

// readMsg reads one message from the bridge.
func (s *Stream) readMsg() (byte, []byte, error) {
	var h [8]byte
	if _, err := io.ReadFull(s.out, h[:]); err != nil {
		return 0, nil, err
	}
	n := binary.LittleEndian.Uint32(h[4:])
	if n > 16<<20 {
		return 0, nil, errors.New("the PlayStation bridge sent a message too big")
	}
	b := make([]byte, n)
	if _, err := io.ReadFull(s.out, b); err != nil {
		return 0, nil, err
	}
	return h[0], b, nil
}

// Next returns the next RTP packet, with its kind (KindVideo or
// KindAudio). It returns io.EOF once the stream ends normally, or why it
// ended.
func (s *Stream) Next() (byte, *rtp.Packet, error) {
	for len(s.pending) == 0 {
		typ, b, err := s.readMsg()
		if err != nil {
			return 0, nil, io.EOF
		}
		switch typ {
		case 'V':
			au := s.keyframes.fix(b)
			ts := uint32(time.Since(s.start).Seconds() * 90000)
			ps := s.video.Packetize(au, 0)
			for _, p := range ps {
				p.Timestamp = ts
			}
			s.pending, s.pendingKind = ps, KindVideo
		case 'A':
			ps := s.audio.Packetize(b, 0)
			for _, p := range ps {
				p.Timestamp = s.audioTS
			}
			s.audioTS += uint32(opusSamples(b))
			s.pending, s.pendingKind = ps, KindAudio
		case 'E':
			return 0, nil, bridgeError(b)
		}
	}
	p := s.pending[0]
	s.pending = s.pending[1:]
	return s.pendingKind, p, nil
}

// send writes one message to the bridge.
func (s *Stream) send(typ byte, b []byte) {
	s.inMu.Lock()
	defer s.inMu.Unlock()
	h := [8]byte{typ}
	binary.LittleEndian.PutUint32(h[4:], uint32(len(b)))
	s.in.Write(append(h[:], b...))
}

// Input takes the player's input messages (see gamestream.Session.input):
// the first controller's buttons and sticks.
func (s *Stream) Input(b []byte) {
	if st, ok := s.pad.apply(b); ok {
		s.send('C', st)
	}
}

// Keyframe asks the console for a whole picture, for a player who has
// just joined.
func (s *Stream) Keyframe() { s.send('K', nil) }

// Close ends the stream and Remote Play with it.
func (s *Stream) Close() {
	s.closeOnce.Do(func() {
		s.send('Q', nil)
		s.in.Close()
		select {
		case <-s.exited:
		case <-time.After(3 * time.Second):
			s.cmd.Process.Kill()
		}
	})
}

// logWriter passes the bridge's log lines on to the server's log.
type logWriter struct{ log *slog.Logger }

func (w logWriter) Write(b []byte) (int, error) {
	if w.log != nil {
		for _, line := range bytesLines(b) {
			w.log.Debug("PlayStation bridge", "msg", line)
		}
	}
	return len(b), nil
}

func bytesLines(b []byte) []string {
	var out []string
	start := 0
	for i, c := range b {
		if c == '\n' {
			if i > start {
				out = append(out, string(b[start:i]))
			}
			start = i + 1
		}
	}
	if start < len(b) {
		out = append(out, string(b[start:]))
	}
	return out
}
