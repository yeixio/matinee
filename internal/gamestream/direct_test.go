package gamestream

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/pion/rtp"
)

func TestDirectStream(t *testing.T) {
	needBuilt(t)
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("needs FFmpeg")
	}
	tmp := t.TempDir()
	m, err := NewManager(Config{FFmpeg: ffmpeg, Runner: filepath.Join(built, "matinee-game-runner"), CoreDir: built,
		SaveDir: filepath.Join(tmp, "saves"), SystemDir: filepath.Join(tmp, "system"), WorkDir: filepath.Join(tmp, "work")},
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	s, answer, err := m.Start(ctx, Game{LagTest: true, Owner: "1"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if answer != "" {
		t.Fatalf("answer %q for a direct stream", answer)
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
	conn.Write(ctx, websocket.MessageBinary, []byte{1, 0, 0, 1}) // hold A
	conn.Write(ctx, websocket.MessageBinary, []byte{2, 42})

	got := map[string]int{}
	for got["video"] < 30 || got["audio"] < 10 || got["pong"] < 1 || got["stats"] < 1 || got["keyframe"] < 1 {
		typ, b, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("read: %v; got %v", err, got)
		}
		if typ == websocket.MessageText {
			got["stats"]++
			continue
		}
		switch b[0] {
		case directVideo, directAudio:
			var p rtp.Packet
			if err := p.Unmarshal(b[1:]); err != nil {
				t.Fatal(err)
			}
			if b[0] == directAudio {
				got["audio"]++
				continue
			}
			got["video"]++
			// A keyframe starts with its SPS, alone or first in a STAP-A
			// (the player needs it in the stream: there's no SDP).
			if n := p.Payload[0] & 0x1f; n == 7 || (n == 24 && len(p.Payload) > 3 && p.Payload[3]&0x1f == 7) {
				got["keyframe"]++
			}
		case 2:
			if len(b) == 2 && b[1] == 42 {
				got["pong"]++
			}
		}
	}

	// A second player can't take over the stream.
	conn2, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := conn2.Read(ctx); err == nil {
		t.Fatal("second player got a message")
	}

	// The player going away ends the game.
	conn.Close(websocket.StatusNormalClosure, "")
	select {
	case <-s.Done():
	case <-ctx.Done():
		t.Fatal("game still running after the player left")
	}
	if m.Count() != 0 {
		t.Fatal("session still listed")
	}
}
