// Package remote makes a home server reachable from the internet without
// router setup: it asks the router to forward a port (UPnP or NAT-PMP),
// gets a Let's Encrypt certificate for the router's public IP address, and
// serves the API over HTTPS there. The apps learn the address while at
// home (GET /api/v1/server) and use it when they're away.
//
// When the router won't open ports, an admin can forward one by hand
// (SetForwardedPort). While it's ready, the server registers its address
// with the connect service, so the apps can find it after the address
// changes (internal/connect). Media never goes through anything but this
// port: when the internet provider shares one public address among
// customers, or a second router is in the way, Status says what to change.
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

	"github.com/yeixio/matinee/internal/connect"
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
	SharedAddress bool `json:"sharedAddress,omitempty"`
	// ForwardedPort is the router port an admin forwarded by hand; 0 means
	// the router is asked to open one.
	ForwardedPort int `json:"forwardedPort,omitempty"`
	// ListenPort is this server's remote access port, where a port
	// forwarded by hand should go.
	ListenPort int `json:"listenPort,omitempty"`
	// Findable means the connect service knows the current address, so
	// the apps find this server even after the address changes.
	Findable bool `json:"findable,omitempty"`
	// Connect is the Matinee Connect subscription that pays for that;
	// nil when the server uses a service that needs none.
	Connect            *ConnectStatus `json:"connect,omitempty"`
	CertificateExpires *time.Time     `json:"certificateExpires,omitempty"`
	CheckedAt          *time.Time     `json:"checkedAt,omitempty"`
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
	// Connect is the service the apps look this server up with; nil for
	// none, which also rules out a port forwarded by hand.
	Connect *connect.Client
	// Billing is where Matinee Connect is bought, for Yeix's service;
	// nil for one that needs no subscription.
	Billing *connect.Billing

	// For tests: the ACME directory (default Let's Encrypt), an HTTP client
	// for it, a router, challenges to try, and accepting private addresses.
	CA           string
	HTTPClient   *http.Client
	FindRouter   func(context.Context) (router, error)
	Challenges   []challenge
	AllowPrivate bool

	mu        sync.Mutex
	enabled   bool
	forwarded int // the router port an admin forwarded by hand; 0 for none
	status    Status
	wake      chan struct{}
	srv       *http.Server
	port      int    // the listener's port
	router    router // nil until found
	mapped    int    // external port forwarded to the listener; 0 if none
	lastFail  time.Time

	subMu      sync.Mutex // guards subscription.json and subProblem
	subProblem string
	subWake    chan struct{}

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

// SetForwardedPort says the admin forwarded the router's port ext (443 or
// 80) to this server's remote access port by hand; 0 goes back to asking
// the router.
func (m *Manager) SetForwardedPort(ext int) {
	m.mu.Lock()
	changed := m.forwarded != ext
	m.forwarded = ext
	if changed {
		m.lastFail = time.Time{}
		if m.enabled {
			m.status = Status{Enabled: true, State: StateWorking}
		}
	}
	m.mu.Unlock()
	if changed {
		m.poke()
	}
}

// Lookup is this server's id with the connect service and the address the
// apps look it up at, or "" without the service.
func (m *Manager) Lookup() (id, url string) {
	if m.Connect == nil {
		return "", ""
	}
	return m.Connect.ID(), m.Connect.LookupURL()
}

// Status reports how remote access is doing.
func (m *Manager) Status() Status {
	m.mu.Lock()
	s := m.status
	if !m.enabled {
		s = Status{State: StateOff}
	}
	m.mu.Unlock()
	s.Connect = m.ConnectStatus()
	return s
}

// URL is the address the apps use away from home, or "" while there's none.
func (m *Manager) URL() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.enabled || m.status.State != StateReady {
		return ""
	}
	return m.status.URL
}

// Findable reports whether the apps can look this server up right now.
func (m *Manager) Findable() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.enabled && m.status.State == StateReady && m.status.Findable
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
	if m.Connect != nil && m.Billing != nil {
		m.subWake = make(chan struct{}, 1)
		go m.runSubscription(ctx)
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
		st.Findable = m.register(ctx, m.mapped, st.URL)
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
	m.mu.Lock()
	forwarded := m.forwarded
	m.mu.Unlock()
	st.ForwardedPort, st.ListenPort = forwarded, m.port
	if fr, ok := m.router.(*forwardedPort); (forwarded != 0) != ok || ok && fr.ext != forwarded {
		m.dropRouter(ctx)
	}
	r := m.router
	if r == nil && forwarded != 0 {
		if m.Connect == nil {
			return st, errors.New("a port forwarded by hand needs the connect service, which this server has turned off (MATINEE_CONNECT_URL)")
		}
		pass := m.pass()
		if m.needsPass() && pass == "" {
			return st, errors.New("checking a port forwarded by hand needs Matinee Connect. Subscribe below, or turn on UPnP or NAT-PMP in the router's settings and set Router port back to asking the router")
		}
		r = &forwardedPort{ext: forwarded, internal: m.port, check: func(ctx context.Context, port int) (netip.Addr, bool, error) {
			return m.Connect.Check(ctx, port, m.pass())
		}}
		m.router = r
	}
	if r == nil {
		find := m.FindRouter
		if find == nil {
			find = findRouter
		}
		var err error
		if r, err = find(ctx); err != nil {
			m.Log.Debug("no router for remote access", "err", err)
			msg := "no router on this network offered to open a port. Turn on UPnP or NAT-PMP in the router's settings"
			if m.Connect != nil {
				msg += ", or forward port 443 to port " + strconv.Itoa(m.port) + " on this computer yourself and pick it under Router port"
			}
			return st, errors.New(msg + ". If Matinee runs in a container, give it the host's network")
		}
		m.router = r
	}
	st.Router = r.Name()
	ip, err := r.ExternalIP(ctx)
	var missing *forwardMissing
	if errors.As(err, &missing) {
		st.PublicIP = missing.ip.String()
		return st, err
	}
	if err != nil {
		m.router = nil
		return st, fmt.Errorf("the router wouldn't say its internet address: %w", err)
	}
	st.PublicIP = ip.String()
	if !m.AllowPrivate && !public(ip) {
		st.SharedAddress = true
		return st, errors.New("this router has no internet address of its own (" + ip.String() + "): the internet provider shares one among customers, or another router sits in front of this one, so the internet can't reach this server directly. Ask the provider for a public address, or put the router in front into bridge mode")
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

// public reports whether ip is reachable from the internet.
func public(ip netip.Addr) bool { return connect.Public(ip) }

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

func url(ip netip.Addr, port int) string { return connect.URL(ip, port) }

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

// register tells the connect service the router's port ext reaches this
// server, and reports whether it worked. The service registers the address
// the request comes from, which should be the one the certificate is for.
func (m *Manager) register(ctx context.Context, ext int, want string) bool {
	if m.Connect == nil {
		return false
	}
	pass := m.pass()
	if m.needsPass() && pass == "" {
		return false
	}
	reg, err := m.Connect.Register(ctx, ext, pass)
	if err != nil {
		m.Log.Warn("couldn't tell the connect service this server's address; the apps find it when they're home", "err", err)
		return false
	}
	if reg.URL != want {
		m.Log.Warn("the connect service sees this server at another address", "service", reg.URL, "certificate", want)
		return false
	}
	return true
}

// dropRouter takes the forward down, for a switch to or from a port
// forwarded by hand.
func (m *Manager) dropRouter(ctx context.Context) {
	if m.router != nil && m.mapped != 0 {
		m.router.Unmap(ctx, m.port, m.mapped)
	}
	m.mapped, m.router = 0, nil
}

// stop takes the forward and the registration down and closes the listener.
func (m *Manager) stop(ctx context.Context) {
	if m.Connect != nil && m.mapped != 0 {
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		if _, err := m.Connect.Register(ctx, 0, ""); err != nil {
			m.Log.Debug("couldn't take the connect registration down; it expires by itself", "err", err)
		}
		cancel()
	}
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
