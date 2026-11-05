package radio

import (
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Stations that send song titles do it the SHOUTcast way: asked with
// Icy-MetaData: 1, they answer with icy-metaint: N, and after every N bytes
// of audio comes one length byte L and L×16 bytes of text such as
// StreamTitle='Artist - Song';. Players can't use that text, so Relay takes
// it out of the audio and keeps the title.

// Relay plays a station to a player: it passes the stream through, takes
// the song titles out of it for onTitle, and ends when the player leaves.
func Relay(w http.ResponseWriter, r *http.Request, client *http.Client, addr string, onTitle func(string)) {
	RelayConverting(w, r, client, addr, onTitle, nil)
}

// Converter turns a stream players can't play into one they can, writing
// the response itself (headers included). Relay uses one for uncompressed
// (PCM) streams, such as an Air-Mixer output's: few players can play an
// endless WAV.
type Converter func(w http.ResponseWriter, r *http.Request, contentType string, body io.Reader)

// IsPCM says whether a stream's content type is uncompressed audio in a
// WAV container.
func IsPCM(ct string) bool {
	switch ct {
	case "audio/wav", "audio/x-wav", "audio/wave", "audio/vnd.wave":
		return true
	}
	return false
}

// RelayConverting is Relay, with PCM streams going through pcm when it
// isn't nil.
func RelayConverting(w http.ResponseWriter, r *http.Request, client *http.Client, addr string, onTitle func(string), pcm Converter) {
	resp, err := open(r.Context(), client, addr, true)
	if err != nil {
		if r.Context().Err() == nil {
			http.Error(w, "the station isn't answering", http.StatusBadGateway)
		}
		return
	}
	defer resp.Body.Close()
	h := w.Header()
	ct := resp.Header.Get("Content-Type")
	if base := strings.ToLower(strings.TrimSpace(strings.Split(ct, ";")[0])); pcm != nil && IsPCM(base) {
		pcm(w, r, base, resp.Body)
		return
	}
	if !isAudio(strings.ToLower(strings.TrimSpace(strings.Split(ct, ";")[0]))) {
		ct = "audio/mpeg"
	}
	h.Set("Content-Type", ct)
	// A live stream has no end and can't be cached or skipped around in.
	h.Set("Cache-Control", "no-store")
	h.Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	var body io.Reader = resp.Body
	if n, err := strconv.Atoi(strings.TrimSpace(resp.Header.Get("Icy-Metaint"))); err == nil && n > 0 {
		body = &icyReader{r: resp.Body, every: n, left: n, onTitle: onTitle}
	}
	copyFlushing(w, body)
}

// copyFlushing sends audio as it comes, so playing starts right away.
func copyFlushing(w http.ResponseWriter, r io.Reader) {
	f, _ := w.(http.Flusher)
	buf := make([]byte, 16<<10)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return
			}
			if f != nil {
				f.Flush()
			}
		}
		if err != nil {
			return
		}
	}
}

// icyReader reads the audio of a stream with titles mixed in, without them.
type icyReader struct {
	r       io.Reader
	every   int // bytes of audio between titles
	left    int // bytes of audio until the next one
	onTitle func(string)
}

func (ir *icyReader) Read(p []byte) (int, error) {
	if ir.left == 0 {
		var l [1]byte
		if _, err := io.ReadFull(ir.r, l[:]); err != nil {
			return 0, err
		}
		if n := int(l[0]) * 16; n > 0 {
			meta := make([]byte, n)
			if _, err := io.ReadFull(ir.r, meta); err != nil {
				return 0, err
			}
			if t, ok := StreamTitle(string(meta)); ok && ir.onTitle != nil {
				ir.onTitle(t)
			}
		}
		ir.left = ir.every
	}
	if len(p) > ir.left {
		p = p[:ir.left]
	}
	n, err := ir.r.Read(p)
	ir.left -= n
	return n, err
}

// StreamTitle reads the title out of a block such as
// StreamTitle='Above & Beyond - Sun & Moon';StreamUrl=”;
func StreamTitle(meta string) (string, bool) {
	meta = strings.TrimRight(meta, "\x00")
	const key = "StreamTitle='"
	i := strings.Index(meta, key)
	if i < 0 {
		return "", false
	}
	rest := meta[i+len(key):]
	end := strings.Index(rest, "';")
	if end < 0 {
		end = strings.LastIndex(rest, "'")
	}
	if end < 0 {
		return "", false
	}
	return clean(rest[:end]), true
}

// Titles remembers the song each station played last, for players to show.
type Titles struct {
	mu sync.Mutex
	m  map[int64]Title
}

// Title is a station's song and when it was heard.
type Title struct {
	Title string    `json:"title"`
	At    time.Time `json:"at"`
}

// Set keeps a station's song.
func (t *Titles) Set(station int64, title string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.m == nil {
		t.m = map[int64]Title{}
	}
	t.m[station] = Title{Title: title, At: time.Now()}
}

// Get is a station's song, if it sent one in the last hour.
func (t *Titles) Get(station int64) (Title, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	v, ok := t.m[station]
	if !ok || time.Since(v.At) > time.Hour {
		return Title{}, false
	}
	return v, true
}
