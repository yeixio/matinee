package xboxremote

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pion/ice/v4"
	"github.com/pion/interceptor"
	"github.com/pion/rtcp"
	"github.com/pion/rtp"
	"github.com/pion/rtp/codecs"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media/samplebuilder"

	"github.com/yeixio/matinee/internal/mediabridge"
)

// Packet kinds, as the game stream's (gamehost.KindVideo and KindAudio).
const (
	KindVideo = mediabridge.KindVideo
	KindAudio = mediabridge.KindAudio
)

// Stream is an Xbox being played: its picture and sound as RTP packets,
// and the player's controller back to it.
type Stream struct {
	Hello mediabridge.Hello

	sess  *session
	pc    *webrtc.PeerConnection
	pack  *mediabridge.Packer
	log   *slog.Logger
	start time.Time

	control, input, message *webrtc.DataChannel

	out       chan kindPacket
	done      chan struct{}
	closeOnce sync.Once
	errMu     sync.Mutex
	err       error

	mu         sync.Mutex
	pad        pad
	frames     []frameTimes
	seq        uint32
	inputOn    bool
	padOn      bool // the console has been told the controller is there
	sendErr    bool // a failed input send has been logged
	seen       map[byte]bool
	videoSSRC  uint32
	lastKeyReq time.Time

	ready     chan struct{} // the console took the handshake
	readyOnce sync.Once
	firstPic  chan struct{}
	picOnce   sync.Once
}

type kindPacket struct {
	kind byte
	p    *rtp.Packet
}

// The stream's size: the console sends 1080p to a Windows player, and
// players scale it to their screens.
const (
	width  = 1920
	height = 1080
	fps    = 60
)

// Play starts remote play on the console and waits until its picture
// comes. A console in Sleep wakes on its own.
func (c *Client) Play(ctx context.Context, t *Tokens, consoleID string, log *slog.Logger) (*Stream, error) {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	if err := c.wake(ctx, t, consoleID); err != nil {
		log.Info("Xbox didn't take the turn-on command", "err", err)
	}
	sess, err := c.startSession(ctx, t, consoleID)
	if err != nil {
		return nil, err
	}
	s := &Stream{Hello: mediabridge.Hello{Width: width, Height: height, FPS: fps}, sess: sess, pack: mediabridge.NewPacker(), log: log,
		start: time.Now(), out: make(chan kindPacket, 1024), done: make(chan struct{}), ready: make(chan struct{}), firstPic: make(chan struct{})}
	if err := s.connect(ctx); err != nil {
		s.Close()
		return nil, err
	}
	select {
	case <-s.firstPic:
	case <-s.done:
		return nil, s.why()
	case <-ctx.Done():
		s.Close()
		return nil, errors.New("the Xbox didn't send its picture in time")
	}
	go s.keepalive()
	go s.inputLoop()
	return s, nil
}

func newAPI() (*webrtc.API, error) {
	me := &webrtc.MediaEngine{}
	fb := []webrtc.RTCPFeedback{{Type: "goog-remb"}, {Type: "ccm", Parameter: "fir"}, {Type: "nack"}, {Type: "nack", Parameter: "pli"}}
	for i, profile := range []string{"4d001f", "42e01f", "42001f"} {
		if err := me.RegisterCodec(webrtc.RTPCodecParameters{RTPCodecCapability: webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264, ClockRate: 90000,
			SDPFmtpLine: "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=" + profile, RTCPFeedback: fb},
			PayloadType: webrtc.PayloadType(102 + 2*i)}, webrtc.RTPCodecTypeVideo); err != nil {
			return nil, err
		}
	}
	if err := me.RegisterCodec(webrtc.RTPCodecParameters{RTPCodecCapability: webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2,
		SDPFmtpLine: "minptime=10;useinbandfec=1;stereo=1"}, PayloadType: 111}, webrtc.RTPCodecTypeAudio); err != nil {
		return nil, err
	}
	ir := &interceptor.Registry{}
	if err := webrtc.ConfigureNack(me, ir); err != nil {
		return nil, err
	}
	if err := webrtc.ConfigureRTCPReports(ir); err != nil {
		return nil, err
	}
	var se webrtc.SettingEngine
	se.SetICEMulticastDNSMode(ice.MulticastDNSModeDisabled)
	return webrtc.NewAPI(webrtc.WithMediaEngine(me), webrtc.WithInterceptorRegistry(ir), webrtc.WithSettingEngine(se)), nil
}

// connect sets up the WebRTC connection with the console, through the
// session.
func (s *Stream) connect(ctx context.Context) error {
	api, err := newAPI()
	if err != nil {
		return err
	}
	pc, err := api.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		return err
	}
	s.pc = pc
	ordered := true
	channel := func(label, protocol string) (*webrtc.DataChannel, error) {
		return pc.CreateDataChannel(label, &webrtc.DataChannelInit{Ordered: &ordered, Protocol: &protocol})
	}
	if _, err := channel("chat", "chatV1"); err != nil {
		return err
	}
	if s.control, err = channel("control", "controlV1"); err != nil {
		return err
	}
	if s.input, err = channel("input", "1.0"); err != nil {
		return err
	}
	if s.message, err = channel("message", "messageV1"); err != nil {
		return err
	}
	if _, err := pc.AddTransceiverFromKind(webrtc.RTPCodecTypeAudio, webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionSendrecv}); err != nil {
		return err
	}
	if _, err := pc.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo, webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly}); err != nil {
		return err
	}
	s.message.OnOpen(func() {
		s.send(s.message, map[string]string{"type": "Handshake", "version": "messageV1", "id": newID(), "cv": "0"})
	})
	s.message.OnMessage(s.onMessage)
	s.input.OnMessage(s.onInput)
	pc.OnTrack(func(tr *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		if tr.Kind() == webrtc.RTPCodecTypeVideo {
			go s.readVideo(tr)
		} else {
			go s.readAudio(tr)
		}
	})
	pc.OnConnectionStateChange(func(st webrtc.PeerConnectionState) {
		s.log.Debug("Xbox connection", "state", st.String())
		if st == webrtc.PeerConnectionStateFailed {
			s.fail(errors.New("lost the connection to the Xbox"))
		} else if st == webrtc.PeerConnectionStateClosed {
			s.fail(io.EOF)
		}
	})

	var ours []string
	var oursMu sync.Mutex
	pc.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c == nil {
			return
		}
		init := c.ToJSON()
		b, _ := json.Marshal(map[string]any{"candidate": init.Candidate, "sdpMid": init.SDPMid, "sdpMLineIndex": init.SDPMLineIndex,
			"usernameFragment": init.UsernameFragment})
		oursMu.Lock()
		ours = append(ours, string(b))
		oursMu.Unlock()
	})
	offer, err := pc.CreateOffer(nil)
	if err != nil {
		return err
	}
	gathered := webrtc.GatheringCompletePromise(pc)
	if err := pc.SetLocalDescription(offer); err != nil {
		return err
	}
	select {
	case <-gathered:
	case <-ctx.Done():
		return ctx.Err()
	}
	answer, err := s.sess.answer(ctx, pc.LocalDescription().SDP)
	if err != nil {
		return err
	}
	if err := pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: answer}); err != nil {
		return fmt.Errorf("the Xbox's answer didn't fit: %w", err)
	}
	oursMu.Lock()
	sent := append([]string(nil), ours...)
	oursMu.Unlock()
	theirs, err := s.sess.candidates(ctx, sent)
	if err != nil {
		return err
	}
	for _, c := range theirs {
		for _, init := range candidateInits(c) {
			if err := pc.AddICECandidate(init); err != nil {
				s.log.Debug("Xbox address not used", "candidate", init.Candidate, "err", err)
			}
		}
	}
	return nil
}

// candidateInits is a console address as pion takes it, with the home
// network address an IPv6 Teredo address stands for.
func candidateInits(c remoteCandidate) []webrtc.ICECandidateInit {
	cand := strings.TrimPrefix(strings.TrimSpace(c.Candidate), "a=")
	if cand == "" || cand == "end-of-candidates" {
		return nil
	}
	mid := c.SDPMid
	var index uint16
	switch v := c.SDPMLineIndex.(type) {
	case float64:
		index = uint16(v)
	case string:
		n, _ := strconv.Atoi(v)
		index = uint16(n)
	}
	mk := func(cand string) webrtc.ICECandidateInit {
		return webrtc.ICECandidateInit{Candidate: cand, SDPMid: &mid, SDPMLineIndex: &index}
	}
	var out []webrtc.ICECandidateInit
	if f := strings.Fields(cand); len(f) > 5 {
		if ip, port, ok := teredo(f[4]); ok {
			out = append(out, mk("candidate:10 1 UDP 1 "+ip+" 9002 typ host"), mk("candidate:11 1 UDP 1 "+ip+" "+strconv.Itoa(port)+" typ host"))
		}
	}
	return append(out, mk(cand))
}

// teredo is the IPv4 address and port a Teredo address (2001:0::/32)
// carries, which the console may give instead of its home network one.
func teredo(addr string) (string, int, bool) {
	ip := net.ParseIP(addr)
	if ip == nil || ip.To4() != nil || ip[0] != 0x20 || ip[1] != 0x01 || ip[2] != 0 || ip[3] != 0 {
		return "", 0, false
	}
	v4 := net.IPv4(ip[12]^0xff, ip[13]^0xff, ip[14]^0xff, ip[15]^0xff)
	port := int(uint16(ip[10])<<8|uint16(ip[11])) ^ 0xffff
	return v4.String(), port, true
}

func newID() string {
	b := make([]byte, 16)
	rand.Read(b)
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// send sends v as JSON, as bytes, the way xbox.com's player does.
func (s *Stream) send(dc *webrtc.DataChannel, v any) {
	b, err := json.Marshal(v)
	if err == nil {
		dc.Send(b)
	}
}

// message is one of the console's messages on the message channel.
type message struct {
	Type    string `json:"type"`
	ID      string `json:"id"`
	Target  string `json:"target"`
	Content string `json:"content"`
}

func (s *Stream) onMessage(m webrtc.DataChannelMessage) {
	var msg message
	if json.Unmarshal(m.Data, &msg) != nil {
		return
	}
	switch msg.Type {
	case "HandshakeAck":
		s.readyOnce.Do(func() {
			s.send(s.control, map[string]string{"message": "authorizationRequest", "accessKey": "4BDB3609-C1F1-4195-9B37-FEFF45DA8B8E"})
			// As xbox.com's player does: add and take out the first
			// controller with the authorization, then add it for real
			// once the player's controller is there (addPad).
			for _, added := range []bool{true, false} {
				s.send(s.control, map[string]any{"message": "gamepadChanged", "gamepadIndex": 0, "wasAdded": added})
			}
			s.input.Send(clientMetadata(time.Since(s.start)))
			for _, m := range []struct {
				target  string
				content any
			}{
				{"/streaming/systemUi/configuration", map[string]any{"version": []int{0, 2, 0}, "systemUis": []int{}}},
				{"/streaming/properties/clientappinstallidchanged", map[string]string{"clientAppInstallId": newID()}},
				{"/streaming/characteristics/orientationchanged", map[string]int{"orientation": 0}},
				{"/streaming/characteristics/touchinputenabledchanged", map[string]bool{"touchInputEnabled": false}},
				{"/streaming/characteristics/clientdevicecapabilities", map[string]any{}},
				{"/streaming/characteristics/dimensionschanged", map[string]any{"horizontal": width, "vertical": height,
					"preferredWidth": width, "preferredHeight": height, "safeAreaLeft": 0, "safeAreaTop": 0,
					"safeAreaRight": width, "safeAreaBottom": height, "supportsCustomResolution": true}},
			} {
				content, _ := json.Marshal(m.content)
				s.send(s.message, map[string]string{"type": "Message", "content": string(content), "id": newID(), "target": m.target, "cv": ""})
			}
			s.mu.Lock()
			s.inputOn = true
			s.mu.Unlock()
			close(s.ready)
		})
	case "TransactionStart", "Message":
		switch msg.Target {
		case "/streaming/sessionLifetimeManagement/serverInitiatedDisconnect":
			s.complete(msg.ID, "")
			s.fail(errors.New("the Xbox ended remote play"))
		case "/streaming/systemUi/messages/ShowMessageDialog":
			// Nobody can answer it here: take the first choice, which is
			// what goes ahead.
			s.log.Info("the Xbox asked something", "dialog", msg.Content)
			s.complete(msg.ID, map[string]int{"Result": 0})
		}
	}
}

func (s *Stream) complete(id string, content any) {
	b, _ := json.Marshal(content)
	s.send(s.message, map[string]string{"type": "TransactionComplete", "content": string(b), "id": id, "cv": ""})
}

// onInput reads the console's reports: the picture's size, and rumble,
// which isn't passed on.
func (s *Stream) onInput(m webrtc.DataChannelMessage) {
	b := m.Data
	if len(b) > 0 {
		s.mu.Lock()
		if s.seen == nil {
			s.seen = map[byte]bool{}
		}
		if !s.seen[b[0]] {
			s.seen[b[0]] = true
			s.log.Info("Xbox input report", "kind", b[0], "bytes", len(b))
		}
		s.mu.Unlock()
	}
	if len(b) >= 10 && b[0] == reportServerMetadata {
		h, w := int(le32(b[2:])), int(le32(b[6:]))
		s.log.Debug("Xbox picture", "width", w, "height", h)
	}
}

func le32(b []byte) uint32 {
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
}

// readVideo reads the console's picture, a whole access unit at a time,
// and packs it again for the players.
func (s *Stream) readVideo(tr *webrtc.TrackRemote) {
	s.mu.Lock()
	s.videoSSRC = uint32(tr.SSRC())
	s.mu.Unlock()
	sb := samplebuilder.New(256, &codecs.H264Packet{}, 90000)
	gotKey := false
	for {
		p, _, err := tr.ReadRTP()
		if err != nil {
			return
		}
		sb.Push(p)
		for sample := sb.Pop(); sample != nil; sample = sb.Pop() {
			if sample.PrevDroppedPackets > 0 {
				gotKey = false
				s.Keyframe()
			}
			if !gotKey {
				if !hasIDR(sample.Data) {
					s.Keyframe()
					continue
				}
				gotKey = true
			}
			s.mu.Lock()
			if len(s.frames) < 30 {
				s.frames = append(s.frames, frameTimes{key: sample.PacketTimestamp, arrived: uint32(time.Since(s.start).Milliseconds())})
			}
			s.mu.Unlock()
			for _, pkt := range s.pack.Video(sample.Data) {
				if !s.push(KindVideo, pkt) {
					return
				}
			}
			s.picOnce.Do(func() { close(s.firstPic) })
		}
	}
}

// hasIDR says whether an Annex B access unit holds a keyframe.
func hasIDR(au []byte) bool {
	for i := 0; i+3 < len(au); i++ {
		if au[i] == 0 && au[i+1] == 0 && au[i+2] == 1 && au[i+3]&0x1f == 5 {
			return true
		}
	}
	return false
}

func (s *Stream) readAudio(tr *webrtc.TrackRemote) {
	s.log.Info("Xbox sound track", "codec", tr.Codec().MimeType, "channels", tr.Codec().Channels)
	first := true
	for {
		p, _, err := tr.ReadRTP()
		if err != nil {
			return
		}
		if len(p.Payload) == 0 {
			continue
		}
		if first {
			first = false
			s.log.Info("Xbox sound started", "bytes", len(p.Payload), "toc", p.Payload[0])
		}
		for _, pkt := range s.pack.Audio(p.Payload) {
			if !s.push(KindAudio, pkt) {
				return
			}
		}
	}
}

func (s *Stream) push(kind byte, p *rtp.Packet) bool {
	select {
	case s.out <- kindPacket{kind, p}:
		return true
	case <-s.done:
		return false
	}
}

// inputLoop sends the pictures' metadata frames, with the controller,
// about once a picture, as xbox.com's player does: the console tunes the
// stream with them.
func (s *Stream) inputLoop() {
	t := time.NewTicker(16 * time.Millisecond)
	defer t.Stop()
	// Without a press from the player within a second, add the
	// controller anyway, so the console has one.
	addBy := time.Now().Add(time.Second)
	for {
		select {
		case <-s.done:
			return
		case <-t.C:
		}
		s.mu.Lock()
		if s.inputOn && !s.padOn && time.Now().After(addBy) {
			s.addPad()
		}
		if s.inputOn && (s.padOn || len(s.frames) > 0) {
			s.sendReport()
		}
		s.mu.Unlock()
	}
}

// addPad tells the console the first controller is there; s.mu is held.
func (s *Stream) addPad() {
	s.padOn = true
	s.send(s.control, map[string]any{"message": "gamepadChanged", "gamepadIndex": 0, "wasAdded": true})
	s.log.Info("Xbox controller added")
}

// sendReport sends what's queued and, once added, the controller, as
// xbox.com's player does every 16 ms; s.mu is held.
func (s *Stream) sendReport() {
	s.seq++
	var gamepad []byte
	if s.padOn {
		gamepad = s.pad.frame()
	}
	b := report(s.seq, time.Since(s.start), s.frames, gamepad)
	s.frames = s.frames[:0]
	if err := s.input.Send(b); err != nil && !s.sendErr {
		s.sendErr = true
		s.log.Warn("Xbox input not sent", "err", err, "state", s.input.ReadyState().String())
	}
}

// keepalive keeps the session going on Microsoft's side.
func (s *Stream) keepalive() {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-t.C:
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := s.sess.keepalive(ctx); err != nil {
			s.log.Debug("Xbox keepalive", "err", err)
		}
		cancel()
	}
}

// Next returns the next RTP packet, with its kind (KindVideo or
// KindAudio). It returns io.EOF once the stream ends normally, or why it
// ended.
func (s *Stream) Next() (byte, *rtp.Packet, error) {
	select {
	case <-s.done:
		return 0, nil, s.why()
	default:
	}
	select {
	case kp := <-s.out:
		return kp.kind, kp.p, nil
	case <-s.done:
		return 0, nil, s.why()
	}
}

// Input takes the player's input messages (see gamestream.Session.input):
// the first controller's buttons and sticks, sent at once.
func (s *Stream) Input(b []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	changed := s.pad.apply(b)
	if !s.inputOn {
		return
	}
	if !s.padOn {
		s.addPad()
	}
	if changed {
		s.sendReport()
	}
}

// Keyframe asks the console for a whole picture, for a player who has
// just joined or lost some of it.
func (s *Stream) Keyframe() {
	s.mu.Lock()
	if time.Since(s.lastKeyReq) < 500*time.Millisecond {
		s.mu.Unlock()
		return
	}
	s.lastKeyReq = time.Now()
	ssrc := s.videoSSRC
	s.mu.Unlock()
	if s.control != nil && s.control.ReadyState() == webrtc.DataChannelStateOpen {
		s.send(s.control, map[string]any{"message": "videoKeyframeRequested", "ifrRequested": true})
	}
	if ssrc != 0 {
		s.pc.WriteRTCP([]rtcp.Packet{&rtcp.PictureLossIndication{MediaSSRC: ssrc}})
	}
}

func (s *Stream) fail(err error) {
	s.errMu.Lock()
	if s.err == nil {
		s.err = err
	}
	s.errMu.Unlock()
	s.closeOnce.Do(func() {
		close(s.done)
		go s.teardown()
	})
}

func (s *Stream) why() error {
	s.errMu.Lock()
	defer s.errMu.Unlock()
	if s.err == nil {
		return io.EOF
	}
	return s.err
}

func (s *Stream) teardown() {
	if s.pc != nil {
		s.pc.Close()
	}
	s.sess.stop()
}

// Close ends remote play.
func (s *Stream) Close() { s.fail(io.EOF) }
