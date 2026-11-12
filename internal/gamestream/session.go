package gamestream

import (
	"archive/zip"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pion/ice/v4"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

// Config says where the pieces are.
type Config struct {
	FFmpeg  string
	Runner  string // matinee-game-runner
	CoreDir string // the emulator cores (*.so) built into Matinee
	// UserCoreDir has the emulator cores an admin added (see emulators.go).
	UserCoreDir string
	SaveDir     string // games' saves, a folder per person under it
	SystemDir   string // BIOS files some systems need
	WorkDir     string // games unpacked from zips while they're played
	// Port is the one UDP port every stream goes through, so a firewall or
	// a container needs only that one opened; 0 picks ports at random.
	Port int
	// PublicIPs, when set, are the addresses players reach this server's
	// Port at, for when the server can't see its own (behind NAT).
	PublicIPs []string
	// MaxSessions is how many games can be played at once.
	MaxSessions int
}

// Manager starts and keeps track of the games being played.
type Manager struct {
	cfg Config
	log *slog.Logger
	api *webrtc.API
	mux ice.UDPMux

	mu       sync.Mutex
	sessions map[string]*Session

	coresMu   sync.Mutex
	userCores map[string]userCore // by file name
}

// NewManager sets up WebRTC on cfg.Port.
func NewManager(cfg Config, log *slog.Logger) (*Manager, error) {
	if cfg.MaxSessions <= 0 {
		cfg.MaxSessions = 4
	}
	m := &Manager{cfg: cfg, log: log, sessions: map[string]*Session{}}
	var se webrtc.SettingEngine
	se.SetNetworkTypes([]webrtc.NetworkType{webrtc.NetworkTypeUDP4, webrtc.NetworkTypeUDP6})
	if cfg.Port > 0 {
		mux, err := ice.NewMultiUDPMuxFromPort(cfg.Port)
		if err != nil {
			return nil, fmt.Errorf("game streaming port %d: %w", cfg.Port, err)
		}
		m.mux = mux
		se.SetICEUDPMux(mux)
	}
	if len(cfg.PublicIPs) > 0 {
		se.SetNAT1To1IPs(cfg.PublicIPs, webrtc.ICECandidateTypeHost)
	}
	// No mDNS names for candidates: players connect by address.
	se.SetICEMulticastDNSMode(ice.MulticastDNSModeDisabled)

	var me webrtc.MediaEngine
	if err := me.RegisterCodec(webrtc.RTPCodecParameters{RTPCodecCapability: videoCodec, PayloadType: 96}, webrtc.RTPCodecTypeVideo); err != nil {
		return nil, err
	}
	if err := me.RegisterCodec(webrtc.RTPCodecParameters{RTPCodecCapability: audioCodec, PayloadType: 111}, webrtc.RTPCodecTypeAudio); err != nil {
		return nil, err
	}
	m.api = webrtc.NewAPI(webrtc.WithSettingEngine(se), webrtc.WithMediaEngine(&me))
	return m, nil
}

var (
	videoCodec = webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264, ClockRate: 90000,
		SDPFmtpLine: "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42e020"}
	audioCodec = webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2,
		SDPFmtpLine: "minptime=10;useinbandfec=1;stereo=1"}
)

// Close ends every game and frees the port.
func (m *Manager) Close() {
	m.mu.Lock()
	all := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		all = append(all, s)
	}
	m.mu.Unlock()
	for _, s := range all {
		s.Stop()
	}
	if m.mux != nil {
		m.mux.Close()
	}
}

// Game is what to play.
type Game struct {
	// System is what plays it: its Core and Options. Leave it empty, with
	// LagTest set, for the lag test.
	System  System
	LagTest bool
	Path    string // the game file; empty for the lag test
	// Owner is whose saves these are; one person plays one game at a time.
	Owner string
}

// ErrBusy means as many games are being played as the server allows.
var ErrBusy = errors.New("too many games are being played")

// Session is one game being played and streamed to one player.
type Session struct {
	ID    string
	Owner string
	Info  Info

	m      *Manager
	pc     *webrtc.PeerConnection
	runner *Runner
	work   string
	cancel context.CancelFunc
	once   sync.Once
	done   chan struct{}
	// Started is when the player connected and the game began.
	Started time.Time

	dc           atomic.Pointer[webrtc.DataChannel]
	encodeMicros atomic.Int64

	// How the picture, sound and messages reach the player: over WebRTC,
	// or straight over a WebSocket (direct.go). Set before connected closes.
	video, audio func(*rtp.Packet) error
	text         func(string)
	connected    chan struct{}
	connectOnce  sync.Once
}

// connect starts the game streaming through the given senders, once.
func (s *Session) connect(video, audio func(*rtp.Packet) error, text func(string)) bool {
	ok := false
	s.connectOnce.Do(func() {
		s.video, s.audio, s.text = video, audio, text
		close(s.connected)
		ok = true
	})
	return ok
}

// Start starts g and answers the player's WebRTC offer. The game itself
// begins once the player has connected. With no offer, the player connects
// straight to the session instead (see Session.ServeDirect), and there's
// no answer.
func (m *Manager) Start(ctx context.Context, g Game, offer string) (*Session, string, error) {
	core := g.System.Core
	if g.LagTest {
		core = filepath.Join(m.cfg.CoreDir, LagTest)
	}
	if _, err := os.Stat(core); core == "" || err != nil {
		return nil, "", fmt.Errorf("no emulator for this game on this server")
	}
	// One game per person: starting another ends the one before.
	m.mu.Lock()
	var old []*Session
	for _, s := range m.sessions {
		if s.Owner == g.Owner {
			old = append(old, s)
		}
	}
	m.mu.Unlock()
	for _, s := range old {
		s.Stop()
	}
	m.mu.Lock()
	full := len(m.sessions) >= m.cfg.MaxSessions
	m.mu.Unlock()
	if full {
		return nil, "", ErrBusy
	}

	id := newID()
	s := &Session{ID: id, Owner: g.Owner, m: m, done: make(chan struct{}), connected: make(chan struct{})}
	var err error
	defer func() {
		if err != nil {
			s.Stop()
		}
	}()
	s.work = filepath.Join(m.cfg.WorkDir, id)
	if err = os.MkdirAll(s.work, 0o755); err != nil {
		return nil, "", err
	}
	path := g.Path
	if strings.EqualFold(filepath.Ext(path), ".zip") && !g.System.playsZips() {
		if path, err = unzipGame(path, s.work); err != nil {
			return nil, "", err
		}
	}
	saves := filepath.Join(m.cfg.SaveDir, safeName(g.Owner))
	if err = os.MkdirAll(saves, 0o755); err != nil {
		return nil, "", err
	}
	os.MkdirAll(m.cfg.SystemDir, 0o755)
	if s.runner, err = StartRunner(m.cfg.Runner, core, path, saves, m.cfg.SystemDir, g.System.Options); err != nil {
		return nil, "", err
	}
	s.Info = s.runner.Info

	answer := ""
	if offer != "" {
		if answer, err = s.startWebRTC(ctx, offer); err != nil {
			return nil, "", err
		}
	}

	m.mu.Lock()
	m.sessions[id] = s
	m.mu.Unlock()

	playCtx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	go func() {
		select {
		case <-s.connected:
		case <-time.After(30 * time.Second):
			m.log.Info("game player never connected", "session", id)
			s.Stop()
			return
		case <-s.done:
			return
		}
		s.Started = time.Now()
		if err := s.play(playCtx); err != nil && playCtx.Err() == nil {
			m.log.Warn("game stopped", "session", id, "err", err)
		}
		s.Stop()
	}()
	return s, answer, nil
}

// startWebRTC answers the player's offer; the session connects once the
// player's side does.
func (s *Session) startWebRTC(ctx context.Context, offer string) (string, error) {
	m := s.m
	var err error
	if s.pc, err = m.api.NewPeerConnection(webrtc.Configuration{}); err != nil {
		return "", err
	}
	video, err := webrtc.NewTrackLocalStaticRTP(videoCodec, "video", "game")
	if err != nil {
		return "", err
	}
	audio, err := webrtc.NewTrackLocalStaticRTP(audioCodec, "audio", "game")
	if err != nil {
		return "", err
	}
	for _, t := range []webrtc.TrackLocal{video, audio} {
		var sender *webrtc.RTPSender
		if sender, err = s.pc.AddTrack(t); err != nil {
			return "", err
		}
		go drainRTCP(sender)
	}
	s.pc.OnDataChannel(func(dc *webrtc.DataChannel) {
		s.dc.Store(dc)
		dc.OnMessage(func(msg webrtc.DataChannelMessage) { s.input(func(b []byte) { dc.Send(b) }, msg.Data) })
	})
	text := func(t string) {
		if dc := s.dc.Load(); dc != nil {
			dc.SendText(t)
		}
	}
	s.pc.OnConnectionStateChange(func(st webrtc.PeerConnectionState) {
		switch st {
		case webrtc.PeerConnectionStateConnected:
			s.connect(video.WriteRTP, audio.WriteRTP, text)
		case webrtc.PeerConnectionStateFailed, webrtc.PeerConnectionStateClosed:
			go s.Stop()
		case webrtc.PeerConnectionStateDisconnected:
			// Wi-Fi drops for a moment now and then; give it a few seconds.
			go func() {
				time.Sleep(8 * time.Second)
				if s.pc.ConnectionState() == webrtc.PeerConnectionStateDisconnected {
					s.Stop()
				}
			}()
		}
	})

	if err = s.pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: offer}); err != nil {
		return "", fmt.Errorf("bad offer: %w", err)
	}
	answer, err := s.pc.CreateAnswer(nil)
	if err != nil {
		return "", err
	}
	gathered := webrtc.GatheringCompletePromise(s.pc)
	if err = s.pc.SetLocalDescription(answer); err != nil {
		return "", err
	}
	select {
	case <-gathered:
	case <-ctx.Done():
		return "", ctx.Err()
	}

	return s.pc.LocalDescription().SDP, nil
}

// play runs the game, encoding as it goes, until it stops.
func (s *Session) play(ctx context.Context) error {
	venc, err := startVideo(ctx, s.m.cfg.FFmpeg, s.Info)
	if err != nil {
		return err
	}
	defer venc.Close()
	aenc, err := startAudio(ctx, s.m.cfg.FFmpeg, s.Info)
	if err != nil {
		return err
	}
	defer aenc.Close()
	// How long FFmpeg takes with each frame, for the player's lag numbers:
	// when each frame went in, and when its last packet came out.
	var wrote [64]time.Time
	var in, out int
	var mu sync.Mutex
	go venc.forward(func(p *rtp.Packet) error {
		if p.Marker {
			mu.Lock()
			if out < in {
				d := time.Since(wrote[out%len(wrote)])
				out++
				s.encodeMicros.Store((s.encodeMicros.Load()*7 + d.Microseconds()) / 8)
			}
			mu.Unlock()
		}
		return s.video(p)
	})
	go aenc.forward(s.audio)
	go s.sendStats(ctx)
	return s.runner.Run(func(f []byte) error {
		mu.Lock()
		wrote[in%len(wrote)] = time.Now()
		in++
		if in-out > len(wrote) {
			out = in - len(wrote)
		}
		mu.Unlock()
		return venc.Write(f)
	}, aenc.Write)
}

// sendStats tells the player, once a second, how long the server takes
// with each frame, as JSON text.
func (s *Session) sendStats(ctx context.Context) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		frame := 1000 / s.Info.FPS
		s.text(fmt.Sprintf(`{"encodeMs":%.1f,"frameMs":%.1f}`, float64(s.encodeMicros.Load())/1000, frame))
	}
}

// Input messages from the player, on the data channel or the WebSocket:
//
//	1 port lo hi   buttons held (see Runner.SetButtons)
//	2 …            a ping: sent straight back, to time the round trip
//	3 port axis v  an analog stick (see Runner.SetStick), v a signed byte
func (s *Session) input(reply func([]byte), b []byte) {
	if len(b) == 0 {
		return
	}
	switch b[0] {
	case 1:
		if len(b) >= 4 {
			s.runner.SetButtons(int(b[1]), uint16(b[2])|uint16(b[3])<<8)
		}
	case 2:
		reply(b)
	case 3:
		if len(b) >= 4 {
			s.runner.SetStick(int(b[1]), int(b[2]), int8(b[3]))
		}
	}
}

// Stop ends the game, keeping its save.
func (s *Session) Stop() {
	s.once.Do(func() {
		// Unlisted first, so a game counts as gone once Done says so.
		s.m.mu.Lock()
		delete(s.m.sessions, s.ID)
		s.m.mu.Unlock()
		close(s.done)
		if s.cancel != nil {
			s.cancel()
		}
		if s.pc != nil {
			s.pc.Close()
		}
		if s.runner != nil {
			s.runner.Close()
		}
		if s.work != "" {
			os.RemoveAll(s.work)
		}
	})
}

// Done is closed once the game has stopped.
func (s *Session) Done() <-chan struct{} { return s.done }

// Get finds a game being played.
func (m *Manager) Get(id string) *Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sessions[id]
}

// Count is how many games are being played.
func (m *Manager) Count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.sessions)
}

func drainRTCP(s *webrtc.RTPSender) {
	buf := make([]byte, 1500)
	for {
		if _, _, err := s.Read(buf); err != nil {
			return
		}
	}
}

func newID() string {
	b := make([]byte, 12)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func safeName(s string) string {
	s = strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == 0 || r == ':' {
			return '_'
		}
		return r
	}, s)
	if s == "" || s == "." || s == ".." {
		return "_"
	}
	return s
}

// maxUnzipped is the biggest game we'll unpack from a zip: a cartridge is
// far smaller, a set of PlayStation discs about this big.
const maxUnzipped = 4 << 30

// unzipGame unpacks the game from a zip into dir and returns its path (see
// zipGame). A disc's tracks are unpacked with it, beside it.
func unzipGame(path, dir string) (string, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return "", fmt.Errorf("can't open the zip: %w", err)
	}
	defer zr.Close()
	best, whole := zipGame(zr.File)
	if best == nil {
		return "", fmt.Errorf("the zip is empty")
	}
	files := []*zip.File{best}
	if whole {
		files = nil
		for _, f := range zr.File {
			if !f.FileInfo().IsDir() {
				files = append(files, f)
			}
		}
	}
	var total uint64
	for _, f := range files {
		total += f.UncompressedSize64
	}
	if total > maxUnzipped {
		return "", fmt.Errorf("the game in the zip is too big")
	}
	var out string
	left := int64(maxUnzipped)
	for _, f := range files {
		// A lone game is named like the zip, so saves are named after the
		// game, with the file's own extension, which some emulators go by.
		// A disc's files keep their names, which its .cue refers to.
		name := filepath.Base(f.Name)
		if !whole {
			name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)) + filepath.Ext(f.Name)
		}
		to := filepath.Join(dir, safeName(name))
		n, err := unzipFile(f, to, left)
		if err != nil {
			return "", err
		}
		left -= n
		if f == best {
			out = to
		}
	}
	return out, nil
}

func unzipFile(f *zip.File, to string, limit int64) (int64, error) {
	rc, err := f.Open()
	if err != nil {
		return 0, err
	}
	defer rc.Close()
	w, err := os.Create(to)
	if err != nil {
		return 0, err
	}
	n, err := io.Copy(w, io.LimitReader(rc, limit))
	if err != nil {
		w.Close()
		return n, err
	}
	return n, w.Close()
}
