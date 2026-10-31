package stream

import (
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/yeixio/matinee/internal/store"
)

var mimeByExt = map[string]string{
	".mp4": "video/mp4", ".m4v": "video/mp4", ".mov": "video/quicktime", ".webm": "video/webm",
	".mkv": "video/x-matroska", ".mp3": "audio/mpeg", ".flac": "audio/flac", ".m4a": "audio/mp4", ".m4b": "audio/mp4",
	".aac": "audio/aac", ".ogg": "audio/ogg", ".opus": "audio/ogg", ".wav": "audio/wav",
}

// MimeType guesses a file's content type for clients deciding whether they can play it.
func MimeType(path string) string {
	if store.IsRemote(path) {
		// An address: its path's extension, without the query.
		if u, err := url.Parse(path); err == nil {
			path = u.Path
		}
	}
	ext := strings.ToLower(filepath.Ext(path))
	if m, ok := mimeByExt[ext]; ok {
		return m
	}
	if m := mime.TypeByExtension(ext); m != "" {
		return m
	}
	return "application/octet-stream"
}

// ServeDirect streams the original file with range support, so seeking is instant.
func ServeDirect(w http.ResponseWriter, r *http.Request, it store.Item) {
	if store.IsRemote(it.Path) {
		serveRemote(w, r, it)
		return
	}
	f, err := os.Open(it.Path)
	if err != nil {
		http.Error(w, "file unavailable", http.StatusNotFound)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		http.Error(w, "file unavailable", http.StatusNotFound)
		return
	}
	allowAnyOrigin(w)
	w.Header().Set("Content-Type", MimeType(it.Path))
	http.ServeContent(w, r, "", info.ModTime(), f)
}

// allowAnyOrigin lets a cast device's player page (Chromecast's, served from
// Google) read the media. The links are signed with ?token=, and no cookies
// are sent cross-origin, so this exposes nothing a link doesn't already.
func allowAnyOrigin(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
}

// Remote is how the server fetches media that lives on the internet, such
// as podcast episodes: no overall time limit, since an episode can play for
// hours, but a quick one for the answer to start.
var Remote = &http.Client{Transport: &http.Transport{
	Proxy:                 http.ProxyFromEnvironment,
	DialContext:           (&net.Dialer{Timeout: 15 * time.Second, Control: publicOnly}).DialContext,
	ResponseHeaderTimeout: 20 * time.Second,
	IdleConnTimeout:       90 * time.Second,
	MaxIdleConnsPerHost:   4,
}}

// AllowHomeNetwork lets Remote reach addresses on the home network and the
// server itself. Off, anyone who can add a podcast can't use a feed to
// make the server fetch from other devices at home, like a router's page.
var AllowHomeNetwork atomic.Bool

// ErrHomeNetwork is a connection Remote refused to make.
var ErrHomeNetwork = errors.New("addresses on the home network aren't allowed")

func publicOnly(network, address string, _ syscall.RawConn) error {
	if AllowHomeNetwork.Load() {
		return nil
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return err
	}
	ip = ip.Unmap()
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified() || ip.IsMulticast() || cgnat.Contains(ip) {
		return ErrHomeNetwork
	}
	return nil
}

// cgnat is the shared address space carriers (and Tailscale) use.
var cgnat = netip.MustParsePrefix("100.64.0.0/10")

// remoteUserAgent is sent with requests for remote media. Podcast hosts
// count listens by it.
const remoteUserAgent = "Matinee/1.0 (+https://matinee.yeix.io)"

// serveRemote passes media on the internet through to the player, ranges
// and all, so players only ever talk to the server: they don't need to
// reach the show's host, and remote access works the same as for files.
func serveRemote(w http.ResponseWriter, r *http.Request, it store.Item) {
	req, err := http.NewRequestWithContext(r.Context(), r.Method, store.EpisodeURL(it.Path), nil)
	if err != nil || r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "file unavailable", http.StatusNotFound)
		return
	}
	req.Header.Set("User-Agent", remoteUserAgent)
	for _, h := range []string{"Range", "If-Range", "If-None-Match", "If-Modified-Since"} {
		if v := r.Header.Get(h); v != "" {
			req.Header.Set(h, v)
		}
	}
	resp, err := Remote.Do(req)
	if err != nil {
		if r.Context().Err() == nil {
			http.Error(w, "the show's host isn't answering", http.StatusBadGateway)
		}
		return
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusPartialContent ||
		resp.StatusCode == http.StatusNotModified || resp.StatusCode == http.StatusRequestedRangeNotSatisfiable:
	default:
		http.Error(w, "the show's host answered "+resp.Status, http.StatusBadGateway)
		return
	}
	allowAnyOrigin(w)
	h := w.Header()
	for _, k := range []string{"Content-Length", "Content-Range", "Accept-Ranges", "ETag", "Last-Modified"} {
		if v := resp.Header.Get(k); v != "" {
			h.Set(k, v)
		}
	}
	// Hosts often say application/octet-stream; the episode's type is known.
	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "audio/") && !strings.HasPrefix(ct, "video/") {
		ct = MimeType(it.Path)
	}
	h.Set("Content-Type", ct)
	w.WriteHeader(resp.StatusCode)
	if r.Method == http.MethodGet {
		io.Copy(w, resp.Body)
	}
}

// Input is FFmpeg's input options for an item's media: its file, or its
// address on the internet, reconnecting if the host drops the connection.
func Input(it store.Item) []string {
	if !store.IsRemote(it.Path) {
		return []string{"-i", it.Path}
	}
	return []string{"-user_agent", remoteUserAgent, "-reconnect", "1", "-reconnect_streamed", "1",
		"-reconnect_on_network_error", "1", "-reconnect_delay_max", "10", "-i", store.EpisodeURL(it.Path)}
}
