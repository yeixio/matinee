package gamehost

import (
	"bytes"
	"cmp"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
)

// Host is the game host's side: it serves the protocol in protocol.go.
type Host struct {
	// SteamRoot is Steam's folder (see SteamRoot), or empty to look for it
	// in SteamHome each time: on a first start Steam hasn't made it yet.
	SteamRoot string
	SteamHome string
	// Token is what Matinee must send; empty lets anyone on the network in.
	Token string
	// Display is the X display the games show on, such as :55.
	Display string
	// FFmpeg encodes; VAAPI is the GPU's render node to encode on, such as
	// /dev/dri/renderD128, or empty to encode on the CPU.
	FFmpeg string
	VAAPI  string
	// AudioSource is the PulseAudio source the games' sound is captured
	// from: the default output's monitor unless set.
	AudioSource string
	// Bitrate is the picture's bitrate in megabits a second.
	Bitrate int
	// Steam is the command that launches a game: Steam's own, run with
	// -applaunch <app id>.
	Steam string
	// Desktop is the address of a web page showing this computer's screen
	// (see desktop.go), or empty for none.
	Desktop string
	Log     *slog.Logger
	// VideoInput and AudioInput replace FFmpeg's input arguments, for tests.
	VideoInput, AudioInput []string

	busy atomic.Bool
}

func (h *Host) log() *slog.Logger { return cmp.Or(h.Log, slog.Default()) }

// Handler serves the protocol.
func (h *Host) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/games", h.auth(h.games))
	mux.HandleFunc("GET /v1/play/{app}", h.auth(h.play))
	mux.HandleFunc(DesktopPath, h.auth(h.desktop()))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok\n")) })
	return mux
}

func (h *Host) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.Token != "" {
			got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			if subtle.ConstantTimeCompare([]byte(got), []byte(h.Token)) != 1 {
				http.Error(w, "wrong token", http.StatusUnauthorized)
				return
			}
		}
		next(w, r)
	}
}

func (h *Host) games(w http.ResponseWriter, r *http.Request) {
	root := h.SteamRoot
	if root == "" {
		var err error
		if root, err = SteamRoot(h.SteamHome); err != nil {
			// No Steam yet: nothing installed.
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"games":[]}` + "\n"))
			return
		}
	}
	games, err := Installed(root)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if games == nil {
		games = []Game{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"games": games})
}

// writeWait is how long one message may take to send before Matinee
// counts as gone.
const writeWait = 5 * time.Second

func (h *Host) play(w http.ResponseWriter, r *http.Request) {
	app, err := strconv.Atoi(r.PathValue("app"))
	if err != nil || app < 0 {
		http.Error(w, "bad app id", http.StatusBadRequest)
		return
	}
	q := r.URL.Query()
	width, _ := strconv.Atoi(q.Get("width"))
	height, _ := strconv.Atoi(q.Get("height"))
	fps, _ := strconv.ParseFloat(q.Get("fps"), 64)
	width, height = clampSize(width, height)
	if fps < 24 || fps > 120 {
		fps = 60
	}
	if !h.busy.CompareAndSwap(false, true) {
		http.Error(w, "already playing a game", http.StatusConflict)
		return
	}
	defer h.busy.Store(false)
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	defer conn.CloseNow()
	conn.SetReadLimit(1 << 10)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	log := h.log().With("app", app)
	if err := h.launch(app); err != nil {
		log.Warn("couldn't launch the game", "err", err)
		conn.Close(websocket.StatusInternalError, "couldn't launch the game")
		return
	}
	defer func() {
		if app == BigPicture {
			h.steamURL("steam://close/bigpicture")
		}
		stopGame(app)
	}()
	log.Info("playing", "width", width, "height", height, "fps", fps)

	var wmu sync.Mutex
	send := func(typ websocket.MessageType, b []byte) error {
		wmu.Lock()
		defer wmu.Unlock()
		wctx, done := context.WithTimeout(ctx, writeWait)
		defer done()
		err := conn.Write(wctx, typ, b)
		if err != nil {
			cancel()
		}
		return err
	}
	encoder := "libx264"
	if h.VAAPI != "" {
		encoder = "h264_vaapi"
	}
	video, err := h.startCapture(ctx, h.videoArgs(width, height, fps), KindVideo, send)
	if err != nil {
		log.Warn("couldn't capture the picture", "err", err)
		conn.Close(websocket.StatusInternalError, "couldn't capture the picture")
		return
	}
	defer video.stop()
	audio, err := h.startCapture(ctx, h.audioArgs(), KindAudio, send)
	if err != nil {
		log.Warn("couldn't capture the sound; playing without it", "err", err)
	} else {
		defer audio.stop()
	}
	hello, _ := json.Marshal(Hello{Width: width, Height: height, FPS: fps, Encoder: encoder})
	if send(websocket.MessageText, hello) != nil {
		return
	}

	// The game is over when its processes have come and gone.
	go func() {
		seen := false
		gone := 0
		for {
			select {
			case <-ctx.Done():
				return
			case <-video.done:
				log.Warn("picture capture stopped", "err", video.err())
				conn.Close(websocket.StatusInternalError, "the picture capture stopped")
				cancel()
				return
			case <-time.After(2 * time.Second):
			}
			if app == BigPicture {
				// Played until the player leaves: games come and go in it.
				continue
			}
			if len(gamePIDs(app)) > 0 {
				seen, gone = true, 0
			} else if seen {
				if gone++; gone >= 3 {
					log.Info("the game ended")
					conn.Close(websocket.StatusNormalClosure, "game over")
					cancel()
					return
				}
			}
		}
	}()

	// The player's controllers, keyboard and mouse, made when first used.
	type pad struct {
		dev   *device
		state padState
	}
	var pads [4]*pad
	var keyboard, pointer *device
	held := map[uint16]bool{}
	defer func() {
		for _, p := range pads {
			if p != nil {
				p.dev.close()
			}
		}
		if keyboard != nil {
			// Nothing stays pressed once the player has gone.
			keyboard.send(releaseAll(held))
			keyboard.close()
		}
		if pointer != nil {
			pointer.close()
		}
	}()
	padFor := func(port int) *pad {
		if port < 0 || port > 3 {
			return nil
		}
		if pads[port] == nil {
			d, err := newDevice(padSpec(port))
			if err != nil {
				log.Warn("no virtual gamepad", "err", err)
				return nil
			}
			pads[port] = &pad{dev: d}
		}
		return pads[port]
	}
	// A device that couldn't be made isn't tried again for every key.
	var noKeyboard, noPointer bool
	deviceFor := func(k kbmKind) *device {
		open := func(d **device, failed *bool, spec deviceSpec) *device {
			if *d == nil && !*failed {
				var err error
				if *d, err = newDevice(spec); err != nil {
					log.Warn("no virtual "+spec.Name, "err", err)
					*failed = true
				}
			}
			return *d
		}
		switch k {
		case kbmKeys:
			return open(&keyboard, &noKeyboard, keyboardSpec())
		case kbmPointer:
			return open(&pointer, &noPointer, pointerSpec())
		}
		return nil
	}
	for {
		typ, b, err := conn.Read(ctx)
		if err != nil {
			return
		}
		if typ != websocket.MessageBinary || len(b) < 3 {
			continue
		}
		switch b[0] {
		case 1:
			if p := padFor(int(b[1])); p != nil && len(b) >= 4 {
				p.dev.send(p.state.setButtons(uint16(b[2]) | uint16(b[3])<<8))
			}
		case 3:
			if p := padFor(int(b[1])); p != nil && len(b) >= 4 {
				p.dev.send(p.state.setStick(int(b[2]), int8(b[3])))
			}
		default:
			if k, evs := kbmEvents(b, held); len(evs) > 0 {
				if d := deviceFor(k); d != nil {
					d.send(evs)
				}
			}
		}
	}
}

// clampSize keeps the picture to what the encoder takes: 640×360 to 3840×2160,
// even sides, 1920×1080 unless asked.
func clampSize(w, h int) (int, int) {
	if w <= 0 || h <= 0 {
		return 1920, 1080
	}
	w, h = min(max(w, 640), 3840), min(max(h, 360), 2160)
	return w &^ 1, h &^ 1
}

// BigPicture, as an app id, plays Steam itself: its Big Picture mode full
// screen, where the player picks and quits games with the controller.
const BigPicture = 0

func (h *Host) launch(app int) error {
	if app == BigPicture {
		return h.steamURL("steam://open/bigpicture")
	}
	return h.steamRun("-applaunch", strconv.Itoa(app))
}

// steamURL hands a steam:// link to the running Steam.
func (h *Host) steamURL(u string) error { return h.steamRun(u) }

func (h *Host) steamRun(args ...string) error {
	cmd := exec.Command(cmp.Or(h.Steam, "steam"), args...)
	cmd.Env = h.env()
	if err := cmd.Start(); err != nil {
		return err
	}
	// With Steam running this hands the launch to it and returns.
	go cmd.Wait()
	return nil
}

func (h *Host) env() []string {
	env := os.Environ()
	if h.Display != "" {
		env = append(env, "DISPLAY="+h.Display)
	}
	return env
}

// videoArgs capture the display and encode it as H.264 for low delay: no
// B-frames, a keyframe a second, a steady bitrate.
func (h *Host) videoArgs(width, height int, fps float64) []string {
	rate := strconv.FormatFloat(fps, 'f', -1, 64)
	gop := strconv.Itoa(int(fps + 0.5))
	br := strconv.Itoa(cmp.Or(h.Bitrate, 20)) + "M"
	args := []string{"-fflags", "nobuffer", "-probesize", "32", "-thread_queue_size", "64",
		"-f", "x11grab", "-draw_mouse", "0", "-framerate", rate, "-i", cmp.Or(h.Display, os.Getenv("DISPLAY"))}
	if h.VideoInput != nil {
		args = slices.Clone(h.VideoInput)
	}
	if h.VAAPI != "" {
		args = append(args, "-vaapi_device", h.VAAPI,
			"-vf", fmt.Sprintf("format=nv12,hwupload,scale_vaapi=w=%d:h=%d", width, height),
			"-c:v", "h264_vaapi", "-profile:v", "constrained_baseline", "-rc_mode", "CBR",
			"-b:v", br, "-maxrate", br, "-bufsize", br, "-g", gop, "-bf", "0", "-async_depth", "1")
	} else {
		args = append(args, "-vf", fmt.Sprintf("scale=%d:%d,format=yuv420p", width, height),
			"-c:v", "libx264", "-preset", "ultrafast", "-tune", "zerolatency", "-profile:v", "baseline",
			"-b:v", br, "-maxrate", br, "-bufsize", "2M", "-g", gop, "-keyint_min", gop, "-bf", "0")
	}
	return append(args, "-payload_type", "96", "-ssrc", "1")
}

// audioArgs capture the games' sound as Opus in 10 ms packets.
func (h *Host) audioArgs() []string {
	in := []string{"-fflags", "nobuffer", "-f", "pulse", "-fragment_size", "1920", "-i", cmp.Or(h.AudioSource, "@DEFAULT_MONITOR@")}
	if h.AudioInput != nil {
		in = slices.Clone(h.AudioInput)
	}
	return append(in, "-ac", "2", "-ar", "48000",
		"-c:a", "libopus", "-application", "lowdelay", "-frame_duration", "10", "-b:a", "128k",
		"-payload_type", "111", "-ssrc", "2")
}

// capture is one FFmpeg sending RTP packets to a port on localhost, each
// passed on with its kind byte.
type capture struct {
	cmd    *exec.Cmd
	conn   *net.UDPConn
	done   chan struct{}
	stderr bytes.Buffer
	mu     sync.Mutex
}

func (c *capture) err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if s := strings.TrimSpace(c.stderr.String()); s != "" {
		return errors.New(s)
	}
	return errors.New("FFmpeg stopped")
}

type lockedWriter struct {
	c *capture
	n int
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.c.mu.Lock()
	defer l.c.mu.Unlock()
	if l.n > 0 {
		q := p[:min(len(p), l.n)]
		l.c.stderr.Write(q)
		l.n -= len(q)
	}
	return len(p), nil
}

func (h *Host) startCapture(ctx context.Context, args []string, kind byte, send func(websocket.MessageType, []byte) error) (*capture, error) {
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		return nil, err
	}
	conn.SetReadBuffer(4 << 20)
	port := conn.LocalAddr().(*net.UDPAddr).Port
	args = append(append([]string{"-hide_banner", "-loglevel", "error", "-nostdin"}, args...),
		"-f", "rtp", fmt.Sprintf("rtp://127.0.0.1:%d?pkt_size=1200", port))
	c := &capture{conn: conn, done: make(chan struct{})}
	c.cmd = exec.CommandContext(ctx, cmp.Or(h.FFmpeg, "ffmpeg"), args...)
	c.cmd.Env = h.env()
	c.cmd.Stderr = &lockedWriter{c: c, n: 16 << 10}
	if err := c.cmd.Start(); err != nil {
		conn.Close()
		return nil, err
	}
	go func() {
		c.cmd.Wait()
		close(c.done)
		conn.Close()
	}()
	go func() {
		buf := make([]byte, 1600)
		for {
			n, _, err := conn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			msg := make([]byte, n+1)
			msg[0] = kind
			copy(msg[1:], buf[:n])
			if send(websocket.MessageBinary, msg) != nil {
				return
			}
		}
	}()
	return c, nil
}

func (c *capture) stop() {
	if c.cmd.Process != nil {
		c.cmd.Process.Signal(os.Interrupt)
	}
	select {
	case <-c.done:
	case <-time.After(3 * time.Second):
		c.cmd.Process.Kill()
		<-c.done
	}
}
