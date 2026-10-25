package api

import (
	"context"
	"net"
	"net/http"
	"net/netip"
	"sync"
)

// Devices on the home network can browse and play without signing in,
// unless an admin turns that off. They share one guest account (see
// store.HomeGuest), which can't change anything and can't get a token to
// use away from home.
//
// Telling "home" from the internet goes by the address the connection
// comes from, and fails closed:
//
//   - A request that came through a proxy (it carries X-Forwarded-For,
//     X-Real-IP or Forwarded) never counts. Behind an ingress or reverse
//     proxy every request arrives from a private address, the internet's
//     included, so the address says nothing.
//   - Otherwise it counts when it comes from a private, link-local or
//     loopback address.
//   - With a separate home listener (MATINEE_HOME_ADDR, which the Helm
//     chart opens as a host port on the node), only requests to that
//     listener count, never ones to the main address.

const settingHomeSignIn = "home_sign_in_required"

type homeKey struct{}

// HomeHandler serves the same API as Handler, on the listener meant for
// the home network.
func (s *Server) HomeHandler() http.Handler {
	h := s.Handler()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), homeKey{}, true)))
	})
}

// fromHome reports whether a request comes straight from the home network.
func (s *Server) fromHome(r *http.Request) bool {
	if s.HomeListener && r.Context().Value(homeKey{}) == nil {
		return false
	}
	return directFromPrivateNetwork(r)
}

func directFromPrivateNetwork(r *http.Request) bool {
	for _, h := range []string{"X-Forwarded-For", "X-Real-Ip", "Forwarded"} {
		if r.Header.Get(h) != "" {
			return false
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	ip = ip.Unmap()
	return ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast()
}

// homeAccess reports whether a request may use the guest account.
func (s *Server) homeAccess(r *http.Request) (bool, error) {
	if !s.fromHome(r) {
		return false, nil
	}
	required, err := s.homeSignInRequired(r.Context())
	if err != nil || required {
		return false, err
	}
	// A brand new server has no admin yet; setup comes first. Once there is
	// one there always is, so that's only asked until then.
	if s.setUp.Load() {
		return true, nil
	}
	n, err := s.Store.CountUsers(r.Context())
	s.setUp.Store(n > 0)
	return n > 0, err
}

// The setting is read on almost every request, so it's kept in memory once
// read; putSettings updates both.
type homeSetting struct {
	mu       sync.Mutex
	loaded   bool
	required bool
}

func (s *Server) homeSignInRequired(ctx context.Context) (bool, error) {
	h := &s.homeSignIn
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.loaded {
		v, err := s.Store.Setting(ctx, settingHomeSignIn)
		if err != nil {
			return false, err
		}
		h.loaded, h.required = true, v == "true"
	}
	return h.required, nil
}

type serverSettings struct {
	// RequireSignInAtHome makes devices on the home network sign in too.
	RequireSignInAtHome bool `json:"requireSignInAtHome"`
}

func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) {
	required, err := s.homeSignInRequired(r.Context())
	if err != nil {
		s.serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, serverSettings{RequireSignInAtHome: required})
}

func (s *Server) putSettings(w http.ResponseWriter, r *http.Request) {
	var in serverSettings
	if err := readJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "bad request")
		return
	}
	v := "false"
	if in.RequireSignInAtHome {
		v = "true"
	}
	if err := s.Store.SetSetting(r.Context(), settingHomeSignIn, v); err != nil {
		s.serverError(w, err)
		return
	}
	s.homeSignIn.mu.Lock()
	s.homeSignIn.loaded, s.homeSignIn.required = true, in.RequireSignInAtHome
	s.homeSignIn.mu.Unlock()
	writeJSON(w, http.StatusOK, in)
}
