package livetv

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// UserAgent is sent with Matinee's own requests, for playlists and guides.
const UserAgent = "Matinee/1.0 (+https://matinee.yeix.io)"

// Limits on what Matinee reads.
const (
	maxPlaylist = 64 << 20  // an M3U file
	maxGuide    = 512 << 20 // an XMLTV file, after unzipping it is read as it comes
)

var (
	// ErrBadURL means an address isn't an http or https one.
	ErrBadURL = errors.New("that isn't a web address")
	// ErrNotPlaylist means the address answered with something that isn't
	// an M3U playlist, such as a web page.
	ErrNotPlaylist = errors.New("that address isn't a channel playlist (M3U)")
	// ErrNoChannels means the playlist lists no channels Matinee can play.
	ErrNoChannels = errors.New("that playlist has no channels Matinee can play")
)

// Normalize checks an address and fills in https:// when it's left out.
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

// StatusError is a host answering with something other than what was asked for.
type StatusError struct{ Status string }

func (e *StatusError) Error() string { return "it answered " + e.Status }

// Open fetches an address, with Matinee's user agent.
func Open(ctx context.Context, client *http.Client, addr string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, addr, nil)
	if err != nil {
		return nil, ErrBadURL
	}
	req.Header.Set("User-Agent", UserAgent)
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

// FetchPlaylist reads the M3U playlist at addr.
func FetchPlaylist(ctx context.Context, client *http.Client, addr string) (Playlist, error) {
	addr, err := Normalize(addr)
	if err != nil {
		return Playlist{}, err
	}
	resp, err := Open(ctx, client, addr)
	if err != nil {
		return Playlist{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxPlaylist))
	if err != nil {
		return Playlist{}, err
	}
	head := bytes.TrimLeft(bytes.TrimPrefix(body, []byte("\ufeff")), " \t\r\n")
	if !bytes.HasPrefix(head, []byte("#EXTM3U")) && !bytes.Contains(body[:min(len(body), 4096)], []byte("#EXTINF")) {
		return Playlist{}, ErrNotPlaylist
	}
	if bytes.Contains(body[:min(len(body), 4096)], []byte("#EXT-X-")) && !bytes.Contains(body, []byte("#EXTINF:-1")) {
		// One channel's own stream, not a list of channels.
		return Playlist{}, ErrNotPlaylist
	}
	return ParsePlaylist(bytes.NewReader(body), resp.Request.URL.String())
}

// Spec says where a source's channels come from: a playlist, or
// iptv-org's lists picked by a filter.
type Spec struct {
	Playlist string
	IPTVOrg  *Filter
	Base     string // iptv-org's base, empty for IPTVOrgBase
}

// Load reads a source's channels.
func Load(ctx context.Context, client *http.Client, spec Spec) (Playlist, error) {
	if spec.IPTVOrg == nil {
		p, err := FetchPlaylist(ctx, client, spec.Playlist)
		if err == nil && len(p.Entries) == 0 {
			err = ErrNoChannels
		}
		return p, err
	}
	var out Playlist
	var keep map[string]bool
	for i, set := range spec.IPTVOrg.Playlists(spec.Base) {
		in := map[string]bool{}
		for _, addr := range set {
			p, err := FetchPlaylist(ctx, client, addr)
			var se *StatusError
			if errors.As(err, &se) && strings.HasPrefix(se.Status, "404") {
				continue // a country with no channels
			}
			if err != nil {
				return Playlist{}, fmt.Errorf("reading %s: %w", addr, err)
			}
			for _, e := range p.Entries {
				if i == 0 && !in[e.URL] {
					out.Entries = append(out.Entries, e)
				}
				in[e.URL] = true
			}
		}
		if i > 0 {
			for u := range keep {
				if !in[u] {
					delete(keep, u)
				}
			}
		} else {
			keep = in
		}
	}
	kept := out.Entries[:0]
	for _, e := range out.Entries {
		if keep[e.URL] {
			kept = append(kept, e)
		}
	}
	out.Entries = kept
	if len(out.Entries) == 0 {
		return out, ErrNoChannels
	}
	return out, nil
}
