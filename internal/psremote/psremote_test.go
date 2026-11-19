package psremote

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// The test binary stands in for matinee-psremote when FAKE_PSREMOTE is set.
func TestMain(m *testing.M) {
	if os.Getenv("FAKE_PSREMOTE") != "" {
		os.Exit(fakeBridge(os.Args[1:]))
	}
	os.Exit(m.Run())
}

func msg(typ byte, b []byte) []byte {
	h := make([]byte, 8)
	h[0] = typ
	binary.LittleEndian.PutUint32(h[4:], uint32(len(b)))
	return append(h, b...)
}

var (
	sps = []byte{0x67, 0x64, 0x00, 0x28}
	pps = []byte{0x68, 0xee, 0x3c, 0x80}
	idr = []byte{0x65, 0x88, 0x84, 0x00}
	pfr = []byte{0x41, 0x9a, 0x02}
)

func annexB(nals ...[]byte) []byte {
	var out []byte
	for _, n := range nals {
		out = append(append(out, 0, 0, 0, 1), n...)
	}
	return out
}

func fakeBridge(args []string) int {
	switch args[0] {
	case "status":
		if args[1] == "192.168.1.9" {
			fmt.Println(`{"error":"no PlayStation answered at 192.168.1.9"}`)
			return 1
		}
		fmt.Println(`{"state":"standby","ps5":true,"name":"PS5-123","app":""}`)
	case "regist":
		if os.Getenv("MATINEE_PS_PIN") != "12345678" || os.Getenv("MATINEE_PS_ACCOUNT") != "0102030405060708" {
			fmt.Println(`{"error":"the console didn't accept the pairing: check the code and the account"}`)
			return 1
		}
		fmt.Println(`{"name":"PS5-123","mac":"aa:bb:cc:dd:ee:ff","target":1000100,"registKey":"3132333461626364000000000000000a","rpKey":"00112233445566778899aabbccddeeff"}`)
	case "wake":
		fmt.Println(`{}`)
	case "play":
		if os.Getenv("MATINEE_PS_RP_KEY") == "" {
			os.Stdout.Write(msg('E', []byte(`{"error":"the console isn't paired"}`)))
			return 1
		}
		os.Stdout.Write(msg('I', []byte(`{"width":1920,"height":1080,"fps":60}`)))
		os.Stdout.Write(msg('V', annexB(sps, pps, idr)))
		os.Stdout.Write(msg('A', []byte{0xfc, 1, 2, 3}))
		os.Stdout.Write(msg('V', annexB(pfr)))
		os.Stdout.Write(msg('V', annexB(idr)))
		// Echo what comes in, as text, so the test sees it.
		h := make([]byte, 8)
		for {
			if _, err := io.ReadFull(os.Stdin, h); err != nil {
				return 0
			}
			b := make([]byte, binary.LittleEndian.Uint32(h[4:]))
			io.ReadFull(os.Stdin, b)
			if h[0] == 'Q' {
				return 0
			}
			os.Stdout.Write(msg('E', []byte(`{"error":"got `+string(h[0])+hex.EncodeToString(b)+`"}`)))
		}
	}
	return 0
}

func fake(t *testing.T) *Bridge {
	t.Setenv("FAKE_PSREMOTE", "1")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return &Bridge{Path: exe}
}

func TestStatusPairWake(t *testing.T) {
	b := fake(t)
	ctx := context.Background()
	st, err := b.Status(ctx, "192.168.1.20")
	if err != nil || st.State != "standby" || !st.PS5 || st.Name != "PS5-123" {
		t.Fatalf("status %+v %v", st, err)
	}
	if _, err := b.Status(ctx, "192.168.1.9"); !errors.Is(err, ErrNoAnswer) {
		t.Fatalf("no answer: %v", err)
	}
	if _, err := b.Pair(ctx, "192.168.1.20", true, "0102030405060708", "00000000"); err == nil || !strings.Contains(err.Error(), "check the code") {
		t.Fatalf("bad pin: %v", err)
	}
	c, err := b.Pair(ctx, "192.168.1.20", true, "0102030405060708", " 12345678 ")
	if err != nil || c.Name != "PS5-123" || c.RegistKey != "3132333461626364000000000000000a" || c.RPKey == "" || !c.PS5 {
		t.Fatalf("pair %+v %v", c, err)
	}
	if err := b.Wake(ctx, c); err != nil {
		t.Fatal(err)
	}
	if (&Bridge{Path: "/nonexistent"}).Available() {
		t.Fatal("missing bridge available")
	}
}

func TestPlay(t *testing.T) {
	b := fake(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c := Console{Host: "192.168.1.20", PS5: true, RegistKey: "00", RPKey: "11"}
	s, err := b.Play(ctx, c, "0102030405060708", 1080, 60, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.Hello.Width != 1920 || s.Hello.FPS != 60 {
		t.Fatalf("hello %+v", s.Hello)
	}
	// The first keyframe's SPS, PPS and IDR, each a packet of its own;
	// then sound; then the P frame; then the second keyframe, given the
	// SPS and PPS it came without.
	var kinds []byte
	var nalTypes []byte
	for range 8 {
		k, p, err := s.Next()
		if err != nil {
			t.Fatal(err)
		}
		kinds = append(kinds, k)
		if k == KindVideo {
			nalTypes = append(nalTypes, p.Payload[0]&0x1f)
		} else if p.Timestamp != 0 {
			t.Fatalf("first sound timestamp %d", p.Timestamp)
		}
	}
	if want := []byte{7, 8, 5, 1, 7, 8, 5}; !bytes.Equal(nalTypes, want) {
		t.Fatalf("NAL units %v, want %v", nalTypes, want)
	}
	if kinds[3] != KindAudio {
		t.Fatalf("kinds %x", kinds)
	}
	// The controller goes to the bridge: Cross held.
	s.Input([]byte{1, 0, 1, 0})
	_, _, err = s.Next()
	if err == nil || !strings.Contains(err.Error(), "got C01000000") {
		t.Fatalf("input: %v", err)
	}
}

func TestPlayError(t *testing.T) {
	b := fake(t)
	_, err := b.Play(context.Background(), Console{Host: "192.168.1.20"}, "0102030405060708", 1080, 60, nil)
	if err == nil || !strings.Contains(err.Error(), "isn't paired") {
		t.Fatalf("err %v", err)
	}
}

func TestPad(t *testing.T) {
	var p pad
	if _, ok := p.apply([]byte{1, 1, 1, 0}); ok {
		t.Fatal("second controller used")
	}
	st, ok := p.apply([]byte{1, 0, 1<<rSelect | 1<<rStart | 1<<rB, 1 << (rL2 - 8)})
	if !ok {
		t.Fatal("no state")
	}
	if got := binary.LittleEndian.Uint32(st); got != psHome|psCross {
		t.Fatalf("buttons %b", got)
	}
	if st[4] != 0xff || st[5] != 0 {
		t.Fatalf("triggers %v", st[4:6])
	}
	if _, ok := p.apply([]byte{1, 0, 1<<rSelect | 1<<rStart | 1<<rB, 1 << (rL2 - 8)}); ok {
		t.Fatal("unchanged state sent")
	}
	st, _ = p.apply([]byte{3, 0, 1, 0x80})
	if y := int16(binary.LittleEndian.Uint16(st[8:])); y != -32768 {
		t.Fatalf("left y %d", y)
	}
	st, _ = p.apply([]byte{3, 0, 2, 127})
	if x := int16(binary.LittleEndian.Uint16(st[10:])); x != 32767 {
		t.Fatalf("right x %d", x)
	}
	st, _ = p.apply([]byte{1, 0, 1 << rSelect, 0})
	if got := binary.LittleEndian.Uint32(st); got != psTouchpad {
		t.Fatalf("select %b", got)
	}
}

func TestOpusSamples(t *testing.T) {
	for _, c := range []struct {
		p    []byte
		want int
	}{
		{[]byte{0xfc}, 960},        // CELT 20 ms
		{[]byte{0xf0}, 480},        // CELT 10 ms
		{[]byte{0xf1}, 960},        // two 10 ms frames
		{[]byte{0xf3, 0x03}, 1440}, // three
		{[]byte{0x00}, 480},        // SILK 10 ms
		{nil, 480},
	} {
		if got := opusSamples(c.p); got != c.want {
			t.Errorf("%x: %d, want %d", c.p, got, c.want)
		}
	}
}

func TestAccountID(t *testing.T) {
	if got, err := AccountID("AQIDBAUGBwg="); err != nil || got != "0102030405060708" {
		t.Fatalf("base64: %q %v", got, err)
	}
	if got, err := AccountID("0102030405060708"); err != nil || got != "0102030405060708" {
		t.Fatalf("hex: %q %v", got, err)
	}
	if _, err := AccountID("nope"); err == nil {
		t.Fatal("bad id taken")
	}
	if got := AccountIDFromUserID(0x0807060504030201); got != "0102030405060708" {
		t.Fatalf("user id: %s", got)
	}
}

func TestCheckHost(t *testing.T) {
	for host, ok := range map[string]bool{"192.168.1.20": true, "10.0.0.5": true, "fe80::1": true, "8.8.8.8": false, "ps5.local": false} {
		if err := CheckHost(host); (err == nil) != ok {
			t.Errorf("%s: %v", host, err)
		}
	}
}

func TestSignIn(t *testing.T) {
	if _, err := CodeFromRedirect("https://remoteplay.dl.playstation.net/remoteplay/redirect?nope=1"); err == nil {
		t.Fatal("no code taken")
	}
	code, err := CodeFromRedirect(" https://remoteplay.dl.playstation.net/remoteplay/redirect?code=abc&cid=1 ")
	if err != nil || code != "abc" {
		t.Fatalf("code %q %v", code, err)
	}
	if u := SignInURL(); !strings.Contains(u, "client_id="+psnClientID) || !strings.Contains(u, "redirect_uri=") {
		t.Fatalf("url %s", u)
	}
	// Sony's two answers, from a stand-in.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u, p, _ := r.BasicAuth(); u != psnClientID || p != psnClientSecret {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.Method == http.MethodPost {
			r.ParseForm()
			if r.Form.Get("code") != "abc" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			io.WriteString(w, `{"access_token":"tok"}`)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/tok") {
			io.WriteString(w, `{"user_id":"578437695752307201"}`)
		}
	}))
	defer srv.Close()
	client := srv.Client()
	client.Transport = rewrite{srv.URL, client.Transport}
	id, err := SignIn(context.Background(), client, "abc")
	if err != nil || id != AccountIDFromUserID(578437695752307201) {
		t.Fatalf("id %q %v", id, err)
	}
	if _, err := SignIn(context.Background(), client, "old"); err == nil || !strings.Contains(err.Error(), "sign in again") {
		t.Fatalf("old code: %v", err)
	}
}

// rewrite sends every request to the stand-in.
type rewrite struct {
	to string
	rt http.RoundTripper
}

func (r rewrite) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.URL.Scheme = "http"
	req.URL.Host = strings.TrimPrefix(r.to, "http://")
	return r.rt.RoundTrip(req)
}
