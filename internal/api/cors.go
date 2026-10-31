package api

import (
	"net/http"
	"net/url"
	"strings"
)

// The Samsung and LG TV apps are the web player packaged as a TV app: they
// run from a file:// page, so every request they make is cross-origin and
// the TV's browser reads the answer only when the API allows it (CORS).
//
// The API allows any origin, without cookies, so a cross-origin caller is
// only ever who its token says. What it never gets is what the home network
// gives for free: any website open in a browser at home could otherwise read
// the library through the guest account (homeAccess) or try passwords without
// the sign-in limits (signInKeys). A TV app signs in like an app away from
// home, with a code or a password.

// crossOrigin reports whether a browser sent the request from another
// origin's page. Same-origin requests carry no Origin, or the server's own.
func crossOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return false
	}
	u, err := url.Parse(origin)
	return err != nil || u.Host == "" || !strings.EqualFold(u.Host, r.Host)
}

// allowOtherOrigins answers CORS preflights for the API and lets other
// origins read its answers.
func allowOtherOrigins(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") || !crossOrigin(r) {
			next.ServeHTTP(w, r)
			return
		}
		h := w.Header()
		h.Set("Access-Control-Allow-Origin", "*")
		h.Add("Vary", "Origin")
		if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
			h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE")
			if headers := r.Header.Get("Access-Control-Request-Headers"); headers != "" {
				h.Set("Access-Control-Allow-Headers", headers)
			}
			// Newer browsers ask before a page reaches a home network address.
			if r.Header.Get("Access-Control-Request-Private-Network") == "true" {
				h.Set("Access-Control-Allow-Private-Network", "true")
			}
			h.Set("Access-Control-Max-Age", "600")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		// Browsers don't send cookies with these, but a page can ask them to.
		r.Header.Del("Cookie")
		next.ServeHTTP(w, r)
	})
}
