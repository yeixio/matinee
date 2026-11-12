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
	"testing"
	"time"

	"github.com/coder/websocket"

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
