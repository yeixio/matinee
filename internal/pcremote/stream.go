package pcremote

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"time"

	"github.com/yeixio/matinee/internal/mediabridge"
)

// Bridge runs matinee-pcremote.
type Bridge struct {
	// Path is matinee-pcremote's.
	Path string
}

// Available says whether this server has the bridge.
func (b *Bridge) Available() bool {
	if b == nil || b.Path == "" {
		return false
	}
	st, err := os.Stat(b.Path)
	return err == nil && !st.IsDir()
}

// LaunchQuery is what the bridge needs added to a launch request.
func (b *Bridge) LaunchQuery(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, b.Path, "query").Output()
	if err != nil {
		return "", errors.New("the PC bridge didn't start")
	}
	var q struct {
		LaunchQuery string `json:"launchQuery"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(out), &q); err != nil {
		return "", errors.New("the PC bridge didn't start")
	}
	return q.LaunchQuery, nil
}

// Settings is the picture asked of the PC.
type Settings struct {
	Width, Height, FPS int
	// Bitrate is in kilobits a second.
	Bitrate int
}

// DefaultSettings is 1080p at 60 frames a second, 20 megabits: what the
// game host sends too.
var DefaultSettings = Settings{Width: 1920, Height: 1080, FPS: 60, Bitrate: 20000}

// Stream is the PC being played: its picture and sound as RTP packets
// (see mediabridge.Stream), and the player's input back to it.
type Stream struct {
	*mediabridge.Stream
	mu    sync.Mutex
	input input
	quit  func()
	once  sync.Once
}

// Play starts app on the PC and streams it.
func (b *Bridge) Play(ctx context.Context, c *Client, pc PC, app int, set Settings, log *slog.Logger) (*Stream, error) {
	if !b.Available() {
		return nil, errors.New("this server can't play games on a PC")
	}
	query, err := b.LaunchQuery(ctx)
	if err != nil {
		return nil, err
	}
	key := make([]byte, 16)
	rand.Read(key)
	var kid [4]byte
	rand.Read(kid[:])
	keyID := binary.BigEndian.Uint32(kid[:]) & 0x7fffffff
	rtsp, info, err := c.Launch(ctx, pc, app, set.Width, set.Height, set.FPS, key, keyID, query)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(b.Path, "play", pc.Host, strconv.Itoa(set.Width), strconv.Itoa(set.Height), strconv.Itoa(set.FPS), strconv.Itoa(set.Bitrate))
	cmd.Env = append(os.Environ(),
		"MATINEE_PC_APPVERSION="+info.AppVersion, "MATINEE_PC_GFEVERSION="+info.GfeVersion,
		"MATINEE_PC_RTSP="+rtsp, "MATINEE_PC_CODECS="+strconv.Itoa(info.Codecs),
		"MATINEE_PC_RIKEY="+hex.EncodeToString(key), "MATINEE_PC_RIKEYID="+strconv.FormatUint(uint64(keyID), 10))
	quit := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := c.Quit(ctx, pc); err != nil && log != nil {
			log.Debug("couldn't end the game on the PC", "err", err)
		}
	}
	st, err := mediabridge.Start(ctx, cmd, mediabridge.Options{Name: "PC", Device: "the PC",
		StartTimeout: 30 * time.Second, TimeoutError: "the PC didn't start streaming: check that its firewall lets Sunshine's ports through", Log: log})
	if err != nil {
		quit()
		return nil, err
	}
	return &Stream{Stream: st, input: newInput(st.Hello.Width, st.Hello.Height), quit: quit}, nil
}

// Input takes the player's input messages (see gamestream.Session.input):
// controllers, keyboard and mouse.
func (s *Stream) Input(b []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, m := range s.input.apply(b) {
		s.Send(m.typ, m.b)
	}
}

// Close ends the stream, and the game on the PC with it, so Big Picture
// doesn't stay up on the PC's own screen.
func (s *Stream) Close() {
	s.mu.Lock()
	for _, m := range s.input.release() {
		s.Send(m.typ, m.b)
	}
	s.mu.Unlock()
	s.Stream.Close()
	s.once.Do(s.quit)
}

// Packet kinds, as the game stream's (gamehost.KindVideo and KindAudio).
const (
	KindVideo = mediabridge.KindVideo
	KindAudio = mediabridge.KindAudio
)
