package connect

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"
)

// Service answers servers and apps. It keeps everything in memory: servers
// register again every few minutes, so a restart forgets nothing for long.
type Service struct {
	// ClientIPHeader names the header a proxy in front puts the client's
	// address in, such as X-Real-IP. Empty uses the connection's address.
	ClientIPHeader string
	// Dial opens the reachability check's connection; nil uses net.Dialer.
	Dial func(ctx context.Context, network, addr string) (net.Conn, error)
	// AllowPrivate accepts private addresses, for tests.
	AllowPrivate bool
	// Now is the clock, for tests.
	Now func() time.Time

	mu      sync.Mutex
	servers map[string]*entry
	limits  map[limitKey]int
	window  time.Time
}

type entry struct {
	urls    []stamped // most recent first, one per address family
	updated time.Time // of the last registration
	signed  int64     // the last registration's Time, which only goes up
}

type stamped struct {
	url  string
	ipv6 bool
	at   time.Time
}

// How long a registration lasts; servers register every ten minutes.
const ttl = 30 * time.Minute

// The most entries kept, so memory stays bounded.
const maxServers = 1_000_000

// How far a registration's clock may be from the service's.
const skew = 5 * time.Minute

// How many of each request one address may make a minute.
var perMinute = map[string]int{"register": 30, "check": 10, "lookup": 60}

type limitKey struct {
	kind string
	ip   netip.Addr
}

// Handler serves the service's API.
func (s *Service) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/register", s.register)
	mux.HandleFunc("POST /v1/check", s.check)
	mux.HandleFunc("GET /v1/servers/{id}", s.lookup)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok\n")) })
	return mux
}

// Run forgets expired registrations until ctx ends.
func (s *Service) Run(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.sweep()
		}
	}
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) sweep() {
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, e := range s.servers {
		if e.prune(now); len(e.urls) == 0 && now.Sub(e.updated) > ttl {
			delete(s.servers, id)
		}
	}
}

func (e *entry) prune(now time.Time) {
	kept := e.urls[:0]
	for _, u := range e.urls {
		if now.Sub(u.at) <= ttl {
			kept = append(kept, u)
		}
	}
	e.urls = kept
}

func (s *Service) register(w http.ResponseWriter, r *http.Request) {
	ip, ok := s.client(w, r, "register")
	if !ok {
		return
	}
	var in signed
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&in); err != nil {
		fail(w, http.StatusBadRequest, "bad request")
		return
	}
	if len(in.Key) != ed25519.PublicKeySize || !ed25519.Verify(in.Key, append([]byte(signContext), in.Payload...), in.Sig) {
		fail(w, http.StatusForbidden, "bad signature")
		return
	}
	var reg registration
	if err := json.Unmarshal(in.Payload, &reg); err != nil || reg.Port < 0 || reg.Port > 65535 {
		fail(w, http.StatusBadRequest, "bad registration")
		return
	}
	now := s.now()
	if d := now.Sub(time.Unix(reg.Time, 0)); d > skew || d < -skew {
		fail(w, http.StatusBadRequest, "clock is off")
		return
	}
	id := ID(in.Key)
	out := Registered{ID: id, TTL: int(ttl.Seconds())}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.servers == nil {
		s.servers = map[string]*entry{}
	}
	e := s.servers[id]
	if e == nil {
		if len(s.servers) >= maxServers {
			fail(w, http.StatusServiceUnavailable, "full")
			return
		}
		e = &entry{}
		s.servers[id] = e
	}
	if reg.Time <= e.signed {
		fail(w, http.StatusConflict, "registration is older than the last one")
		return
	}
	e.signed, e.updated = reg.Time, now
	// The new address replaces any other of its family; a port of 0 only
	// takes that one down.
	kept := []stamped{}
	if reg.Port != 0 {
		out.URL = URL(ip, reg.Port)
		kept = append(kept, stamped{url: out.URL, ipv6: ip.Is6(), at: now})
	}
	for _, u := range e.urls {
		if u.ipv6 != ip.Is6() {
			kept = append(kept, u)
		}
	}
	e.urls = kept
	e.prune(now)
	writeJSON(w, out)
}

// check tells the caller its public address and whether the service can
// open a connection to it on a port. It only ever connects to the caller.
func (s *Service) check(w http.ResponseWriter, r *http.Request) {
	ip, ok := s.client(w, r, "check")
	if !ok {
		return
	}
	var in struct {
		Port int `json:"port"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&in); err != nil || in.Port < 1 || in.Port > 65535 {
		fail(w, http.StatusBadRequest, "bad request")
		return
	}
	dial := s.Dial
	if dial == nil {
		dial = (&net.Dialer{}).DialContext
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	out := Checked{IP: ip.String()}
	if c, err := dial(ctx, "tcp", netip.AddrPortFrom(ip, uint16(in.Port)).String()); err == nil {
		c.Close()
		out.Reachable = true
	}
	writeJSON(w, out)
}

func (s *Service) lookup(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.client(w, r, "lookup"); !ok {
		return
	}
	id := strings.ToLower(r.PathValue("id"))
	now := s.now()
	s.mu.Lock()
	e := s.servers[id]
	var out Found
	if e != nil && validID(id) {
		e.prune(now)
		out = Found{ID: id, URLs: []string{}, Updated: e.updated.Unix()}
		for _, u := range e.urls {
			out.URLs = append(out.URLs, u.url)
		}
	}
	s.mu.Unlock()
	if len(out.URLs) == 0 {
		fail(w, http.StatusNotFound, "not registered")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, out)
}

// client finds the request's address and counts it against the limit for
// kind; it answers the request itself when it returns false.
func (s *Service) client(w http.ResponseWriter, r *http.Request, kind string) (netip.Addr, bool) {
	ip, err := s.clientIP(r)
	if err != nil {
		fail(w, http.StatusBadRequest, "no client address")
		return ip, false
	}
	if !s.AllowPrivate && !Public(ip) {
		fail(w, http.StatusForbidden, "not a public address")
		return ip, false
	}
	now := s.now()
	s.mu.Lock()
	if now.Sub(s.window) >= time.Minute || s.limits == nil {
		s.limits, s.window = map[limitKey]int{}, now
	}
	k := limitKey{kind, ip}
	s.limits[k]++
	over := s.limits[k] > perMinute[kind]
	s.mu.Unlock()
	if over {
		w.Header().Set("Retry-After", "60")
		fail(w, http.StatusTooManyRequests, "slow down")
		return ip, false
	}
	return ip, true
}

func (s *Service) clientIP(r *http.Request) (netip.Addr, error) {
	if s.ClientIPHeader != "" {
		// A proxy appends to X-Forwarded-For, so the last address is the
		// one it saw.
		v := r.Header.Get(s.ClientIPHeader)
		if i := strings.LastIndexByte(v, ','); i >= 0 {
			v = v[i+1:]
		}
		ip, err := netip.ParseAddr(strings.TrimSpace(v))
		return ip.Unmap(), err
	}
	ap, err := netip.ParseAddrPort(r.RemoteAddr)
	return ap.Addr().Unmap(), err
}

// Public reports whether ip can be reached from the internet: not private,
// and not in the 100.64.0.0/10 range providers share among customers.
func Public(ip netip.Addr) bool {
	shared := netip.MustParsePrefix("100.64.0.0/10")
	return ip.IsGlobalUnicast() && !ip.IsPrivate() && !shared.Contains(ip)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
