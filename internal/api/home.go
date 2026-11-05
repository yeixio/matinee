package api

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/yeixio/matinee/internal/hwaccel"
	"github.com/yeixio/matinee/internal/metadata"
	"github.com/yeixio/matinee/internal/remote"
	"github.com/yeixio/matinee/internal/scan"
	"github.com/yeixio/matinee/internal/stream"
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
//   - Requests to the remote access port (see remote.go) never count. Some
//     routers make forwarded connections look like they come from the
//     router itself.
//   - With a separate home listener (MATINEE_HOME_ADDR, which the Helm
//     chart opens as a host port on the node), only requests to that
//     listener count, never ones to the main address.

const settingHomeSignIn = "home_sign_in_required"

// SettingHardwareAcceleration holds serverSettings.HardwareAcceleration. The
// server reads it at startup to set up the transcoder.
const SettingHardwareAcceleration = "hardware_acceleration"

// settingToneMapping holds serverSettings.ToneMapping.
const settingToneMapping = "tone_mapping"

// toneMapping is how converted HDR is made SDR: an algorithm, or off.
func (s *Server) toneMapping(ctx context.Context) (string, error) {
	v, err := s.Store.Setting(ctx, settingToneMapping)
	return cmp.Or(v, stream.ToneMapHable), err
}

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
	if r.Context().Value(remoteKey{}) != nil {
		return false
	}
	return directFromPrivateNetwork(r)
}

func directFromPrivateNetwork(r *http.Request) bool {
	if !direct(r) {
		return false
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
	// Not for a page from somewhere else, such as a website open at home (cors.go).
	if !s.fromHome(r) || crossOrigin(r) {
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
	// ToneMapping is how an HDR picture that's converted is made SDR, so
	// it isn't flat and grey: hable (the default), mobius, reinhard, or
	// off. ToneMapTools says what this server's FFmpeg can do it with;
	// it can't be changed.
	ToneMapping  string            `json:"toneMapping"`
	ToneMapTools *stream.ToneTools `json:"toneMapTools,omitempty"`
	// RemoteTrailers offers trailers from TMDB (played from YouTube) along
	// with trailer files next to the media. On unless an admin turns it off.
	RemoteTrailers bool `json:"remoteTrailers"`
	// HolidayRows shows a row of titles for a holiday on Home in the weeks
	// before it (see holidays.go). On unless an admin turns it off.
	HolidayRows bool `json:"holidayRows"`
	// BoxOfficeAsksNeedApproval makes other editions and better copies,
	// asked for from Box Office, always wait for an admin, even when
	// requests are added right away (see boxoffice_pairing.go). Off unless
	// an admin turns it on.
	BoxOfficeAsksNeedApproval bool `json:"boxOfficeAsksNeedApproval"`
	// OnlineLyrics looks up songs' words on LRCLIB when there are none
	// next to the song or in its tags (see lyrics.go). Off unless an admin
	// turns it on.
	OnlineLyrics bool `json:"onlineLyrics"`
	// SubtitleSync lines subtitle files up with the audio the first time a
	// video plays with them (see subsync.go). On unless an admin turns it off.
	SubtitleSync bool `json:"subtitleSync"`
	// AdaptiveQuality makes transcodes adaptive streams of the quality steps
	// in QualitySteps, and lets people pick a step (see quality.go). Off
	// unless an admin turns it on.
	AdaptiveQuality bool     `json:"adaptiveQuality"`
	QualitySteps    []string `json:"qualitySteps"`
	// RemoteQualityCap is the best quality step a stream away from home
	// may use, or "" for no cap. It applies with adaptive quality off too.
	RemoteQualityCap string `json:"remoteQualityCap"`
	// Theme is what everyone sees until they pick their own (see themes.go).
	Theme string `json:"theme"`
	// ThemeProblems lists theme files in the themes folder that couldn't be
	// loaded, and why. It can't be changed.
	ThemeProblems []string `json:"themeProblems"`
	// RemoteAccess opens a port on the router so the apps reach this server
	// away from home (see remote.go). Off unless an admin turns it on.
	RemoteAccess bool `json:"remoteAccess"`
	// RemoteForwardedPort is the router port (443 or 80) an admin forwarded
	// to the remote access port by hand, for routers that won't open one
	// when asked; 0 asks the router.
	RemoteForwardedPort int `json:"remoteForwardedPort"`
	// Remote says how remote access is doing; nil when this server can't
	// offer it (MATINEE_REMOTE_ADDR=off). It can't be changed.
	Remote *remote.Status `json:"remote,omitempty"`
	// PublicURL is the address set with MATINEE_PUBLIC_URL, which the apps
	// use away from home instead. It can't be changed here.
	PublicURL string `json:"publicURL,omitempty"`
	// PublicURLHomeOnly means PublicURL's name doesn't resolve to a public
	// address, so it can't be reached from the internet and remote access
	// is offered as if it weren't set.
	PublicURLHomeOnly bool `json:"publicURLHomeOnly,omitempty"`
	// LibraryWatch is how library folders are watched for new files: auto
	// (change notifications on local disks, a quick check every so often on
	// network shares), poll (the quick check everywhere) or off. See
	// internal/scan/watch.go.
	LibraryWatch scan.WatchMode `json:"libraryWatch"`
	// WatchPollSeconds is how often polled folders are checked, and
	// ScanIntervalSeconds how often every library is rescanned in full (0
	// for never). Both are set on the server's command line and can't be
	// changed here.
	WatchPollSeconds    int `json:"watchPollSeconds"`
	ScanIntervalSeconds int `json:"scanIntervalSeconds"`
	// TMDB says where the TMDB key in use comes from. An admin changes it
	// with tmdbKey (see tmdbkey.go); the key itself is never sent back.
	TMDB tmdbStatus `json:"tmdb"`
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
	if err != nil {
		return serverSettings{}, err
	}
	holidaysOn, err := s.holidayRowsOn(ctx)
	if err != nil {
		return serverSettings{}, err
	}
	lyricsOn, err := s.onlineLyricsOn(ctx)
	if err != nil {
		return serverSettings{}, err
	}
	remoteOn, err := RemoteAccessOn(ctx, s.Store)
	if err != nil {
		return serverSettings{}, err
	}
	forwarded, err := RemoteForwardedPort(ctx, s.Store)
	if err != nil {
		return serverSettings{}, err
	}
	tmdb, err := s.tmdbStatus(ctx)
	if err != nil {
		return serverSettings{}, err
	}
	quality, err := s.qualitySettings(ctx)
	if err != nil {
		return serverSettings{}, err
	}
	toneMap, err := s.toneMapping(ctx)
	if err != nil {
		return serverSettings{}, err
	}
	asksNeed, err := s.Store.Setting(ctx, settingAsksNeedApproval)
	if err != nil {
		return serverSettings{}, err
	}
	themes, problems := s.themes()
	def, err := s.defaultTheme(ctx, themes)
	set := serverSettings{AdaptiveQuality: quality.Adaptive, QualitySteps: stepIDs(quality.Steps), RemoteQualityCap: quality.Cap, RequireSignInAtHome: required, Requests: mode, RemoteTrailers: trailers, HolidayRows: holidaysOn, OnlineLyrics: lyricsOn, SubtitleSync: s.autoSyncs(ctx), Theme: def,
		RemoteAccess: remoteOn, RemoteForwardedPort: forwarded, Remote: s.remoteStatus(), PublicURL: s.PublicURL, PublicURLHomeOnly: s.PublicURL != "" && !s.publicURLIsPublic(ctx), TMDB: tmdb,
		ThemeProblems: []string{}, HardwareAcceleration: hwaccel.Auto, Hardware: &hwaccel.Status{Available: []hwaccel.Accel{}}, ToneMapping: toneMap,
		BoxOfficeAsksNeedApproval: asksNeed == "true"}
	if s.Transcoder != nil {
		tools := s.Transcoder.ToneTools()
		set.ToneMapTools = &tools
	}
	for _, p := range problems {
		set.ThemeProblems = append(set.ThemeProblems, p.Error())
	}
	if hw := s.HW; hw != nil {
		st := hw.Status()
		set.Hardware, set.HardwareAcceleration = &st, st.Preference
	}
	set.LibraryWatch, set.ScanIntervalSeconds = scan.WatchOff, int(s.ScanInterval.Seconds())
	if w := s.Watcher; w != nil && err == nil {
		set.LibraryWatch, err = w.Mode(ctx)
		set.WatchPollSeconds = int(w.PollInterval.Seconds())
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
		RequireSignInAtHome  *bool     `json:"requireSignInAtHome"`
		Requests             *string   `json:"requests"`
		HardwareAcceleration *string   `json:"hardwareAcceleration"`
		ToneMapping          *string   `json:"toneMapping"`
		RemoteTrailers       *bool     `json:"remoteTrailers"`
		HolidayRows          *bool     `json:"holidayRows"`
		BoxOfficeAsksNeed    *bool     `json:"boxOfficeAsksNeedApproval"`
		OnlineLyrics         *bool     `json:"onlineLyrics"`
		SubtitleSync         *bool     `json:"subtitleSync"`
		AdaptiveQuality      *bool     `json:"adaptiveQuality"`
		QualitySteps         *[]string `json:"qualitySteps"`
		RemoteQualityCap     *string   `json:"remoteQualityCap"`
		Theme                *string   `json:"theme"`
		RemoteAccess         *bool     `json:"remoteAccess"`
		RemoteForwardedPort  *int      `json:"remoteForwardedPort"`
		LibraryWatch         *string   `json:"libraryWatch"`
		// TMDBKey replaces the TMDB key; "" goes back to the environment's.
		TMDBKey *string `json:"tmdbKey"`
		// Read-only; accepted so a client can send back what it got.
		Hardware            json.RawMessage `json:"hardware"`
		ToneMapTools        json.RawMessage `json:"toneMapTools"`
		ThemeProblems       json.RawMessage `json:"themeProblems"`
		Remote              json.RawMessage `json:"remote"`
		PublicURL           json.RawMessage `json:"publicURL"`
		WatchPollSeconds    json.RawMessage `json:"watchPollSeconds"`
		ScanIntervalSeconds json.RawMessage `json:"scanIntervalSeconds"`
		TMDB                json.RawMessage `json:"tmdb"`
	}
	if err := readJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "bad request")
		return
	}
	ctx := r.Context()
	// First, so a key TMDB turns down changes nothing else.
	if in.TMDBKey != nil {
		switch err := s.setTMDBKey(ctx, *in.TMDBKey); {
		case errors.Is(err, metadata.ErrBadKey):
			writeError(w, http.StatusBadRequest, "TMDB didn't accept that key")
			return
		case errors.Is(err, errTMDBUnreachable):
			writeError(w, http.StatusBadGateway, "TMDB couldn't be reached to check the key")
			return
		case errors.Is(err, errNoTMDB):
			writeError(w, http.StatusBadRequest, "this server can't change its TMDB key")
			return
		case err != nil:
			s.serverError(w, err)
			return
		}
	}
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
	if m := in.ToneMapping; m != nil && *m != "" {
		if *m != stream.ToneMapOff && !slices.Contains(stream.ToneMapAlgorithms, *m) {
			writeError(w, http.StatusBadRequest, "toneMapping must be hable, mobius, reinhard or off")
			return
		}
		if err := s.Store.SetSetting(ctx, settingToneMapping, *m); err != nil {
			s.serverError(w, err)
			return
		}
	}
	if t := in.Theme; t != nil && *t != "" {
		if themes, _ := s.themes(); !hasTheme(themes, *t) {
			writeError(w, http.StatusBadRequest, "no such theme")
			return
		}
		if err := s.Store.SetSetting(ctx, settingTheme, *t); err != nil {
			s.serverError(w, err)
			return
		}
	}
	if m := in.LibraryWatch; m != nil && *m != "" {
		mode := scan.WatchMode(*m)
		if mode != scan.WatchAuto && mode != scan.WatchPoll && mode != scan.WatchOff {
			writeError(w, http.StatusBadRequest, "libraryWatch must be auto, poll or off")
			return
		}
		switch {
		case s.Watcher != nil:
			if err := s.Watcher.SetMode(ctx, mode); err != nil {
				s.serverError(w, err)
				return
			}
		case mode != scan.WatchOff:
			writeError(w, http.StatusBadRequest, "this server doesn't watch library folders")
			return
		}
	}
	if in.RemoteTrailers != nil {
		if err := s.Store.SetSetting(ctx, settingRemoteTrailers, strconv.FormatBool(*in.RemoteTrailers)); err != nil {
			s.serverError(w, err)
			return
		}
	}
	if in.QualitySteps != nil {
		for _, id := range *in.QualitySteps {
			if _, ok := stream.StepByID(id); !ok {
				writeError(w, http.StatusBadRequest, "qualitySteps must be quality steps")
				return
			}
		}
		if len(*in.QualitySteps) == 0 {
			writeError(w, http.StatusBadRequest, "pick at least one quality step")
			return
		}
		if err := s.Store.SetSetting(ctx, settingQualitySteps, strings.Join(*in.QualitySteps, ",")); err != nil {
			s.serverError(w, err)
			return
		}
	}
	if c := in.RemoteQualityCap; c != nil {
		if _, ok := stream.StepByID(*c); !ok && *c != "" {
			writeError(w, http.StatusBadRequest, "remoteQualityCap must be a quality step or empty")
			return
		}
		if err := s.Store.SetSetting(ctx, settingRemoteQualityCap, *c); err != nil {
			s.serverError(w, err)
			return
		}
	}
	if in.AdaptiveQuality != nil {
		if err := s.Store.SetSetting(ctx, settingAdaptiveQuality, strconv.FormatBool(*in.AdaptiveQuality)); err != nil {
			s.serverError(w, err)
			return
		}
	}
	if in.BoxOfficeAsksNeed != nil {
		if err := s.Store.SetSetting(ctx, settingAsksNeedApproval, strconv.FormatBool(*in.BoxOfficeAsksNeed)); err != nil {
			s.serverError(w, err)
			return
		}
	}
	if in.HolidayRows != nil {
		if err := s.Store.SetSetting(ctx, settingHolidayRows, strconv.FormatBool(*in.HolidayRows)); err != nil {
			s.serverError(w, err)
			return
		}
	}
	if in.OnlineLyrics != nil {
		if err := s.setOnlineLyrics(ctx, *in.OnlineLyrics); err != nil {
			s.serverError(w, err)
			return
		}
	}
	if in.SubtitleSync != nil {
		v := ""
		if !*in.SubtitleSync {
			v = "off"
		}
		if err := s.Store.SetSetting(ctx, settingSubtitleSync, v); err != nil {
			s.serverError(w, err)
			return
		}
	}
	if in.RemoteForwardedPort != nil {
		if !validForwardedPort(*in.RemoteForwardedPort) {
			writeError(w, http.StatusBadRequest, "remoteForwardedPort must be 443, 80 or 0")
			return
		}
		if err := s.setRemoteForwardedPort(ctx, *in.RemoteForwardedPort); err != nil {
			s.serverError(w, err)
			return
		}
	}
	if in.RemoteAccess != nil {
		if err := s.setRemoteAccess(ctx, *in.RemoteAccess); err != nil {
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
