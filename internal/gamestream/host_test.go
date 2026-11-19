package gamestream

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/pion/rtp"

	"github.com/yeixio/matinee/internal/gamehost"
)

// A PC game from a game host reaches the player over the direct stream,
// and the player leaving ends it on the host.
func TestHostGame(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("needs FFmpeg")
	}
	root := filepath.Join(t.TempDir(), "Steam")
	os.MkdirAll(filepath.Join(root, "steamapps"), 0o755)
	h := &gamehost.Host{SteamRoot: root, Token: "t", FFmpeg: ffmpeg, Steam: "true",
		VideoInput: []string{"-re", "-f", "lavfi", "-i", "testsrc2=size=640x360:rate=30"},
		AudioInput: []string{"-re", "-f", "lavfi", "-i", "sine=sample_rate=48000"}}
	hostSrv := httptest.NewServer(h.Handler())
	defer hostSrv.Close()

	m, err := NewManager(Config{FFmpeg: ffmpeg}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s, _, err := m.Start(ctx, Game{Host: &gamehost.Client{URL: hostSrv.URL, Token: "t"}, AppID: 1677740, Owner: "1"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if s.Info.Width != hostWidth || s.Info.FPS != hostFPS {
		t.Fatalf("info %+v", s.Info)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		s.ServeDirect(r.Context(), conn)
	}))
	defer srv.Close()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	conn.SetReadLimit(1 << 20)
	conn.Write(ctx, websocket.MessageBinary, []byte{1, 0, 1, 0})
	conn.Write(ctx, websocket.MessageBinary, []byte{2, 7})
	got := map[byte]int{}
	for got[directVideo] < 20 || got[directAudio] < 5 || got[2] < 1 {
		typ, b, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("read: %v; got %v", err, got)
		}
		if typ == websocket.MessageBinary && len(b) > 0 {
			got[b[0]]++
		}
	}
	conn.Close(websocket.StatusNormalClosure, "")
	select {
	case <-s.Done():
	case <-ctx.Done():
		t.Fatal("game still running after the player left")
	}
	// The host is free for the next game.
	for range 50 {
		if _, err := (&gamehost.Client{URL: hostSrv.URL, Token: "t"}).Games(ctx); err == nil {
			if c, _, err := (&gamehost.Client{URL: hostSrv.URL, Token: "t"}).Play(ctx, 1, 640, 360, 30); err == nil {
				c.CloseNow()
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("host still busy")
}

// fakeRemote sends a video packet every few milliseconds until closed.
type fakeRemote struct {
	keyframes, inputs atomic.Int32
	closed            chan struct{}
	once              sync.Once
	seq               uint16
}

func (f *fakeRemote) Next() (RemotePacket, error) {
	select {
	case <-f.closed:
		return RemotePacket{}, io.EOF
	case <-time.After(5 * time.Millisecond):
	}
	f.seq++
	return RemotePacket{Kind: gamehost.KindVideo, RTP: &rtp.Packet{Header: rtp.Header{Version: 2, SequenceNumber: f.seq}, Payload: []byte{0x65, 1}}}, nil
}
func (f *fakeRemote) Input([]byte) { f.inputs.Add(1) }
func (f *fakeRemote) Keyframe()    { f.keyframes.Add(1) }
func (f *fakeRemote) Close()       { f.once.Do(func() { close(f.closed) }) }

// A game played elsewhere, such as a console, reaches the player as it
// comes; it's asked for a keyframe when the player joins, gets the
// player's buttons, and ends when the player leaves.
func TestRemoteGame(t *testing.T) {
	m, err := NewManager(Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	f := &fakeRemote{closed: make(chan struct{})}
	start := func(context.Context) (Remote, Info, error) {
		return f, Info{Width: 1920, Height: 1080, FPS: 60}, nil
	}
	s, _, err := m.Start(ctx, Game{Remote: start, Owner: "1"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if s.Info.Width != 1920 || s.Info.SampleRate != 48000 {
		t.Fatalf("info %+v", s.Info)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		s.ServeDirect(r.Context(), conn)
	}))
	defer srv.Close()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	conn.Write(ctx, websocket.MessageBinary, []byte{1, 0, 1, 0})
	for n := 0; n < 5; {
		typ, b, err := conn.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if typ == websocket.MessageBinary && b[0] == directVideo {
			n++
		}
	}
	if f.keyframes.Load() != 1 {
		t.Fatalf("keyframes asked: %d", f.keyframes.Load())
	}
	conn.Close(websocket.StatusNormalClosure, "")
	select {
	case <-f.closed:
	case <-ctx.Done():
		t.Fatal("still playing after the player left")
	}
	if f.inputs.Load() != 1 {
		t.Fatalf("inputs: %d", f.inputs.Load())
	}
}
