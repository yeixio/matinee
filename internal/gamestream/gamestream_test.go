package gamestream

import (
	"archive/zip"
	"context"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"
)

// built has the runner and the lag test core, compiled for the tests; empty
// when there's no C compiler, and the tests that need them skip.
var built string

func TestMain(m *testing.M) {
	dir, _ := os.MkdirTemp("", "gamestream")
	if runtime.GOOS == "linux" {
		if _, err := exec.LookPath("cc"); err == nil {
			src := filepath.Join("..", "..", "games")
			a := exec.Command("cc", "-O2", "-o", filepath.Join(dir, "matinee-game-runner"), filepath.Join(src, "runner", "runner.c"), "-ldl")
			b := exec.Command("cc", "-O2", "-shared", "-fPIC", "-o", filepath.Join(dir, "lagtest.so"), filepath.Join(src, "testcore", "testcore.c"))
			a.Stderr, b.Stderr = os.Stderr, os.Stderr
			if a.Run() == nil && b.Run() == nil {
				built = dir
			}
		}
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func needBuilt(t *testing.T) {
	t.Helper()
	if built == "" {
		t.Skip("needs a C compiler on Linux")
	}
}

func TestRunnerFramesAndButtons(t *testing.T) {
	needBuilt(t)
	r, err := StartRunner(filepath.Join(built, "matinee-game-runner"), filepath.Join(built, "lagtest.so"), "", t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if r.Info.Width != 320 || r.Info.Height != 240 || r.Info.FPS != 60 || r.Info.SampleRate != 48000 {
		t.Fatalf("info = %+v", r.Info)
	}
	frames, sound := 0, 0
	white := false
	pressed := false
	err = r.Run(func(f []byte) error {
		if len(f) != 320*240*4 {
			t.Fatalf("frame is %d bytes", len(f))
		}
		frames++
		if frames == 5 {
			// Hold A: the lag test turns white.
			r.SetButtons(0, 1<<8)
			pressed = true
		}
		if pressed && f[0] == 0xff && f[1] == 0xff && f[2] == 0xff {
			white = true
			return io.EOF
		}
		if frames > 60 {
			return io.EOF
		}
		return nil
	}, func(s []byte) error {
		sound += len(s)
		return nil
	})
	if err != io.EOF {
		t.Fatal(err)
	}
	if !white {
		t.Fatal("holding A never turned the picture white")
	}
	if sound == 0 {
		t.Fatal("no sound")
	}
}

func TestRunnerBadCore(t *testing.T) {
	needBuilt(t)
	_, err := StartRunner(filepath.Join(built, "matinee-game-runner"), filepath.Join(built, "nothing.so"), "", t.TempDir(), t.TempDir())
	if err == nil {
		t.Fatal("started without a core")
	}
}

func TestScaleFor(t *testing.T) {
	for _, c := range []struct{ w, h, k int }{{160, 144, 5}, {256, 240, 3}, {240, 160, 4}, {320, 240, 3}, {640, 480, 1}, {1920, 1080, 1}} {
		if k := scaleFor(c.w, c.h); k != c.k {
			t.Errorf("scaleFor(%d, %d) = %d, want %d", c.w, c.h, k, c.k)
		}
	}
}

func TestUnzipGame(t *testing.T) {
	dir := t.TempDir()
	zpath := filepath.Join(dir, "Tetris (World).zip")
	f, _ := os.Create(zpath)
	zw := zip.NewWriter(f)
	w, _ := zw.Create("readme.txt")
	w.Write([]byte("hi"))
	w, _ = zw.Create("tetris.gb")
	w.Write(make([]byte, 32768))
	zw.Close()
	f.Close()
	out, err := unzipGame(zpath, dir)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(out) != "Tetris (World).gb" {
		t.Fatalf("unpacked to %s", out)
	}
	if st, _ := os.Stat(out); st == nil || st.Size() != 32768 {
		t.Fatal("wrong size")
	}
}

// TestStream plays the lag test to a WebRTC player on this machine and
// checks pictures and sound arrive and a ping comes back.
func TestStream(t *testing.T) {
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

	var se webrtc.SettingEngine
	se.SetIncludeLoopbackCandidate(true)
	var me webrtc.MediaEngine
	me.RegisterDefaultCodecs()
	api := webrtc.NewAPI(webrtc.WithSettingEngine(se), webrtc.WithMediaEngine(&me))
	pc, err := api.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	pc.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo, webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly})
	pc.AddTransceiverFromKind(webrtc.RTPCodecTypeAudio, webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly})
	dc, err := pc.CreateDataChannel("input", nil)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int{}
	gotc := make(chan string, 64)
	pc.OnTrack(func(tr *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		for {
			if _, _, err := tr.ReadRTP(); err != nil {
				return
			}
			select {
			case gotc <- tr.Kind().String():
			default:
			}
		}
	})
	dc.OnOpen(func() {
		dc.Send([]byte{1, 0, 0, 1}) // hold A
		dc.Send([]byte{2, 42})
	})
	dc.OnMessage(func(msg webrtc.DataChannelMessage) {
		if msg.IsString {
			gotc <- "stats"
			return
		}
		if len(msg.Data) == 2 && msg.Data[0] == 2 && msg.Data[1] == 42 {
			gotc <- "pong"
		}
	})
	offer, err := pc.CreateOffer(nil)
	if err != nil {
		t.Fatal(err)
	}
	gathered := webrtc.GatheringCompletePromise(pc)
	pc.SetLocalDescription(offer)
	<-gathered

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	s, answer, err := m.Start(ctx, Game{Core: "lagtest.so", Owner: "1"}, pc.LocalDescription().SDP)
	if err != nil {
		t.Fatal(err)
	}
	if err := pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: answer}); err != nil {
		t.Fatal(err)
	}
	for got["video"] < 30 || got["audio"] < 10 || got["pong"] < 1 || got["stats"] < 1 {
		select {
		case k := <-gotc:
			got[k]++
		case <-ctx.Done():
			t.Fatalf("timed out; got %v", got)
		}
	}
	if m.Count() != 1 {
		t.Fatalf("%d sessions", m.Count())
	}
	s.Stop()
	if m.Count() != 0 {
		t.Fatal("session still listed after Stop")
	}
}
