package gamestream

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"sync"

	"github.com/coder/websocket"
	"github.com/pion/rtp"

	"github.com/yeixio/matinee/internal/gamehost"
	"github.com/yeixio/matinee/internal/psremote"
)

// The picture a PC game is played at. Players scale it to their screens.
const (
	hostWidth  = 1920
	hostHeight = 1080
	hostFPS    = 60
)

// Remote is a game played somewhere other than this server that sends its
// own stream, already encoded: a PC game on a game host, or a console.
// The server only passes its packets on.
type Remote interface {
	// Next returns its next packet: video or sound (Kind is
	// gamehost.KindVideo or KindAudio, with RTP), or JSON Text for the
	// player. io.EOF means it ended on its own.
	Next() (RemotePacket, error)
	// Input sends on the player's input messages (see Session.input).
	Input(b []byte)
	// Keyframe asks for a whole picture, for a player who just joined.
	Keyframe()
	Close()
}

// RemotePacket is one thing a Remote sends.
type RemotePacket struct {
	Kind byte
	RTP  *rtp.Packet
	Text string
}

// StartRemote starts a Remote game: it returns its stream and the picture
// it sends.
type StartRemote func(ctx context.Context) (Remote, Info, error)

// startHost has the game host start g.AppID.
func (s *Session) startHost(ctx context.Context, g Game) error {
	conn, hello, err := g.Host.Play(ctx, g.AppID, hostWidth, hostHeight, hostFPS)
	if err != nil {
		return err
	}
	s.m.log.Info("playing a game on the game host", "session", s.ID, "app", g.AppID, "encoder", hello.Encoder,
		"size", strconv.Itoa(hello.Width)+"x"+strconv.Itoa(hello.Height))
	s.startRemote(&hostRemote{conn: conn}, Info{Width: hello.Width, Height: hello.Height, FPS: hello.FPS})
	return nil
}

// startRemote passes r's stream on to the player once the player connects:
// it's encoded already, so packets go through as they come.
func (s *Session) startRemote(r Remote, info Info) {
	s.remote = r
	info.SampleRate = 48000
	info.Aspect = float64(info.Width) / float64(info.Height)
	s.Info = info
	s.remoteDone = make(chan error, 1)
	// Read from the start, so the game never waits on us; until the player
	// connects, what it sends is dropped.
	go func() {
		s.remoteDone <- s.relayRemote(r)
	}()
}

func (s *Session) relayRemote(r Remote) error {
	joined := false
	for {
		p, err := r.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		select {
		case <-s.connected:
		default:
			continue
		}
		if !joined {
			// What came before the player joined is gone, the keyframe
			// with it: ask for another, so the picture starts at once.
			joined = true
			r.Keyframe()
		}
		var send func(*rtp.Packet) error
		switch {
		case p.Text != "":
			s.text(p.Text)
			continue
		case p.RTP == nil:
			continue
		case p.Kind == gamehost.KindVideo:
			send = s.video
		case p.Kind == gamehost.KindAudio:
			send = s.audio
		default:
			continue
		}
		if err := send(p.RTP); err != nil {
			return err
		}
	}
}

// playRemote passes the game's stream on until it or the player stops.
func (s *Session) playRemote(ctx context.Context) error {
	go s.sendStats(ctx)
	select {
	case err := <-s.remoteDone:
		return err
	case <-ctx.Done():
		return nil
	}
}

// hostRemote is a game host's play socket (see gamehost.Client.Play).
type hostRemote struct {
	conn *websocket.Conn
}

func (h *hostRemote) Next() (RemotePacket, error) {
	for {
		typ, b, err := h.conn.Read(context.Background())
		if err != nil {
			if websocket.CloseStatus(err) == websocket.StatusNormalClosure {
				return RemotePacket{}, io.EOF
			}
			return RemotePacket{}, fmt.Errorf("game host: %w", err)
		}
		if typ == websocket.MessageText {
			// Anything the host says goes on to the player as stats do.
			if json.Valid(b) {
				return RemotePacket{Text: string(b)}, nil
			}
			continue
		}
		if len(b) < 2 {
			continue
		}
		var p rtp.Packet
		if p.Unmarshal(b[1:]) != nil {
			continue
		}
		return RemotePacket{Kind: b[0], RTP: &p}, nil
	}
}

func (h *hostRemote) Input(b []byte) {
	ctx, cancel := context.WithTimeout(context.Background(), writeWait)
	defer cancel()
	h.conn.Write(ctx, websocket.MessageBinary, b)
}

// Keyframe does nothing: the host sends one every couple of seconds.
func (h *hostRemote) Keyframe() {}

func (h *hostRemote) Close() {
	// Closing the host's stream ends the game there.
	h.conn.Close(websocket.StatusNormalClosure, "game over")
}

// ConsoleRemote is a PlayStation being played (see internal/psremote).
func ConsoleRemote(st *psremote.Stream) (Remote, Info) {
	return &consoleRemote{st: st}, Info{Width: st.Hello.Width, Height: st.Hello.Height, FPS: st.Hello.FPS}
}

type consoleRemote struct {
	st *psremote.Stream
	mu sync.Mutex // one Input at a time: the pad keeps what's held
}

func (c *consoleRemote) Next() (RemotePacket, error) {
	kind, p, err := c.st.Next()
	if err != nil {
		return RemotePacket{}, err
	}
	return RemotePacket{Kind: kind, RTP: p}, nil
}

func (c *consoleRemote) Input(b []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.st.Input(b)
}

func (c *consoleRemote) Keyframe() { c.st.Keyframe() }
func (c *consoleRemote) Close()    { c.st.Close() }
