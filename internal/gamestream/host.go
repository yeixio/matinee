package gamestream

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/coder/websocket"
	"github.com/pion/rtp"

	"github.com/yeixio/matinee/internal/gamehost"
)

// The picture a PC game is played at. Players scale it to their screens.
const (
	hostWidth  = 1920
	hostHeight = 1080
	hostFPS    = 60
)

// startHost has the game host start g.AppID, and passes its stream on to
// the player once the player connects: the host encodes, so packets go
// through as they come.
func (s *Session) startHost(ctx context.Context, g Game) error {
	conn, hello, err := g.Host.Play(ctx, g.AppID, hostWidth, hostHeight, hostFPS)
	if err != nil {
		return err
	}
	s.host = conn
	s.Info = Info{Width: hello.Width, Height: hello.Height, FPS: hello.FPS, SampleRate: 48000,
		Aspect: float64(hello.Width) / float64(hello.Height)}
	s.hostDone = make(chan error, 1)
	s.m.log.Info("playing a game on the game host", "session", s.ID, "app", g.AppID, "encoder", hello.Encoder,
		"size", strconv.Itoa(hello.Width)+"x"+strconv.Itoa(hello.Height))
	// Read from the start, so the host never waits on us; until the player
	// connects, what it sends is dropped.
	go func() {
		s.hostDone <- s.relayHost(conn)
	}()
	return nil
}

func (s *Session) relayHost(conn *websocket.Conn) error {
	for {
		typ, b, err := conn.Read(context.Background())
		if err != nil {
			if websocket.CloseStatus(err) == websocket.StatusNormalClosure {
				return nil
			}
			return fmt.Errorf("game host: %w", err)
		}
		select {
		case <-s.connected:
		default:
			continue
		}
		if typ == websocket.MessageText {
			// Anything the host says goes on to the player as stats do.
			if json.Valid(b) {
				s.text(string(b))
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
		var send func(*rtp.Packet) error
		switch b[0] {
		case gamehost.KindVideo:
			send = s.video
		case gamehost.KindAudio:
			send = s.audio
		default:
			continue
		}
		if err := send(&p); err != nil {
			return err
		}
	}
}

// playHost passes the host's stream on until it or the player stops.
func (s *Session) playHost(ctx context.Context) error {
	go s.sendStats(ctx)
	select {
	case err := <-s.hostDone:
		return err
	case <-ctx.Done():
		return nil
	}
}

// hostInput sends the player's buttons and sticks on to the host.
func (s *Session) hostInput(b []byte) {
	ctx, cancel := context.WithTimeout(context.Background(), writeWait)
	defer cancel()
	s.host.Write(ctx, websocket.MessageBinary, b)
}
