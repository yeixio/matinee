// Package radio plays internet radio stations: streams that go on for as
// long as anyone listens, from Icecast, SHOUTcast and the like. People add
// a station by its stream's address, or find one in Radio Browser
// (radio-browser.info), an open directory of stations. The server passes
// the stream through to players, and reads the song titles stations send
// along with it (see icy.go).
package radio

import (
	"bufio"
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
)

var (
	// ErrBadURL means the address isn't an http or https one.
	ErrBadURL = errors.New("that isn't a web address")
	// ErrNotAudio means the address answered with something that isn't a
	// stream, such as a web page.
	ErrNotAudio = errors.New("that address isn't a radio stream")
	// ErrHLS means the station streams in pieces (HLS), which Matinee
	// can't relay yet.
	ErrHLS = errors.New("that station streams as HLS, which isn't supported yet. Look for its MP3 or AAC stream")
)

// UserAgent is sent to stations, which count listeners by it.
const UserAgent = "Matinee/1.0 (+https://matinee.yeix.io)"

// Info is what a station says about itself when asked for its stream.
type Info struct {
	URL         string // the stream, after following a playlist file
	Name        string // icy-name, if it sends one
	Homepage    string // icy-url
	Genre       string // icy-genre
	ContentType string
}

// Normalize checks a station's address and fills in https:// when it's left out.
func Normalize(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ErrBadURL
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", ErrBadURL
	}
	return u.String(), nil
}

// Probe opens a station's stream to check it plays, and reads what the
// station says about itself. A playlist file (.pls or .m3u) is followed to
// the first stream it lists.
func Probe(ctx context.Context, client *http.Client, raw string) (Info, error) {
	addr, err := Normalize(raw)
	if err != nil {
		return Info{}, err
	}
	for range 3 { // a playlist may point at another playlist
		resp, err := open(ctx, client, addr, false)
		if err != nil {
			return Info{}, err
		}
		ct, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
		ct = strings.ToLower(ct)
		if isPlaylist(ct, addr) {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
			resp.Body.Close()
			if strings.Contains(string(body), "#EXT-X-") {
				return Info{}, ErrHLS
			}
			next := firstStream(string(body), addr)
			if next == "" {
				return Info{}, ErrNotAudio
			}
			addr = next
			continue
		}
		resp.Body.Close()
		if !isAudio(ct) {
			return Info{}, ErrNotAudio
		}
		return Info{
			URL: addr, ContentType: ct,
			Name:     clean(resp.Header.Get("Icy-Name")),
			Homepage: clean(resp.Header.Get("Icy-Url")),
			Genre:    clean(resp.Header.Get("Icy-Genre")),
		}, nil
	}
	return Info{}, ErrNotAudio
}

// open asks a station for its stream; icy asks for song titles in it too.
func open(ctx context.Context, client *http.Client, addr string, icy bool) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, addr, nil)
	if err != nil {
		return nil, ErrBadURL
	}
	req.Header.Set("User-Agent", UserAgent)
	if icy {
		req.Header.Set("Icy-MetaData", "1")
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, &StatusError{resp.Status}
	}
	return resp, nil
}

// StatusError is a station answering with something other than its stream.
type StatusError struct{ Status string }

func (e *StatusError) Error() string { return "the station answered " + e.Status }

func isAudio(ct string) bool {
	return strings.HasPrefix(ct, "audio/") && !isPlaylistType(ct) || ct == "application/ogg" || ct == "video/mp2t"
}

func isPlaylistType(ct string) bool {
	switch ct {
	case "audio/x-scpls", "audio/scpls", "audio/x-mpegurl", "audio/mpegurl", "application/vnd.apple.mpegurl",
		"application/x-mpegurl", "application/pls+xml":
		return true
	}
	return false
}

func isPlaylist(ct, addr string) bool {
	if isPlaylistType(ct) {
		return true
	}
	if isAudio(ct) {
		return false
	}
	p := strings.ToLower(addr)
	if u, err := url.Parse(addr); err == nil {
		p = strings.ToLower(u.Path)
	}
	return strings.HasSuffix(p, ".pls") || strings.HasSuffix(p, ".m3u") || strings.HasSuffix(p, ".m3u8")
}

// firstStream finds the first address in a .pls (File1=…) or .m3u playlist.
func firstStream(body, base string) string {
	sc := bufio.NewScanner(strings.NewReader(body))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "[") {
			continue
		}
		if k, v, ok := strings.Cut(line, "="); ok {
			if !strings.HasPrefix(strings.ToLower(k), "file") {
				continue
			}
			line = strings.TrimSpace(v)
		}
		u, err := url.Parse(line)
		if err != nil {
			continue
		}
		if b, err := url.Parse(base); err == nil {
			u = b.ResolveReference(u)
		}
		if u.Scheme == "http" || u.Scheme == "https" {
			return u.String()
		}
	}
	return ""
}

// clean drops what isn't printable from a header a station set.
func clean(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || r == 0xfffd {
			return -1
		}
		return r
	}, s)
	return strings.TrimSpace(s)
}
