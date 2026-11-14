package gamehost

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/pion/rtp"
)

func TestParseVDF(t *testing.T) {
	kv, err := ParseVDF(strings.NewReader(`// a comment
"libraryfolders"
{
	"0"
	{
		"path"		"/home/default/.local/share/Steam"
		"apps" { "228980" "1234" }
	}
	"1"
	{
		"path"		"/mnt/games/Steam \"Library\""
	}
}`))
	if err != nil {
		t.Fatal(err)
	}
	libs := kv.Child("LibraryFolders").Children()
	if len(libs) != 2 || libs[1].Value.Get("PATH") != `/mnt/games/Steam "Library"` || libs[0].Value.Child("apps").Get("228980") != "1234" {
		t.Fatalf("parsed %+v", libs)
	}
	for _, bad := range []string{`"a" {`, `}`, `"a"`, `"a" "b`} {
		if _, err := ParseVDF(strings.NewReader(bad)); err == nil {
			t.Errorf("%q: no error", bad)
		}
	}
}

func writeManifest(t *testing.T, dir string, id int, name string, flags int) {
	t.Helper()
	os.MkdirAll(dir, 0o755)
	body := "\"AppState\"\n{\n\t\"appid\"\t\t\"" + itoa(id) + "\"\n\t\"name\"\t\t\"" + name + "\"\n\t\"StateFlags\"\t\t\"" + itoa(flags) + "\"\n\t\"SizeOnDisk\"\t\t\"1000\"\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "appmanifest_"+itoa(id)+".acf"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func itoa(n int) string { return fmtInt(n) }

func fmtInt(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for ; n > 0; n /= 10 {
		b = append([]byte{byte('0' + n%10)}, b...)
	}
	return string(b)
}

func TestInstalled(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".local/share/Steam")
	other := filepath.Join(home, "games")
	apps := filepath.Join(root, "steamapps")
	writeManifest(t, apps, 1677740, "Tape to Tape", 4)
	writeManifest(t, apps, 1493710, "Proton Experimental", 4)
	writeManifest(t, apps, 228980, "Steamworks Common Redistributables", 4)
	writeManifest(t, apps, 1145360, "Hades", 1030) // being updated, still installed
	writeManifest(t, filepath.Join(other, "steamapps"), 504230, "Celeste", 4)
	writeManifest(t, filepath.Join(other, "steamapps"), 413150, "Stardew Valley", 2) // still downloading
	os.WriteFile(filepath.Join(apps, "libraryfolders.vdf"), []byte(`"libraryfolders" { "0" { "path" "`+root+`" } "1" { "path" "`+other+`" } }`), 0o644)

	got, err := SteamRoot(home)
	if err != nil || got != root {
		t.Fatalf("SteamRoot = %q, %v", got, err)
	}
	games, err := Installed(root)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, g := range games {
		names = append(names, g.Name)
	}
	if strings.Join(names, ", ") != "Celeste, Hades, Tape to Tape" || games[2].AppID != 1677740 || games[2].Size != 1000 {
		t.Fatalf("games: %+v", games)
	}
}

func TestPadEvents(t *testing.T) {
	var p padState
	evs := p.setButtons(1<<rB | 1<<rUp | 1<<rR2)
	want := []event{{evKey, btnSouth, 1}, {evAbs, absRZ, 255}, {evAbs, absHat0Y, -1}, {evSyn, 0, 0}}
	if !eq(evs, want) {
		t.Fatalf("press: %v", evs)
	}
	if evs := p.setButtons(1<<rB | 1<<rUp | 1<<rR2); evs != nil {
		t.Fatalf("no change: %v", evs)
	}
	evs = p.setButtons(1 << rA)
	want = []event{{evKey, btnSouth, 0}, {evKey, btnEast, 1}, {evAbs, absRZ, 0}, {evAbs, absHat0Y, 0}, {evSyn, 0, 0}}
	if !eq(evs, want) {
		t.Fatalf("change: %v", evs)
	}
	evs = p.setButtons(1<<rSelect | 1<<rStart)
	want = []event{{evKey, btnSelect, 1}, {evKey, btnStart, 1}, {evKey, btnEast, 0}, {evKey, btnMode, 1}, {evSyn, 0, 0}}
	if !eq(evs, want) {
		t.Fatalf("guide: %v", evs)
	}
	if evs := p.setStick(1, -128); !eq(evs, []event{{evAbs, absY, -32768}, {evSyn, 0, 0}}) {
		t.Fatalf("stick up: %v", evs)
	}
	if evs := p.setStick(2, 127); !eq(evs, []event{{evAbs, absRX, 32767}, {evSyn, 0, 0}}) {
		t.Fatalf("stick right: %v", evs)
	}
	if p.setStick(2, 127) != nil || p.setStick(4, 1) != nil {
		t.Fatal("no change, or no such axis")
	}
}

func eq(a, b []event) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// A game plays end to end, with FFmpeg's test picture and tone standing in
// for the display and Steam's sound.
func TestPlay(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("no ffmpeg")
	}
	home := t.TempDir()
	root := filepath.Join(home, ".steam/steam")
	writeManifest(t, filepath.Join(root, "steamapps"), 1677740, "Tape to Tape", 4)
	h := &Host{SteamRoot: root, Token: "secret", FFmpeg: ffmpeg, Steam: "true",
		VideoInput: []string{"-re", "-f", "lavfi", "-i", "testsrc2=size=1280x720:rate=30"},
		AudioInput: []string{"-re", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000"}}
	srv := httptest.NewServer(h.Handler())
	defer srv.Close()
	ctx := context.Background()

	if _, err := (&Client{URL: srv.URL, Token: "wrong"}).Games(ctx); err == nil {
		t.Fatal("wrong token let in")
	}
	c := &Client{URL: srv.URL, Token: "secret"}
	games, err := c.Games(ctx)
	if err != nil || len(games) != 1 || games[0].Name != "Tape to Tape" {
		t.Fatalf("games: %+v, %v", games, err)
	}
	conn, hello, err := c.Play(ctx, 1677740, 1280, 720, 30)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	if hello.Width != 1280 || hello.Height != 720 || hello.Encoder != "libx264" {
		t.Fatalf("hello: %+v", hello)
	}
	if _, _, err := c.Play(ctx, 1677740, 1280, 720, 30); err != ErrBusy {
		t.Fatalf("second game: %v", err)
	}
	// Buttons go in without trouble even with no uinput here.
	conn.Write(ctx, websocket.MessageBinary, []byte{1, 0, 1, 0})

	rctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	var sps, sound bool
	for !(sps && sound) {
		typ, b, err := conn.Read(rctx)
		if err != nil {
			t.Fatalf("read: %v (sps %v, sound %v)", err, sps, sound)
		}
		if typ != websocket.MessageBinary || len(b) < 2 {
			continue
		}
		var p rtp.Packet
		if p.Unmarshal(b[1:]) != nil {
			t.Fatalf("not RTP: kind %x", b[0])
		}
		switch b[0] {
		case KindVideo:
			if p.PayloadType != 96 {
				t.Fatalf("video payload type %d", p.PayloadType)
			}
			// SPS alone (7) or first in a STAP-A (24).
			if n := p.Payload[0] & 0x1f; n == 7 || n == 24 && p.Payload[3]&0x1f == 7 {
				sps = true
			}
		case KindAudio:
			sound = p.PayloadType == 111
		}
	}
	conn.Close(websocket.StatusNormalClosure, "")
	// The host is free again once the game has ended.
	for i := 0; i < 50; i++ {
		if !h.busy.Load() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("still busy after the game ended")
}

func TestPlayBigPicture(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("no ffmpeg")
	}
	dir := t.TempDir()
	calls := filepath.Join(dir, "calls")
	steam := filepath.Join(dir, "steam")
	os.WriteFile(steam, []byte("#!/bin/sh\necho \"$@\" >> "+calls+"\n"), 0o755)
	h := &Host{SteamRoot: dir, Token: "t", FFmpeg: ffmpeg, Steam: steam,
		VideoInput: []string{"-re", "-f", "lavfi", "-i", "testsrc2=size=640x360:rate=30"},
		AudioInput: []string{"-re", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000"}}
	srv := httptest.NewServer(h.Handler())
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	conn, _, err := (&Client{URL: srv.URL, Token: "t"}).Play(ctx, BigPicture, 640, 360, 30)
	if err != nil {
		t.Fatal(err)
	}
	// Still playing after a few seconds with no game running: Big Picture
	// lasts until the player leaves.
	deadline := time.Now().Add(7 * time.Second)
	for time.Now().Before(deadline) {
		if _, _, err := conn.Read(ctx); err != nil {
			t.Fatalf("stream ended: %v", err)
		}
	}
	conn.Close(websocket.StatusNormalClosure, "")
	for i := 0; i < 50 && h.busy.Load(); i++ {
		time.Sleep(100 * time.Millisecond)
	}
	b, _ := os.ReadFile(calls)
	if got := string(b); got != "steam://open/bigpicture\nsteam://close/bigpicture\n" {
		t.Fatalf("steam was run with %q", got)
	}
}

func TestDesktop(t *testing.T) {
	// The desktop: a page, and a socket that echoes.
	desk := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
			t.Errorf("credentials reached the desktop: %v", r.Header)
		}
		if r.URL.Path == "/websockify" {
			c, err := websocket.Accept(w, r, nil)
			if err != nil {
				return
			}
			defer c.CloseNow()
			typ, b, err := c.Read(r.Context())
			if err == nil {
				c.Write(r.Context(), typ, b)
			}
			return
		}
		w.Write([]byte("page " + r.URL.Path + "?" + r.URL.RawQuery))
	}))
	defer desk.Close()
	host := httptest.NewServer((&Host{Token: "t", Desktop: desk.URL}).Handler())
	defer host.Close()
	c := &Client{URL: host.URL, Token: "t"}
	// Matinee's side, passing on what comes after /desktop/.
	matinee := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Desktop(strings.TrimPrefix(r.URL.Path, "/desktop/")).ServeHTTP(w, r)
	}))
	defer matinee.Close()

	if resp, err := http.Get(host.URL + DesktopPath + "web/index.html"); err != nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("desktop without the token: %v %v", resp, err)
	}
	req, _ := http.NewRequest("GET", matinee.URL+"/desktop/web/index.html?autoconnect=true", nil)
	req.Header.Set("Cookie", "matinee_session=secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(b) != "page /web/index.html?autoconnect=true" {
		t.Fatalf("page: %d %q", resp.StatusCode, b)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ws, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(matinee.URL, "http")+"/desktop/websockify", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.CloseNow()
	ws.Write(ctx, websocket.MessageBinary, []byte("hi"))
	if _, b, err := ws.Read(ctx); err != nil || string(b) != "hi" {
		t.Fatalf("socket: %q %v", b, err)
	}

	// No desktop: 404.
	none := httptest.NewServer((&Host{}).Handler())
	defer none.Close()
	if resp, _ := http.Get(none.URL + DesktopPath); resp == nil || resp.StatusCode != http.StatusNotFound {
		t.Fatal("desktop without one")
	}
}

func TestKeyboardMouseEvents(t *testing.T) {
	held := map[uint16]bool{}
	k, evs := kbmEvents([]byte{4, 30, 0, 1}, held) // KEY_A down
	if k != kbmKeys || len(evs) != 2 || evs[0] != (event{evKey, 30, 1}) {
		t.Fatalf("key down: %v %v", k, evs)
	}
	if _, evs := kbmEvents([]byte{4, 30, 0, 1}, held); evs != nil {
		t.Errorf("a key held isn't pressed again: %v", evs)
	}
	if _, evs := kbmEvents([]byte{4, 0, 1, 1}, held); evs != nil {
		t.Errorf("code 256 is past the keyboard: %v", evs)
	}
	k, evs = kbmEvents([]byte{5, 0xff, 0xff, 0x00, 0x80}, held)
	if k != kbmPointer || evs[0] != (event{evAbs, absX, 65535}) || evs[1] != (event{evAbs, absY, 32768}) {
		t.Errorf("pointer: %v %v", k, evs)
	}
	k, evs = kbmEvents([]byte{6, 0xfb, 0xff, 3, 0}, held) // dx -5, dy 3
	if k != kbmKeys || evs[0] != (event{evRel, relX, -5}) || evs[1] != (event{evRel, relY, 3}) {
		t.Errorf("move: %v %v", k, evs)
	}
	k, evs = kbmEvents([]byte{7, 2, 1}, held)
	if k != kbmKeys || evs[0] != (event{evKey, btnRight, 1}) {
		t.Errorf("right button: %v %v", k, evs)
	}
	if _, evs := kbmEvents([]byte{7, 9, 1}, held); evs != nil {
		t.Errorf("unknown button: %v", evs)
	}
	_, evs = kbmEvents([]byte{8, 0xff, 0}, held)
	if len(evs) != 2 || evs[0] != (event{evRel, relWheel, -1}) {
		t.Errorf("wheel: %v", evs)
	}
	evs = releaseAll(held)
	if len(evs) != 3 || held[30] || held[btnRight] {
		t.Errorf("release all: %v %v", evs, held)
	}
}
