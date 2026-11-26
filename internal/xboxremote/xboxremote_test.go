package xboxremote

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
)

// fakeXbox is Microsoft's servers and a console behind them.
type fakeXbox struct {
	t   *testing.T
	srv *httptest.Server

	mu        sync.Mutex
	polls     int
	refreshes []string
	state     string
	notReady  int // starts that fail until the console checks in
	plays     int
	connected string // the transfer token the console got
	answer    string
	deleted   bool
	console   *webrtc.PeerConnection
	inputs    [][]byte
	controls  []string
}

func newFakeXbox(t *testing.T) *fakeXbox {
	f := &fakeXbox{t: t, state: "Provisioning"}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(func() {
		f.srv.Close()
		f.mu.Lock()
		if f.console != nil {
			f.console.Close()
		}
		f.mu.Unlock()
	})
	return f
}

func (f *fakeXbox) client() *Client {
	u := f.srv.URL
	return &Client{End: Endpoints{Login: u, Live: u + "/live", UserAuth: u, XSTS: u, XHome: u}}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func (f *fakeXbox) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	body, _ := io.ReadAll(r.Body)
	switch p := r.URL.Path; {
	case p == "/consumers/oauth2/v2.0/devicecode":
		writeJSON(w, map[string]any{"user_code": "ABCD1234", "device_code": "dev", "verification_uri": "https://www.microsoft.com/link", "expires_in": 900, "interval": 0})
	case p == "/consumers/oauth2/v2.0/token":
		r.Form, _ = parseForm(body)
		switch r.Form.Get("grant_type") {
		case "urn:ietf:params:oauth:grant-type:device_code":
			f.polls++
			if f.polls < 2 {
				w.WriteHeader(400)
				writeJSON(w, map[string]string{"error": "authorization_pending"})
				return
			}
			writeJSON(w, map[string]any{"access_token": "a0", "refresh_token": "r1", "expires_in": 3600})
		case "refresh_token":
			f.refreshes = append(f.refreshes, r.Form.Get("refresh_token"))
			if r.Form.Get("refresh_token") == "gone" {
				w.WriteHeader(400)
				writeJSON(w, map[string]string{"error": "invalid_grant"})
				return
			}
			writeJSON(w, map[string]any{"access_token": "a1", "refresh_token": "r2", "expires_in": 3600})
		}
	case p == "/live/oauth20_token.srf":
		r.Form, _ = parseForm(body)
		if !strings.Contains(r.Form.Get("scope"), "XBOX_CLOUD_CONSOLE_TRANSFER_TOKEN") {
			w.WriteHeader(400)
			return
		}
		writeJSON(w, map[string]any{"access_token": "transfer", "refresh_token": "ignored"})
	case p == "/user/authenticate":
		if !strings.Contains(string(body), `"d=a1"`) {
			w.WriteHeader(401)
			return
		}
		writeJSON(w, map[string]any{"Token": "user"})
	case p == "/xsts/authorize":
		if strings.Contains(string(body), "gssv") {
			writeJSON(w, map[string]any{"Token": "gssv"})
		} else {
			writeJSON(w, map[string]any{"Token": "live", "DisplayClaims": map[string]any{"xui": []any{map[string]string{"gtg": "Mike Plays"}}}})
		}
	case p == "/v2/login/user":
		if !strings.Contains(string(body), `"xhome"`) || !strings.Contains(string(body), `"gssv"`) {
			w.WriteHeader(400)
			return
		}
		writeJSON(w, map[string]any{"gsToken": "gs", "durationInSeconds": 14400, "offeringSettings": map[string]any{
			"regions": []any{map[string]any{"baseUri": "https://elsewhere.invalid", "isDefault": false}, map[string]any{"baseUri": f.srv.URL, "isDefault": true}}}})
	case r.Header.Get("Authorization") != "Bearer gs":
		w.WriteHeader(401)
	case p == "/v6/servers/home":
		writeJSON(w, map[string]any{"results": []any{
			map[string]string{"deviceName": "Living Room", "serverId": "F4001", "powerState": "ConnectedStandby", "consoleType": "XboxSeriesX"},
			map[string]string{"deviceName": "", "serverId": "F4002", "powerState": "Off", "consoleType": "XboxOne"},
		}})
	case p == "/v5/sessions/home/play":
		if !strings.Contains(string(body), `"serverId":"F4001"`) {
			w.WriteHeader(404)
			return
		}
		f.plays++
		f.state = "Provisioning"
		if f.notReady > 0 {
			f.notReady--
			f.state = "NotRegistered"
		}
		writeJSON(w, map[string]string{"sessionId": "s1", "sessionPath": "v5/sessions/home/s1", "state": "Provisioning"})
	case p == "/v5/sessions/home/s1/state":
		if f.state == "NotRegistered" {
			writeJSON(w, map[string]any{"state": "Failed", "errorDetails": map[string]string{"code": "StartSessionFailed",
				"message": "Xccs : ErrorCallingWNS : Send command failed : State WaitingForServerToRegister"}})
			return
		}
		if f.state == "Provisioning" {
			f.state = "ReadyToConnect"
		}
		writeJSON(w, map[string]string{"state": f.state})
	case p == "/v5/sessions/home/s1/connect":
		var b struct{ UserToken string }
		json.Unmarshal(body, &b)
		f.connected = b.UserToken
		f.state = "Provisioned"
	case p == "/v5/sessions/home/s1/sdp" && r.Method == "POST":
		var b struct {
			SDP string `json:"sdp"`
		}
		json.Unmarshal(body, &b)
		go f.startConsole(b.SDP)
	case p == "/v5/sessions/home/s1/sdp":
		if f.answer == "" {
			w.WriteHeader(204)
			return
		}
		ex, _ := json.Marshal(map[string]string{"messageType": "answer", "sdp": f.answer, "status": "success"})
		writeJSON(w, map[string]string{"exchangeResponse": string(ex)})
	case p == "/v5/sessions/home/s1/ice" && r.Method == "POST":
	case p == "/v5/sessions/home/s1/ice":
		ex, _ := json.Marshal([]map[string]any{{"candidate": "a=end-of-candidates", "sdpMid": "0", "sdpMLineIndex": "0"}})
		writeJSON(w, map[string]string{"exchangeResponse": string(ex)})
	case p == "/v5/sessions/home/s1/keepalive":
	case p == "/v5/sessions/home/s1" && r.Method == "DELETE":
		f.deleted = true
	default:
		f.t.Errorf("unexpected %s %s", r.Method, p)
		w.WriteHeader(404)
	}
}

func parseForm(b []byte) (map[string][]string, error) {
	req, _ := http.NewRequest("POST", "/", strings.NewReader(string(b)))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	err := req.ParseForm()
	return req.PostForm, err
}

// startConsole answers the offer as a console would, and sends a picture
// and sound once connected.
func (f *fakeXbox) startConsole(offer string) {
	t := f.t
	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Error(err)
		return
	}
	video, _ := webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264, ClockRate: 90000,
		SDPFmtpLine: "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42e01f"}, "video", "xbox")
	audio, _ := webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2}, "audio", "xbox")
	pc.OnDataChannel(func(dc *webrtc.DataChannel) {
		dc.OnMessage(func(m webrtc.DataChannelMessage) {
			f.mu.Lock()
			defer f.mu.Unlock()
			switch dc.Label() {
			case "message":
				var msg message
				json.Unmarshal(m.Data, &msg)
				if msg.Type == "Handshake" {
					dc.SendText(`{"type":"HandshakeAck","version":"messageV1","id":"x"}`)
				}
			case "input":
				f.inputs = append(f.inputs, m.Data)
			case "control":
				f.controls = append(f.controls, string(m.Data))
			}
		})
	})
	if err := pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: offer}); err != nil {
		t.Error(err)
		return
	}
	for _, tr := range []webrtc.TrackLocal{video, audio} {
		if _, err := pc.AddTrack(tr); err != nil {
			t.Error(err)
			return
		}
	}
	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		t.Error(err)
		return
	}
	gathered := webrtc.GatheringCompletePromise(pc)
	pc.SetLocalDescription(answer)
	<-gathered
	f.mu.Lock()
	f.console = pc
	f.answer = pc.LocalDescription().SDP
	f.mu.Unlock()

	idr := []byte{0, 0, 0, 1, 0x67, 0x42, 0xe0, 0x1f, 0, 0, 0, 1, 0x68, 0xce, 0x38, 0x80, 0, 0, 0, 1, 0x65, 0x88, 0x84, 0x00, 0x10}
	frame := []byte{0, 0, 0, 1, 0x41, 0x9a, 0x02, 0x03}
	go func() {
		for i := 0; ; i++ {
			if pc.ConnectionState() == webrtc.PeerConnectionStateClosed {
				return
			}
			au := frame
			if i%30 == 0 {
				au = idr
			}
			video.WriteSample(media.Sample{Data: au, Duration: 16 * time.Millisecond})
			audio.WriteSample(media.Sample{Data: []byte{0xfc, 0xff, 0xfe}, Duration: 20 * time.Millisecond})
			time.Sleep(16 * time.Millisecond)
		}
	}()
}

func TestSignIn(t *testing.T) {
	f := newFakeXbox(t)
	c := f.client()
	ctx := context.Background()
	d, err := c.StartSignIn(ctx)
	if err != nil || d.Code != "ABCD1234" || d.URL != "https://www.microsoft.com/link" {
		t.Fatalf("StartSignIn = %+v, %v", d, err)
	}
	d.interval = time.Millisecond
	refresh, err := c.WaitSignIn(ctx, d)
	if err != nil || refresh != "r1" {
		t.Fatalf("WaitSignIn = %q, %v", refresh, err)
	}
	tok, err := c.Tokens(ctx, refresh)
	if err != nil || tok.Refresh != "r2" || tok.Gamertag != "Mike Plays" || tok.GS != "gs" || tok.Base != f.srv.URL {
		t.Fatalf("Tokens = %+v, %v", tok, err)
	}
	// Kept until it nears its end.
	if again, _ := c.Tokens(ctx, "r2"); again != tok || len(f.refreshes) != 1 {
		t.Fatalf("Tokens again signed in again: %v", f.refreshes)
	}
	if _, err := c.Tokens(ctx, "gone"); err != ErrSignedOut {
		t.Fatalf("ended sign-in: %v", err)
	}
	consoles, err := c.Consoles(ctx, tok)
	if err != nil || len(consoles) != 2 {
		t.Fatalf("Consoles = %v, %v", consoles, err)
	}
	if c := consoles[0]; c.ID != "F4001" || c.Name != "Living Room" || c.State != "standby" || c.Type != "XboxSeriesX" {
		t.Errorf("console = %+v", c)
	}
	if c := consoles[1]; c.Name != "Xbox" || c.State != "off" {
		t.Errorf("console = %+v", c)
	}
}

func TestPlay(t *testing.T) {
	f := newFakeXbox(t)
	c := f.client()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	tok, err := c.Tokens(ctx, "r1")
	if err != nil {
		t.Fatal(err)
	}
	st, err := c.Play(ctx, tok, "F4001", nil)
	if err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	if f.connected != "transfer" {
		t.Errorf("the console was signed in with %q", f.connected)
	}
	f.mu.Unlock()

	var video, audio int
	for video < 20 || audio < 5 {
		kind, p, err := st.Next()
		if err != nil {
			t.Fatal(err)
		}
		switch kind {
		case KindVideo:
			if video == 0 && p.Payload[0]&0x1f != 7 && p.Payload[0]&0x1f != 28 {
				t.Errorf("the first picture packet isn't the start of a keyframe: % x", p.Payload[:4])
			}
			video++
		case KindAudio:
			audio++
		}
	}

	// A, then the left stick pushed up.
	st.Input([]byte{1, 0, 1 << rB, 0})
	st.Input([]byte{3, 0, 1, 0x81})
	var gotA, gotUp bool
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && !(gotA && gotUp) {
		time.Sleep(20 * time.Millisecond)
		f.mu.Lock()
		for _, b := range f.inputs {
			if len(b) < 14 || binary.LittleEndian.Uint16(b)&reportGamepad == 0 {
				continue
			}
			o := 14
			if binary.LittleEndian.Uint16(b)&reportMetadata != 0 {
				o += 1 + 28*int(b[14])
			}
			g := b[o+1:]
			if binary.LittleEndian.Uint16(g[1:])&xA != 0 {
				gotA = true
				if int16(binary.LittleEndian.Uint16(g[5:])) == 32767 {
					gotUp = true
				}
			}
		}
		controls := strings.Join(f.controls, "\n")
		f.mu.Unlock()
		if !strings.Contains(controls, "authorizationRequest") || !strings.Contains(controls, "gamepadChanged") {
			t.Fatalf("the console wasn't told about the controller: %s", controls)
		}
	}
	if !gotA || !gotUp {
		t.Fatalf("the console didn't get the controller (A %v, up %v)", gotA, gotUp)
	}
	f.mu.Lock()
	first := f.inputs[0]
	f.mu.Unlock()
	if binary.LittleEndian.Uint16(first) != reportClientMetadata || len(first) != 15 {
		t.Errorf("first input report = % x", first)
	}

	st.Close()
	if _, _, err := st.Next(); err != io.EOF {
		t.Errorf("Next after Close = %v", err)
	}
	deadline = time.Now().Add(5 * time.Second)
	for {
		f.mu.Lock()
		deleted := f.deleted
		f.mu.Unlock()
		if deleted {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the session wasn't ended")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// A console that hasn't checked in with Microsoft yet is asked again, and
// one that never does says what to do.
func TestPlayNotRegistered(t *testing.T) {
	defer func(w time.Duration) { registerWait = w }(registerWait)
	registerWait = 10 * time.Millisecond
	f := newFakeXbox(t)
	c := f.client()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	tok, err := c.Tokens(ctx, "r1")
	if err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.notReady = 2
	f.mu.Unlock()
	st, err := c.Play(ctx, tok, "F4001", nil)
	if err != nil {
		t.Fatal(err)
	}
	st.Close()
	f.mu.Lock()
	if f.plays != 3 {
		t.Errorf("asked %d times, want 3", f.plays)
	}
	f.notReady, f.plays = 100, 0
	f.mu.Unlock()
	if _, err := c.Play(ctx, tok, "F4001", nil); err == nil || !strings.Contains(err.Error(), "Restart the Xbox") {
		t.Fatalf("never registered: %v", err)
	}
	f.mu.Lock()
	if f.plays != 4 {
		t.Errorf("asked %d times, want 4", f.plays)
	}
	f.mu.Unlock()
}

func TestTeredo(t *testing.T) {
	// 2001:0:4136:e378:8000:63bf:3fff:fdd2 is 192.0.2.45 port 40000, as
	// RFC 4380's example.
	ip, port, ok := teredo("2001:0:4136:e378:8000:63bf:3fff:fdd2")
	if !ok || ip != "192.0.2.45" || port != 40000 {
		t.Errorf("teredo = %s %d %v", ip, port, ok)
	}
	if _, _, ok := teredo("192.168.1.5"); ok {
		t.Error("an IPv4 address isn't Teredo")
	}
	got := candidateInits(remoteCandidate{Candidate: "a=candidate:1 1 UDP 1 2001:0:4136:e378:8000:63bf:3fff:fdd2 9002 typ host", SDPMid: "0", SDPMLineIndex: "0"})
	if len(got) != 3 || !strings.Contains(got[1].Candidate, "192.0.2.45 40000") || strings.HasPrefix(got[2].Candidate, "a=") {
		t.Errorf("candidates = %+v", got)
	}
	if got := candidateInits(remoteCandidate{Candidate: "a=end-of-candidates"}); len(got) != 0 {
		t.Errorf("end of candidates = %+v", got)
	}
}

func TestPad(t *testing.T) {
	var p pad
	if !p.apply([]byte{1, 0, 1<<rSelect | 1<<rStart, 0}) {
		t.Fatal("no change")
	}
	g := p.frame()
	if b := binary.LittleEndian.Uint16(g[1:]); b != xNexus {
		t.Errorf("Select+Start = %#x, want the Xbox button", b)
	}
	p.apply([]byte{1, 0, 0, 1 << (rL2 - 8)})
	p.apply([]byte{3, 0, 0, 0x80}) // left stick all the way left
	g = p.frame()
	if binary.LittleEndian.Uint16(g[11:]) != 0xffff || int16(binary.LittleEndian.Uint16(g[3:])) != -32767 {
		t.Errorf("frame = % x", g)
	}
	if p.apply([]byte{1, 1, 1, 0}) {
		t.Error("the second controller isn't sent on")
	}
	r := report(7, time.Second, []frameTimes{{key: 1, arrived: 2}}, g)
	if len(r) != 14+1+28+1+23 || binary.LittleEndian.Uint16(r) != reportMetadata|reportGamepad || binary.LittleEndian.Uint32(r[2:]) != 7 {
		t.Errorf("report = % x", r)
	}
}
