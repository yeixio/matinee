package api

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"sync"

	"github.com/yeixio/matinee/internal/hwaccel"
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

// SettingHardwareAcceleration holds serverSettings.HardwareAcceleration. The
// server reads it at startup to set up the transcoder.
const SettingHardwareAcceleration = "hardware_acceleration"

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
	// Requests says who may add titles through Radarr and Sonarr besides
	// admins: off (nobody), approve (anyone may ask; an admin approves) or
	// auto (what people ask for is added right away). See arr.go.
	Requests string `json:"requests"`
	// HardwareAcceleration is what transcodes run on: auto (the best
	// accelerator that passed its test at startup), off (the CPU), or one
	// accelerator by id. See internal/hwaccel.
	HardwareAcceleration string `json:"hardwareAcceleration"`
	// Hardware says which accelerator is in use and which ones work here.
	// It can't be changed.
	Hardware *hwaccel.Status `json:"hardware,omitempty"`
	// RemoteTrailers offers trailers from TMDB (played from YouTube) along
	// with trailer files next to the media. On unless an admin turns it off.
	RemoteTrailers bool `json:"remoteTrailers"`
}

func (s *Server) settings(ctx context.Context) (serverSettings, error) {
	required, err := s.homeSignInRequired(ctx)
	if err != nil {
		return serverSettings{}, err
	}
	mode, err := s.requestMode(ctx)
	if err != nil {
		return serverSettings{}, err
	}
	trailers, err := s.remoteTrailersOn(ctx)
	set := serverSettings{RequireSignInAtHome: required, Requests: mode, RemoteTrailers: trailers,
		HardwareAcceleration: hwaccel.Auto, Hardware: &hwaccel.Status{Available: []hwaccel.Accel{}}}
	if hw := s.HW; hw != nil {
		st := hw.Status()
		set.Hardware, set.HardwareAcceleration = &st, st.Preference
	}
	return set, err
}

func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) {
	set, err := s.settings(r.Context())
	if err != nil {
		s.serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, set)
}

// putSettings changes the settings it's sent; ones left out stay as they are.
func (s *Server) putSettings(w http.ResponseWriter, r *http.Request) {
	var in struct {
		RequireSignInAtHome  *bool   `json:"requireSignInAtHome"`
		Requests             *string `json:"requests"`
		HardwareAcceleration *string `json:"hardwareAcceleration"`
		RemoteTrailers       *bool   `json:"remoteTrailers"`
		// Read-only; accepted so a client can send back what it got.
		Hardware json.RawMessage `json:"hardware"`
	}
	if err := readJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "bad request")
		return
	}
	ctx := r.Context()
	if m := in.Requests; m != nil && *m != "" {
		if *m != requestsOff && *m != requestsApprove && *m != requestsAuto {
			writeError(w, http.StatusBadRequest, "requests must be off, approve or auto")
			return
		}
		if err := s.Store.SetSetting(ctx, settingRequests, *m); err != nil {
			s.serverError(w, err)
			return
		}
	}
	if p := in.HardwareAcceleration; p != nil && *p != "" {
		if !hwaccel.ValidPreference(*p) {
			writeError(w, http.StatusBadRequest, "hardwareAcceleration must be auto, off or an accelerator's id")
			return
		}
		if err := s.Store.SetSetting(ctx, SettingHardwareAcceleration, *p); err != nil {
			s.serverError(w, err)
			return
		}
		if hw := s.HW; hw != nil {
			hw.SetPreference(*p)
		}
	}
	if in.RemoteTrailers != nil {
		if err := s.Store.SetSetting(ctx, settingRemoteTrailers, strconv.FormatBool(*in.RemoteTrailers)); err != nil {
			s.serverError(w, err)
			return
		}
	}
	if in.RequireSignInAtHome != nil {
		required := *in.RequireSignInAtHome
		if err := s.Store.SetSetting(ctx, settingHomeSignIn, strconv.FormatBool(required)); err != nil {
			s.serverError(w, err)
			return
		}
		s.homeSignIn.mu.Lock()
		s.homeSignIn.loaded, s.homeSignIn.required = true, required
		s.homeSignIn.mu.Unlock()
	}
	set, err := s.settings(ctx)
	if err != nil {
		s.serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, set)
}
