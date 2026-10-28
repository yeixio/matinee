package api

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/yeixio/matinee/internal/connect"
	"github.com/yeixio/matinee/internal/remote"
)

// Remote access: with it on, the server opens a port on the home router and
// gets a certificate for the router's public address, so the apps reach it
// away from home without anyone setting up the router (internal/remote).
// It's off until an admin turns it on, since it opens a port to the
// internet. A server with its own public address (MATINEE_PUBLIC_URL), such
// as one behind an ingress the internet reaches, doesn't need it.

type remoteKey struct{}

// RemoteHandler serves the same API as Handler, on the remote access port.
// Nothing that arrives there counts as coming from home.
func (s *Server) RemoteHandler() http.Handler {
	h := s.Handler()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), remoteKey{}, true)))
	})
}

// SettingRemoteAccess holds serverSettings.RemoteAccess. The server reads it
// at startup.
const SettingRemoteAccess = "remote_access"

// RemoteAccessOn reads the setting.
func RemoteAccessOn(ctx context.Context, s settingStore) (bool, error) {
	v, err := s.Setting(ctx, SettingRemoteAccess)
	return v == "true", err
}

type settingStore interface {
	Setting(ctx context.Context, key string) (string, error)
}

// SettingRemoteForwardedPort holds serverSettings.RemoteForwardedPort.
const SettingRemoteForwardedPort = "remote_forwarded_port"

// RemoteForwardedPort reads the setting: the router port an admin forwarded
// to the remote access port by hand, or 0.
func RemoteForwardedPort(ctx context.Context, s settingStore) (int, error) {
	v, err := s.Setting(ctx, SettingRemoteForwardedPort)
	if err != nil || v == "" {
		return 0, err
	}
	return strconv.Atoi(v)
}

// validForwardedPort reports whether the certificate check can come in
// through router port p: Let's Encrypt only checks on 443 and 80.
func validForwardedPort(p int) bool { return p == 0 || p == 443 || p == 80 }

func (s *Server) setRemoteForwardedPort(ctx context.Context, port int) error {
	if err := s.Store.SetSetting(ctx, SettingRemoteForwardedPort, strconv.Itoa(port)); err != nil {
		return err
	}
	if s.Remote != nil {
		s.Remote.SetForwardedPort(port)
	}
	return nil
}

// A public URL isn't taken to work away from home just because it's set:
// many are names that only resolve, or only lead anywhere, at home. Its
// host has to resolve to addresses the internet can route to before it
// stands in for remote access; until then remote access stays offered
// alongside it.

// publicCheck caches whether PublicURL's host resolved to public addresses.
type publicCheck struct {
	mu     sync.Mutex
	at     time.Time
	public bool
}

const publicCheckEvery = 10 * time.Minute

// publicURLIsPublic reports whether PublicURL is set and its host resolves
// only to public addresses.
func (s *Server) publicURLIsPublic(ctx context.Context) bool {
	if s.PublicURL == "" {
		return false
	}
	c := &s.publicCheck
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.at.IsZero() && time.Since(c.at) < publicCheckEvery {
		return c.public
	}
	c.public = s.resolvesPublic(ctx)
	c.at = time.Now()
	return c.public
}

func (s *Server) resolvesPublic(ctx context.Context) bool {
	u, err := url.Parse(s.PublicURL)
	if err != nil || u.Hostname() == "" {
		return false
	}
	host := u.Hostname()
	if ip, err := netip.ParseAddr(host); err == nil {
		return connect.Public(ip)
	}
	lookup := s.LookupHost
	if lookup == nil {
		lookup = net.DefaultResolver.LookupHost
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	addrs, err := lookup(ctx, host)
	if err != nil || len(addrs) == 0 {
		return false
	}
	for _, a := range addrs {
		ip, err := netip.ParseAddr(a)
		if err != nil || !connect.Public(ip) {
			return false
		}
	}
	return true
}

// lookup is this server's id with the connect service and where the apps
// look it up, while the service knows its address; both "" otherwise.
func (s *Server) lookup(ctx context.Context) (id, url string) {
	if s.publicURLIsPublic(ctx) || s.Remote == nil || !s.Remote.Findable() {
		return "", ""
	}
	return s.Remote.Lookup()
}

// remoteURL is where the apps reach this server away from home, or "".
func (s *Server) remoteURL(ctx context.Context) string {
	if s.publicURLIsPublic(ctx) {
		return s.PublicURL
	}
	if s.Remote != nil {
		if u := s.Remote.URL(); u != "" {
			return u
		}
	}
	// Maybe it works through a VPN or tunnel the apps also use.
	return s.PublicURL
}

// remoteReady reports whether remoteURL is known to work away from home.
func (s *Server) remoteReady(ctx context.Context) bool {
	return s.publicURLIsPublic(ctx) || s.Remote != nil && s.Remote.URL() != ""
}

func (s *Server) setRemoteAccess(ctx context.Context, on bool) error {
	if err := s.Store.SetSetting(ctx, SettingRemoteAccess, strconv.FormatBool(on)); err != nil {
		return err
	}
	if s.Remote != nil {
		s.Remote.SetEnabled(on)
	}
	return nil
}

func (s *Server) remoteStatus() *remote.Status {
	if s.Remote == nil {
		return nil
	}
	st := s.Remote.Status()
	return &st
}

// Matinee Connect, the paid service that lets the apps find this server
// after its address changes, is bought here (internal/remote/subscription.go).

func (s *Server) connectPlans(w http.ResponseWriter, r *http.Request) {
	if s.Remote == nil {
		writeError(w, http.StatusNotFound, "this server doesn't offer remote access")
		return
	}
	plans, err := s.Remote.Plans(r.Context())
	if err != nil {
		s.connectError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"plans": plans})
}

func (s *Server) connectSubscribe(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Plan string `json:"plan"`
	}
	if err := readJSON(r, &in); err != nil || (in.Plan != "monthly" && in.Plan != "yearly") {
		writeError(w, http.StatusBadRequest, "plan must be monthly or yearly")
		return
	}
	if s.Remote == nil {
		writeError(w, http.StatusNotFound, "this server doesn't offer remote access")
		return
	}
	url, err := s.Remote.Subscribe(r.Context(), in.Plan)
	if err != nil {
		s.connectError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"url": url})
}

func (s *Server) connectManage(w http.ResponseWriter, r *http.Request) {
	if s.Remote == nil {
		writeError(w, http.StatusNotFound, "this server doesn't offer remote access")
		return
	}
	url, err := s.Remote.ManageURL(r.Context())
	if err != nil {
		s.connectError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"url": url})
}

func (s *Server) connectError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, remote.ErrNoBilling):
		writeError(w, http.StatusNotFound, "this server doesn't use Matinee Connect")
	case errors.Is(err, remote.ErrNoSubscriber):
		writeError(w, http.StatusNotFound, "this server has no Matinee Connect subscription bought in its settings")
	default:
		s.Log.Warn("Matinee Connect billing", "err", err)
		writeError(w, http.StatusBadGateway, "billing couldn't be reached; try again in a minute")
	}
}
