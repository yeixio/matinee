// Package mediabridge runs a bridge program that receives a game's picture
// and sound from a device on the home network (a PlayStation, or a gaming
// PC running Sunshine) and turns what it hands over into the RTP packets
// the game stream sends on to players (see gamestream.Remote).
//
// A bridge's output (stdout) is messages, each an 8-byte header (a type
// letter, 3 bytes of padding, the payload's length as a little-endian
// uint32) and the payload:
//
//	'I'  JSON {"width":W,"height":H,"fps":F}, once connected.
//	'V'  one H.264 access unit, Annex B.
//	'A'  one Opus packet, 48 kHz stereo.
//	'E'  JSON {"error":…}: why the stream ended.
//
// Its input (stdin) takes messages with the same headers, which each
// bridge defines; 'K' asks for a keyframe and 'Q' stops, for every bridge.
package mediabridge

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
	"os/exec"
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

// Hello is the picture the device sends.
type Hello struct {
	Width  int     `json:"width"`
	Height int     `json:"height"`
	FPS    float64 `json:"fps"`
}

// Options says how a bridge's errors read.
type Options struct {
	// Name is the device's, as errors start: "PlayStation".
	Name string
	// Device is how errors name it: "the PlayStation".
	Device string
	// StartTimeout is how long the device may take to start streaming,
	// and TimeoutError what's said when it doesn't.
	StartTimeout time.Duration
	TimeoutError string
	Log          *slog.Logger
}

// Stream is a device being played: its picture and sound as RTP packets,
// and the player's input back to it.
type Stream struct {
	cmd   *exec.Cmd
	in    io.WriteCloser
	inMu  sync.Mutex
	out   *bufio.Reader
	opt   Options
	Hello Hello

	pack        *Packer
	pending     []*rtp.Packet
	pendingKind byte
	closeOnce   sync.Once
	exited      chan struct{}
}

// Packer turns a device's H.264 access units and Opus packets into the
// RTP packets the game stream sends on.
type Packer struct {
	video, audio rtp.Packetizer
	start        time.Time
	audioTS      uint32
	keyframes    keyframer
}

// NewPacker starts a stream's packets.
func NewPacker() *Packer {
	ssrc := rand.Uint32()
	return &Packer{start: time.Now(),
		video: rtp.NewPacketizer(1200, 96, ssrc, &codecs.H264Payloader{DisableStapA: true}, rtp.NewRandomSequencer(), 90000),
		audio: rtp.NewPacketizer(1200, 111, ssrc+1, &codecs.OpusPayloader{}, rtp.NewRandomSequencer(), 48000)}
}

// Video packs one Annex B access unit, stamped with the time it came.
func (k *Packer) Video(au []byte) []*rtp.Packet {
	au = k.keyframes.fix(au)
	ts := uint32(time.Since(k.start).Seconds() * 90000)
	ps := k.video.Packetize(au, 0)
	for _, p := range ps {
		p.Timestamp = ts
	}
	return ps
}

// Audio packs one 48 kHz Opus packet.
func (k *Packer) Audio(b []byte) []*rtp.Packet {
	ps := k.audio.Packetize(b, 0)
	for _, p := range ps {
		p.Timestamp = k.audioTS
	}
	k.audioTS += uint32(opusSamples(b))
	return ps
}

// Start starts the bridge cmd and waits until its stream starts.
func Start(ctx context.Context, cmd *exec.Cmd, opt Options) (*Stream, error) {
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = logWriter{opt.Log, opt.Name + " bridge"}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	s := &Stream{cmd: cmd, in: in, out: bufio.NewReaderSize(out, 1<<20), opt: opt, pack: NewPacker(), exited: make(chan struct{})}
	go func() {
		cmd.Wait()
		close(s.exited)
	}()

	type hello struct {
		h   Hello
		err error
	}
	got := make(chan hello, 1)
	go func() {
		typ, b, err := s.readMsg()
		switch {
		case err != nil:
			got <- hello{err: fmt.Errorf("the %s bridge stopped", opt.Name)}
		case typ == 'E':
			got <- hello{err: s.bridgeError(b)}
		case typ != 'I':
			got <- hello{err: fmt.Errorf("the %s bridge sent %q first", opt.Name, typ)}
		default:
			var h Hello
			if json.Unmarshal(b, &h) != nil || h.Width <= 0 || h.Height <= 0 {
				got <- hello{err: fmt.Errorf("the %s bridge sent something unexpected", opt.Name)}
				return
			}
			got <- hello{h: h}
		}
	}()
	timeout := opt.StartTimeout
	if timeout <= 0 {
		timeout = 45 * time.Second
	}
	select {
	case h := <-got:
		if h.err != nil {
			s.Close()
			return nil, h.err
		}
		s.Hello = h.h
	case <-time.After(timeout):
		s.Close()
		return nil, errors.New(opt.TimeoutError)
	case <-ctx.Done():
		s.Close()
		return nil, ctx.Err()
	}
	return s, nil
}

func (s *Stream) bridgeError(b []byte) error {
	var e struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(b, &e) == nil && e.Error != "" {
		return errors.New(s.opt.Name + ": " + e.Error)
	}
	return fmt.Errorf("%s stopped the stream", s.opt.Device)
}

// readMsg reads one message from the bridge.
func (s *Stream) readMsg() (byte, []byte, error) {
	var h [8]byte
	if _, err := io.ReadFull(s.out, h[:]); err != nil {
		return 0, nil, err
	}
	n := binary.LittleEndian.Uint32(h[4:])
	if n > 16<<20 {
		return 0, nil, fmt.Errorf("the %s bridge sent a message too big", s.opt.Name)
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
			s.pending, s.pendingKind = s.pack.Video(b), KindVideo
		case 'A':
			s.pending, s.pendingKind = s.pack.Audio(b), KindAudio
		case 'E':
			return 0, nil, s.bridgeError(b)
		}
	}
	p := s.pending[0]
	s.pending = s.pending[1:]
	return s.pendingKind, p, nil
}

// Send writes one message to the bridge.
func (s *Stream) Send(typ byte, b []byte) {
	s.inMu.Lock()
	defer s.inMu.Unlock()
	h := [8]byte{typ}
	binary.LittleEndian.PutUint32(h[4:], uint32(len(b)))
	s.in.Write(append(h[:], b...))
}

// Keyframe asks the device for a whole picture, for a player who has
// just joined.
func (s *Stream) Keyframe() { s.Send('K', nil) }

// Close ends the stream.
func (s *Stream) Close() {
	s.closeOnce.Do(func() {
		s.Send('Q', nil)
		s.in.Close()
		select {
		case <-s.exited:
		case <-time.After(3 * time.Second):
			s.cmd.Process.Kill()
		}
	})
}

// logWriter passes the bridge's log lines on to the server's log.
type logWriter struct {
	log  *slog.Logger
	name string
}

func (w logWriter) Write(b []byte) (int, error) {
	if w.log != nil {
		for _, line := range bytesLines(b) {
			w.log.Debug(w.name, "msg", line)
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
