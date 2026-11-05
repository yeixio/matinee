package api

import (
	"bytes"
	"encoding/binary"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/yeixio/matinee/internal/store"
	"github.com/yeixio/matinee/internal/stream"
)

// GET /items/{id}/audio sends a song as an endless 16-bit 44.1 kHz stereo
// WAV, from startMs on, for speakers like Air-Mixer.
func TestItemAudio(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	c, tmp := newTestServer(t)
	var login struct{ Token string }
	c.do("POST", "/api/setup", credentials{"admin", "correct horse"}, &login)
	c.token = login.Token
	makeMedia(t, filepath.Join(tmp, "music", "Artist", "Album", "01 Song.flac"),
		"-f", "lavfi", "-i", "sine=duration=2:sample_rate=48000", "-ac", "1",
		"-metadata", "artist=Artist", "-metadata", "album=Album", "-metadata", "title=Song")
	var lib libraryView
	if code := c.do("POST", "/api/libraries", map[string]any{"name": "Music", "kind": "music", "paths": []string{filepath.Join(tmp, "music")}}, &lib); code != http.StatusCreated {
		t.Fatalf("create library: %d", code)
	}
	var song store.Item
	for deadline := time.Now().Add(30 * time.Second); song.ID == 0 && time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		waitForScan(t, c)
		var artists, albums, tracks []store.Item
		c.do("GET", "/api/libraries/"+itoa(lib.ID)+"/items", nil, &artists)
		if len(artists) == 0 {
			continue
		}
		c.do("GET", "/api/items/"+itoa(artists[0].ID)+"/children", nil, &albums)
		if len(albums) == 0 {
			continue
		}
		c.do("GET", "/api/items/"+itoa(albums[0].ID)+"/children", nil, &tracks)
		if len(tracks) > 0 {
			song = tracks[0]
		}
	}
	if song.ID == 0 {
		t.Fatal("the song wasn't scanned")
	}

	get := func(query string) (*http.Response, []byte) {
		t.Helper()
		res, err := http.Get(c.base + "/api/v1/items/" + itoa(song.ID) + "/audio?token=" + c.token + query)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		return res, body
	}
	res, body := get("")
	if res.StatusCode != http.StatusOK || res.Header.Get("Content-Type") != "audio/wav" {
		t.Fatalf("audio: %d %s", res.StatusCode, res.Header.Get("Content-Type"))
	}
	if len(body) < 44 || string(body[:4]) != "RIFF" || string(body[8:12]) != "WAVE" {
		t.Fatalf("not a WAV: % x", body[:min(len(body), 44)])
	}
	if ch, rate, bits := binary.LittleEndian.Uint16(body[22:]), binary.LittleEndian.Uint32(body[24:]), binary.LittleEndian.Uint16(body[34:]); ch != 2 || rate != 44100 || bits != 16 {
		t.Fatalf("format: %d channels, %d Hz, %d bits", ch, rate, bits)
	}
	// Two seconds of stereo 16-bit audio, give or take a frame.
	full := len(body) - 44
	if want := 2 * 44100 * 4; full < want*9/10 || full > want*11/10 {
		t.Fatalf("audio is %d bytes, want about %d", full, want)
	}

	// From one second in, half as much.
	_, body = get("&startMs=1000")
	if half := len(body) - 44; half < full*4/10 || half > full*6/10 {
		t.Fatalf("from 1s: %d bytes of %d", half, full)
	}
	if res, _ := get("&startMs=soon"); res.StatusCode != http.StatusBadRequest {
		t.Fatalf("a bad startMs: %d", res.StatusCode)
	}
}

// An admin can add a station on the home network, such as an Air-Mixer
// output; nobody else can, and stations without home stay off it. A
// station that streams WAV plays to players as AAC.
func TestHomeNetworkStation(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	stream.AllowHomeNetwork.Store(false)
	wav := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/wav")
		w.Write(stream.WAVHeader(44100, 2))
		w.Write(make([]byte, 44100*4)) // a second of silence, then the "live" stream ends
	}))
	defer wav.Close()

	c, tmp := newTestServer(t)
	var login struct{ Token string }
	c.do("POST", "/api/setup", credentials{"admin", "correct horse"}, &login)
	c.token = login.Token
	os.MkdirAll(filepath.Join(tmp, "music"), 0o755)
	var lib libraryView
	c.do("POST", "/api/v1/libraries", map[string]any{"name": "Music", "kind": "music", "paths": []string{filepath.Join(tmp, "music")}}, &lib)
	waitForScan(t, c)

	// 127.0.0.1 is on this machine, so it's refused without home.
	if code := c.do("POST", "/api/v1/stations", map[string]any{"libraryId": lib.ID, "url": wav.URL + "/kitchen"}, nil); code != http.StatusBadGateway {
		t.Fatalf("a home address without home: %d", code)
	}
	var st store.Item
	if code := c.do("POST", "/api/v1/stations", map[string]any{"libraryId": lib.ID, "url": wav.URL + "/kitchen", "name": "Kitchen", "home": true}, &st); code != http.StatusCreated {
		t.Fatalf("an admin's home station: %d", code)
	}

	res, err := http.Get(c.base + "/api/v1/items/" + itoa(st.ID) + "/file?token=" + c.token)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.Header.Get("Content-Type") != "audio/aac" || len(body) < 2 || body[0] != 0xFF || body[1]&0xF0 != 0xF0 {
		t.Fatalf("relayed WAV: %s, %d bytes, starts % x", res.Header.Get("Content-Type"), len(body), body[:min(len(body), 4)])
	}

	// And as PCM for speakers, the WAV as it is.
	res, err = http.Get(c.base + "/api/v1/items/" + itoa(st.ID) + "/audio?token=" + c.token)
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(res.Body)
	res.Body.Close()
	if !bytes.HasPrefix(body, []byte("RIFF")) || len(body) < 44100*4*9/10 {
		t.Fatalf("station as PCM: %d bytes", len(body))
	}
}

// Only admins add stations on the home network.
func TestHomeNetworkStationAdminsOnly(t *testing.T) {
	c, tmp := newTestServer(t)
	var login struct{ Token string }
	c.do("POST", "/api/setup", credentials{"admin", "correct horse"}, &login)
	c.token = login.Token
	os.MkdirAll(filepath.Join(tmp, "music"), 0o755)
	var lib libraryView
	c.do("POST", "/api/v1/libraries", map[string]any{"name": "Music", "kind": "music", "paths": []string{filepath.Join(tmp, "music")}}, &lib)
	waitForScan(t, c)
	if code := c.do("POST", "/api/v1/users", map[string]any{"username": "sam", "password": "another horse"}, nil); code != http.StatusCreated {
		t.Fatalf("add user: %d", code)
	}
	var sam struct{ Token string }
	c.token = ""
	c.do("POST", "/api/auth/login", credentials{"sam", "another horse"}, &sam)
	c.token = sam.Token
	if code := c.do("POST", "/api/v1/stations", map[string]any{"libraryId": lib.ID, "url": "http://192.168.1.20:9001/api/clients/x/stream", "home": true}, nil); code != http.StatusForbidden {
		t.Fatalf("someone else's home station: %d", code)
	}
}
