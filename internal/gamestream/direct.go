package gamestream

import (
	"context"
	"errors"
	"time"

	"github.com/coder/websocket"
	"github.com/pion/rtp"
)

// The direct stream, for players without WebRTC (the Apple TV app): the
// same RTP packets FFmpeg makes, sent over a WebSocket on the server's own
// port instead of WebRTC's UDP. Over a home network the extra delay of TCP
// is a millisecond or so, and the player decodes the H.264 and Opus itself.
//
// From the server, binary messages are a kind byte and an RTP packet:
//
//	0x10 rtp   a video packet (H.264, packetization mode 1)
//	0x11 rtp   a sound packet (Opus, 48 kHz stereo)
//
// and a ping sent back as it came (kind 2). Text messages are the same
// JSON stats WebRTC players get. From the player, binary messages are the
// input messages Session.input reads.
const (
	directVideo = 0x10
	directAudio = 0x11
)

// writeWait is how long one message may take to send before the player
// counts as gone.
const writeWait = 5 * time.Second

// ServeDirect streams the session over conn until either side stops. The
// session must have been started without an offer.
func (s *Session) ServeDirect(ctx context.Context, conn *websocket.Conn) error {
	if s.pc != nil {
		return errors.New("this game streams over WebRTC")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	conn.SetReadLimit(1 << 10)
	send := func(kind byte) func(*rtp.Packet) error {
		return func(p *rtp.Packet) error {
			b, err := p.Marshal()
			if err != nil {
				return nil
			}
			wctx, done := context.WithTimeout(ctx, writeWait)
			defer done()
			if err := conn.Write(wctx, websocket.MessageBinary, append([]byte{kind}, b...)); err != nil {
				cancel()
				return err
			}
			return nil
		}
	}
	write := func(typ websocket.MessageType, b []byte) {
		wctx, done := context.WithTimeout(ctx, writeWait)
		defer done()
		if conn.Write(wctx, typ, b) != nil {
			cancel()
		}
	}
	if !s.connect(send(directVideo), send(directAudio), func(t string) { write(websocket.MessageText, []byte(t)) }) {
		return errors.New("a player is already connected to this game")
	}
	go func() {
		select {
		case <-s.done:
		case <-ctx.Done():
		}
		cancel()
	}()
	defer s.Stop()
	for {
		typ, b, err := conn.Read(ctx)
		if err != nil {
			if s.isDone() {
				conn.Close(websocket.StatusNormalClosure, "game over")
				return nil
			}
			return err
		}
		if typ == websocket.MessageBinary {
			s.input(func(r []byte) { write(websocket.MessageBinary, r) }, b)
		}
	}
}

func (s *Session) isDone() bool {
	select {
	case <-s.done:
		return true
	default:
		return false
	}
}
