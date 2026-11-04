package livetv

import (
	"bufio"
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// Most channels stream as HLS: a playlist naming short pieces of video,
// fetched one after another. The server passes them through to players,
// rewriting each playlist so the pieces it names come through the server
// too. Players then reach any channel the server can, even one on plain
// http or on the home network, and channels that need a referrer or user
// agent get them. Each link the server writes is signed, so the relay
// only fetches what a channel's own playlists name.

// Signer signs relay links. Key is the server's secret.
type Signer struct{ Key []byte }

// Sign is the signature for addr on channel.
func (s Signer) Sign(channel, addr string) string {
	m := hmac.New(sha256.New, s.Key)
	m.Write([]byte(channel))
	m.Write([]byte{0})
	m.Write([]byte(addr))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil)[:16])
}

// Verify checks a link's signature.
func (s Signer) Verify(channel, addr, sig string) bool {
	return hmac.Equal([]byte(s.Sign(channel, addr)), []byte(sig))
}

// EncodeURL and DecodeURL carry an address in a link.
func EncodeURL(addr string) string { return base64.RawURLEncoding.EncodeToString([]byte(addr)) }

func DecodeURL(s string) (string, error) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	return string(b), err
}

var uriAttr = regexp.MustCompile(`URI="([^"]*)"`)

// Rewrite changes each address in an HLS playlist fetched from base into
// the link that link returns for it.
func Rewrite(playlist []byte, base *url.URL, link func(abs string, isPlaylist bool) string) []byte {
	var out bytes.Buffer
	sc := bufio.NewScanner(bytes.NewReader(playlist))
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	nextIsPlaylist := false
	resolve := func(ref string) string {
		u, err := url.Parse(strings.TrimSpace(ref))
		if err != nil {
			return ""
		}
		return base.ResolveReference(u).String()
	}
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case line == "":
		case strings.HasPrefix(line, "#"):
			if strings.HasPrefix(line, "#EXT-X-STREAM-INF") {
				nextIsPlaylist = true
			}
			isList := strings.HasPrefix(line, "#EXT-X-MEDIA:") || strings.HasPrefix(line, "#EXT-X-I-FRAME-STREAM-INF")
			line = uriAttr.ReplaceAllStringFunc(line, func(m string) string {
				ref := uriAttr.FindStringSubmatch(m)[1]
				if strings.HasPrefix(ref, "data:") || strings.HasPrefix(ref, "skd:") {
					return m
				}
				abs := resolve(ref)
				if abs == "" {
					return m
				}
				return `URI="` + link(abs, isList) + `"`
			})
		default:
			if abs := resolve(line); abs != "" {
				line = link(abs, nextIsPlaylist || looksLikePlaylist(abs))
			}
			nextIsPlaylist = false
		}
		out.WriteString(line)
		out.WriteByte('\n')
	}
	return out.Bytes()
}

func looksLikePlaylist(addr string) bool {
	u, err := url.Parse(addr)
	if err != nil {
		return false
	}
	p := strings.ToLower(u.Path)
	return strings.HasSuffix(p, ".m3u8") || strings.HasSuffix(p, ".m3u")
}

// IsHLS says whether a response is an HLS playlist: by its type, or by
// its first bytes when the type says nothing useful.
func IsHLS(contentType string, head []byte) bool {
	ct, _, _ := mime.ParseMediaType(contentType)
	switch strings.ToLower(ct) {
	case "application/vnd.apple.mpegurl", "application/x-mpegurl", "audio/mpegurl", "audio/x-mpegurl":
		return true
	}
	return bytes.HasPrefix(bytes.TrimLeft(bytes.TrimPrefix(head, []byte("\ufeff")), " \t\r\n"), []byte("#EXTM3U"))
}

// Stream is where a channel streams and how to ask for it.
type Stream struct {
	URL       string
	UserAgent string
	Referrer  string
}

// Request is a GET for addr, with what the channel needs.
func (s Stream) Request(ctx context.Context, addr string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, addr, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", firstOf(s.UserAgent, "Mozilla/5.0 (compatible; "+UserAgent+")"))
	if s.Referrer != "" {
		req.Header.Set("Referer", s.Referrer)
	}
	return req, nil
}

// ErrBroken means a channel's stream didn't play.
var ErrBroken = errors.New("the channel isn't streaming")

// Check asks for a channel's stream to see that it plays: an HLS playlist
// that names pieces (following a list of qualities to its first), or a
// stream of video.
func Check(ctx context.Context, client *http.Client, s Stream) error {
	addr := s.URL
	for range 3 {
		req, err := s.Request(ctx, addr)
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		head, _ := io.ReadAll(io.LimitReader(resp.Body, 256<<10))
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return &StatusError{resp.Status}
		}
		if !IsHLS(resp.Header.Get("Content-Type"), head) {
			ct, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
			if strings.HasPrefix(ct, "video/") || ct == "application/octet-stream" || ct == "" && len(head) > 0 && head[0] == 0x47 {
				return nil
			}
			return ErrBroken
		}
		base := resp.Request.URL
		var next string
		pieces := 0
		Rewrite(head, base, func(abs string, isPlaylist bool) string {
			if isPlaylist {
				if next == "" {
					next = abs
				}
			} else {
				pieces++
			}
			return abs
		})
		if pieces > 0 {
			return nil
		}
		if next == "" {
			return ErrBroken
		}
		addr = next
	}
	return ErrBroken
}
