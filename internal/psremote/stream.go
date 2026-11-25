package psremote

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"time"

	"github.com/yeixio/matinee/internal/mediabridge"
)

// Packet kinds, as the game stream's (gamehost.KindVideo and KindAudio).
const (
	KindVideo = mediabridge.KindVideo
	KindAudio = mediabridge.KindAudio
)

// Stream is a console being played: its picture and sound as RTP packets
// (see mediabridge.Stream), and the player's controller back to it.
type Stream struct {
	*mediabridge.Stream
	padMu sync.Mutex
	pad   pad
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
	// The console answers within a few seconds, or not at all.
	st, err := mediabridge.Start(ctx, cmd, mediabridge.Options{Name: "PlayStation", Device: "the PlayStation",
		StartTimeout: 45 * time.Second, TimeoutError: "the PlayStation didn't start Remote Play: is it already being played remotely?", Log: log})
	if err != nil {
		return nil, err
	}
	return &Stream{Stream: st}, nil
}

// Input takes the player's input messages (see gamestream.Session.input):
// the first controller's buttons and sticks.
func (s *Stream) Input(b []byte) {
	s.padMu.Lock()
	defer s.padMu.Unlock()
	if st, ok := s.pad.apply(b); ok {
		s.Send('C', st)
	}
}
