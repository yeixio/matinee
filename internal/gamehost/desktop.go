package gamehost

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

// The desktop is a web page that shows the game host's screen, with a
// keyboard and mouse, such as the noVNC page a Steam container serves: an
// admin uses it to sign in to Steam and install games. The host passes it
// on under /v1/desktop/, behind its token, and Matinee passes it on to its
// admins, so it never needs its own way in.

// DesktopPath is where the host serves the desktop.
const DesktopPath = "/v1/desktop/"

func (h *Host) desktop() http.HandlerFunc {
	target, err := url.Parse(h.Desktop)
	if h.Desktop == "" || err != nil {
		return func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "no desktop on this game host", http.StatusNotFound)
		}
	}
	p := &httputil.ReverseProxy{Rewrite: func(pr *httputil.ProxyRequest) {
		pr.SetURL(target)
		pr.Out.URL.Path = strings.TrimSuffix(target.Path, "/") + "/" + strings.TrimPrefix(pr.In.URL.Path, DesktopPath)
		pr.Out.URL.RawPath = ""
		pr.Out.Header.Del("Authorization")
	}}
	return p.ServeHTTP
}

// Desktop passes requests on to the host's desktop: path is the part
// after the desktop's own address. It adds the token, and drops the
// player's cookies and credentials so they never reach the host.
func (c *Client) Desktop(path string) http.Handler {
	base, err := url.Parse(c.url(DesktopPath))
	if err != nil {
		return http.NotFoundHandler()
	}
	var transport http.RoundTripper
	if c.HTTP != nil {
		transport = c.HTTP.Transport
	}
	return &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(base)
			pr.Out.URL.Path = base.Path + strings.TrimPrefix(path, "/")
			pr.Out.URL.RawPath = ""
			pr.Out.Header.Del("Cookie")
			pr.Out.Header.Del("Authorization")
			for k, v := range c.header() {
				pr.Out.Header[k] = v
			}
		},
		Transport: transport,
	}
}
