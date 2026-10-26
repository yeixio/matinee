// Package remote makes a home server reachable from the internet without
// router setup: it asks the router to forward a port (UPnP or NAT-PMP),
// gets a Let's Encrypt certificate for the router's public IP address, and
// serves the API over HTTPS there. The apps learn the address while at
// home (GET /api/v1/server) and use it when they're away.
//
// It can't help when the internet provider shares one public address
// among customers, or with a second router in the way; Status says so.
package remote

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Status is what Settings shows about remote access.
type Status struct {
	Enabled bool `json:"enabled"`
	// State is off, working (setting up), ready, or failed (see Problem).
	State string `json:"state"`
	// URL is the address the apps use away from home.
	URL string `json:"url,omitempty"`
	// Router is how the port was opened: UPnP or NAT-PMP.
	Router   string `json:"router,omitempty"`
	PublicIP string `json:"publicIP,omitempty"`
	// Problem says, for people, what stands in the way.
	Problem string `json:"problem,omitempty"`
	// SharedAddress means the router has no public address of its own (the
	// provider shares one, or another router sits in front), which only a
	// relay can get around.
	SharedAddress      bool       `json:"sharedAddress,omitempty"`
	CertificateExpires *time.Time `json:"certificateExpires,omitempty"`
	CheckedAt          *time.Time `json:"checkedAt,omitempty"`
}

const (
	StateOff     = "off"
	StateWorking = "working"
	StateReady   = "ready"
	StateFailed  = "failed"
)

// Manager keeps remote access going while it's on.
type Manager struct {
	Addr    string       // the HTTPS listen address, such as :7443
	Dir     string       // keeps the certificate and the CA account key
	Handler http.Handler // serves the API
	Log     *slog.Logger

	// For tests: the ACME directory (default Let's Encrypt), an HTTP client
	// for it, a router, challenges to try, and accepting private addresses.
	CA           string
	HTTPClient   *http.Client
	FindRouter   func(context.Context) (router, error)
	Challenges   []challenge
	AllowPrivate bool

	mu       sync.Mutex
	enabled  bool
	status   Status
	wake     chan struct{}
	srv      *http.Server
	port     int    // the listener's port
	router   router // nil until found
	mapped   int    // external port forwarded to the listener; 0 if none
	lastFail time.Time

	cert            atomic.Pointer[tls.Certificate]
	alpnCert        atomic.Pointer[tls.Certificate]
	httpToken       atomic.Pointer[httpAnswer]
	failedChallenge atomic.Value
}

// How often a working setup is checked, and how soon a failed one is
// tried again. The CA allows only a few failed checks an hour.
const (
	checkEvery   = 10 * time.Minute
	retryAfter   = 30 * time.Minute
	mappingLease = time.Hour
)

// SetEnabled turns remote access on or off; Run does the work.
func (m *Manager) SetEnabled(on bool) {
	m.mu.Lock()
	changed := m.enabled != on
	m.enabled = on
	if changed {
		m.lastFail = time.Time{}
		if on {
			m.status = Status{Enabled: true, State: StateWorking}
		}
	}
	m.mu.Unlock()
	if changed {
		m.poke()
	}
}

func (m *Manager) poke() {
	m.mu.Lock()
	if m.wake == nil {
		m.wake = make(chan struct{}, 1)
	}
	w := m.wake
	m.mu.Unlock()
	select {
	case w <- struct{}{}:
	default:
	}
}

// Status reports how remote access is doing.
func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.status
	if !m.enabled {
		s = Status{State: StateOff}
	}
	return s
}

// URL is the address the apps use away from home, or "" while there's none.
func (m *Manager) URL() string {
	s := m.Status()
	if s.State != StateReady {
		return ""
	}
	return s.URL
}

// Run keeps remote access up while it's on, until ctx ends; then it takes
// the router's forward down.
func (m *Manager) Run(ctx context.Context) {
	m.poke()
	m.mu.Lock()
	wake := m.wake
	m.mu.Unlock()
	if c := m.loadCert(); c != nil {
		m.cert.Store(c)
	}
	t := time.NewTimer(checkEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			m.stop(context.Background())
			return
		case <-wake:
		case <-t.C:
		}
		m.mu.Lock()
		on := m.enabled
		m.mu.Unlock()
		if on {
			m.check(ctx)
		} else {
			m.stop(ctx)
		}
		t.Reset(checkEvery)
	}
}

// check sets up whatever is missing: the listener, the router's forward,
// and a current certificate for the public address.
func (m *Manager) check(ctx context.Context) {
	m.mu.Lock()
	if !m.lastFail.IsZero() && time.Since(m.lastFail) < retryAfter {
		m.mu.Unlock()
		return
	}
	m.mu.Unlock()
	st, err := m.setUp(ctx)
	now := time.Now()
	st.Enabled, st.CheckedAt = true, &now
	if err != nil {
		st.State, st.Problem = StateFailed, sentence(err.Error())
		m.Log.Warn("remote access isn't working", "err", err)
	} else {
		st.State = StateReady
	}
	m.mu.Lock()
	if err != nil {
		m.lastFail = now
	} else {
		m.lastFail = time.Time{}
	}
	if m.enabled {
		m.status = st
	}
	m.mu.Unlock()
}

func (m *Manager) setUp(ctx context.Context) (Status, error) {
	var st Status
	if err := m.listen(); err != nil {
		return st, fmt.Errorf("couldn't listen on %s: %w", m.Addr, err)
	}
	r := m.router
	if r == nil {
		find := m.FindRouter
		if find == nil {
			find = findRouter
		}
		var err error
		if r, err = find(ctx); err != nil {
			m.Log.Debug("no router for remote access", "err", err)
			return st, errors.New("no router on this network offered to open a port. Turn on UPnP or NAT-PMP in the router's settings. If Matinee runs in a container, give it the host's network")
		}
		m.router = r
	}
	st.Router = r.Name()
	ip, err := r.ExternalIP(ctx)
	if err != nil {
		m.router = nil
		return st, fmt.Errorf("the router wouldn't say its internet address: %w", err)
	}
	st.PublicIP = ip.String()
	if !m.AllowPrivate && !public(ip) {
		st.SharedAddress = true
		return st, errors.New("this router has no internet address of its own (" + ip.String() + "): the internet provider shares one among customers, or another router sits in front of this one, so the internet can't reach this server directly")
	}
	want := m.mapped
	if want == 0 {
		want = m.port
	}
	got, err := r.Map(ctx, m.port, want, mappingLease)
	if err != nil {
		m.router = nil
		return st, fmt.Errorf("the router wouldn't forward port %d: %w", want, err)
	}
	if m.mapped != 0 && got != m.mapped {
		r.Unmap(ctx, m.port, m.mapped)
	}
	m.mapped = got
	if c := m.cert.Load(); !current(c, ip) {
		m.Log.Info("getting a certificate for remote access", "ip", ip)
		if c, err = m.obtain(ctx, r, ip); err != nil {
			return st, err
		}
		m.cert.Store(c)
		m.Log.Info("remote access is ready", "url", url(ip, got), "expires", c.Leaf.NotAfter)
	}
	exp := m.cert.Load().Leaf.NotAfter
	st.CertificateExpires = &exp
	st.URL = url(ip, got)
	return st, nil
}

// current reports whether c is for ip and has a third of its life left,
// which for a six-day certificate is a couple of days of retries.
func current(c *tls.Certificate, ip netip.Addr) bool {
	if c == nil || c.Leaf == nil {
		return false
	}
	match := false
	for _, a := range c.Leaf.IPAddresses {
		if b, ok := netip.AddrFromSlice(a); ok && b.Unmap() == ip {
			match = true
		}
	}
	life := c.Leaf.NotAfter.Sub(c.Leaf.NotBefore)
	return match && time.Until(c.Leaf.NotAfter) > life/3
}

// public reports whether ip is reachable from the internet: not private,
// and not in the 100.64.0.0/10 range providers share among customers.
func public(ip netip.Addr) bool {
	shared := netip.MustParsePrefix("100.64.0.0/10")
	return ip.IsGlobalUnicast() && !ip.IsPrivate() && !shared.Contains(ip)
}

// sentence turns an error into something to show people.
func sentence(s string) string {
	if s == "" {
		return s
	}
	s = strings.ToUpper(s[:1]) + s[1:]
	if !strings.HasSuffix(s, ".") {
		s += "."
	}
	return s
}

func url(ip netip.Addr, port int) string {
	if port == 443 {
		return "https://" + ip.String()
	}
	return "https://" + net.JoinHostPort(ip.String(), strconv.Itoa(port))
}

func (m *Manager) listen() error {
	if m.srv != nil {
		return nil
	}
	l, err := net.Listen("tcp", m.Addr)
	if err != nil {
		return err
	}
	m.port = l.Addr().(*net.TCPAddr).Port
	challenge := m.challengeHandler()
	m.srv = &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.TLS == nil {
				challenge.ServeHTTP(w, r)
				return
			}
			m.Handler.ServeHTTP(w, r)
		}),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go m.srv.Serve(newMixedListener(l, m.tlsConfig()))
	return nil
}

// stop takes the forward down and closes the listener.
func (m *Manager) stop(ctx context.Context) {
	if m.router != nil && m.mapped != 0 {
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		if err := m.router.Unmap(ctx, m.port, m.mapped); err != nil {
			m.Log.Warn("couldn't remove the router's port forward", "port", m.mapped, "err", err)
		}
		cancel()
	}
	m.mapped, m.router = 0, nil
	if m.srv != nil {
		m.srv.Close()
		m.srv = nil
	}
}
