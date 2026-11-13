package gamestream

import (
	"archive/zip"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
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
			c := exec.Command("cc", "-O2", "-shared", "-fPIC", `-DLAGTEST_EXTENSIONS="tst|TST2"`, "-o", filepath.Join(dir, "custom_libretro.so"), filepath.Join(src, "testcore", "testcore.c"))
			a.Stderr, b.Stderr, c.Stderr = os.Stderr, os.Stderr, os.Stderr
			if a.Run() == nil && b.Run() == nil && c.Run() == nil {
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
	r, err := StartRunner(filepath.Join(built, "matinee-game-runner"), filepath.Join(built, "lagtest.so"), "", t.TempDir(), t.TempDir(), nil)
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

// pixel is the colour at x, y of a lag test frame, as 0xRRGGBB.
func pixel(f []byte, x, y int) uint32 {
	i := (y*320 + x) * 4
	return uint32(f[i+2])<<16 | uint32(f[i+1])<<8 | uint32(f[i])
}

// The emulator's settings reach it, and so do analog sticks: the lag
// test's square turns blue when set to, and the left stick moves it.
func TestRunnerOptionsAndSticks(t *testing.T) {
	needBuilt(t)
	r, err := StartRunner(filepath.Join(built, "matinee-game-runner"), filepath.Join(built, "lagtest.so"), "", t.TempDir(), t.TempDir(),
		map[string]string{"lagtest_square": "blue"})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	frames := 0
	err = r.Run(func(f []byte) error {
		frames++
		if frames == 2 {
			if c := pixel(f, 160, 120); c != 0x3050ff {
				t.Fatalf("square is %06x, want blue", c)
			}
			r.SetStick(0, 0, 127)
		}
		// Pushed right, the square leaves the middle and reaches the edge.
		if frames > 2 && pixel(f, 310, 120) == 0x3050ff {
			if pixel(f, 150, 120) == 0x3050ff {
				t.Fatal("square didn't move")
			}
			return io.EOF
		}
		if frames > 120 {
			t.Fatal("the stick never moved the square")
		}
		return nil
	}, func([]byte) error { return nil })
	if err != io.EOF {
		t.Fatal(err)
	}
}

func TestCoreInfo(t *testing.T) {
	needBuilt(t)
	runner := filepath.Join(built, "matinee-game-runner")
	info, err := ReadCoreInfo(context.Background(), runner, filepath.Join(built, "lagtest.so"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Name != "Matinee lag test" || info.Version != "1" || len(info.Extensions) != 0 {
		t.Fatalf("info = %+v", info)
	}
	bad := filepath.Join(t.TempDir(), "bad_libretro.so")
	os.WriteFile(bad, []byte("not a core"), 0o755)
	if _, err := ReadCoreInfo(context.Background(), runner, bad); err == nil {
		t.Fatal("a text file passed as a core")
	}
}

func TestRunnerBadCore(t *testing.T) {
	needBuilt(t)
	_, err := StartRunner(filepath.Join(built, "matinee-game-runner"), filepath.Join(built, "nothing.so"), "", t.TempDir(), t.TempDir(), nil)
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
	s, answer, err := m.Start(ctx, Game{LagTest: true, Owner: "1"}, pc.LocalDescription().SDP)
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

func TestUserEmulators(t *testing.T) {
	needBuilt(t)
	cores := t.TempDir()
	os.WriteFile(filepath.Join(cores, "gambatte_libretro.so"), []byte("stands in"), 0o644)
	m := &Manager{cfg: Config{Runner: filepath.Join(built, "matinee-game-runner"), CoreDir: cores, UserCoreDir: filepath.Join(t.TempDir(), "mine")}}
	if s, ok := m.SystemFor("/games/Tetris.gb"); !ok || s.Key != "gb" || s.Custom {
		t.Fatalf("built-in system = %+v", s)
	}

	// Not an emulator: refused, and not kept.
	if _, err := m.AddEmulator(context.Background(), "junk_libretro.so", strings.NewReader("hello")); !errors.Is(err, ErrNotEmulator) {
		t.Fatalf("junk added: %v", err)
	}
	if _, err := m.AddEmulator(context.Background(), "core.dll", strings.NewReader("hello")); !errors.Is(err, ErrNotEmulator) {
		t.Fatalf("a .dll added: %v", err)
	}
	if entries, _ := os.ReadDir(m.cfg.UserCoreDir); len(entries) != 0 {
		t.Fatalf("left %d files behind", len(entries))
	}

	f, _ := os.Open(filepath.Join(built, "custom_libretro.so"))
	e, err := m.AddEmulator(context.Background(), "../custom_libretro.so", f)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	if e.File != "custom_libretro.so" || e.Name != "Matinee lag test" || strings.Join(e.Extensions, " ") != ".tst .tst2" || e.BuiltIn {
		t.Fatalf("added %+v", e)
	}
	s, ok := m.SystemFor("/games/Thing.TST")
	if !ok || !s.Custom || s.Core != filepath.Join(m.cfg.UserCoreDir, "custom_libretro.so") {
		t.Fatalf("system for .tst = %+v %v", s, ok)
	}
	if !m.IsGameFile("x.tst2") {
		t.Fatal(".tst2 isn't a game file")
	}
	list := m.Emulators()
	if len(list) != 2 || list[0].File != "custom_libretro.so" || !list[1].BuiltIn || list[1].Name != "Gambatte" {
		t.Fatalf("emulators = %+v", list)
	}

	// A broken file put in the folder by hand is listed with why.
	os.WriteFile(filepath.Join(m.cfg.UserCoreDir, "broken_libretro.so"), []byte("x"), 0o755)
	list = m.Emulators()
	if len(list) != 3 || list[0].File != "broken_libretro.so" || list[0].Error == "" {
		t.Fatalf("emulators = %+v", list)
	}

	if err := m.RemoveEmulator("../mine/custom_libretro.so"); err == nil {
		t.Fatal("removed by a path")
	}
	if err := m.RemoveEmulator("custom_libretro.so"); err != nil {
		t.Fatal(err)
	}
	if _, ok := m.SystemFor("/games/Thing.tst"); ok {
		t.Fatal("still plays .tst")
	}

	// A Mac or Windows emulator's .dylib or .dll is kept as .so, like the
	// built-in ones.
	f, _ = os.Open(filepath.Join(built, "custom_libretro.so"))
	e, err = m.AddEmulator(context.Background(), "mac_libretro.dylib", f)
	f.Close()
	if err != nil || e.File != "mac_libretro.so" {
		t.Fatalf("added %+v: %v", e, err)
	}
}

// A zip of a disc unpacks every track beside the .cue, which is played.
func TestUnzipDisc(t *testing.T) {
	dir := t.TempDir()
	zpath := filepath.Join(dir, "Game (USA).zip")
	f, _ := os.Create(zpath)
	zw := zip.NewWriter(f)
	for name, size := range map[string]int{"Game (USA) (Track 1).bin": 4096, "Game (USA) (Track 2).bin": 2048, "Game (USA).cue": 100} {
		w, _ := zw.Create(name)
		w.Write(make([]byte, size))
	}
	zw.Close()
	f.Close()
	if ext := zipGameExt(zpath); ext != ".cue" {
		t.Fatalf("game in the zip is %q", ext)
	}
	out := t.TempDir()
	got, err := unzipGame(zpath, out)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(got) != "Game (USA).cue" {
		t.Fatalf("plays %s", got)
	}
	if entries, _ := os.ReadDir(out); len(entries) != 3 {
		t.Fatalf("unpacked %d files", len(entries))
	}
}

// A built-in emulator that doesn't load on this computer is left out, so
// its games say there's no emulator instead of failing to start.
func TestBrokenBuiltIn(t *testing.T) {
	needBuilt(t)
	dir := t.TempDir()
	// Gambatte plays .gb; this one isn't a library at all.
	if err := os.WriteFile(filepath.Join(dir, "gambatte_libretro.so"), []byte("not a core"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := &Manager{cfg: Config{Runner: filepath.Join(built, "matinee-game-runner"), CoreDir: dir}, log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	if _, ok := m.SystemFor("game.gb"); !ok {
		t.Fatal("before the check, the emulator should count")
	}
	m.checkBuiltIn(context.Background())
	if s, ok := m.SystemFor("game.gb"); ok {
		t.Fatalf("a broken emulator plays .gb: %+v", s)
	}
}
