package api

import (
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Signing in is open to the internet once remote access is on, so wrong
// passwords slow down whoever keeps trying. Each username, and each address
// a request comes straight from, gets a few free tries; after that every
// wrong password locks it for twice as long as the last time, up to
// signInMaxLock. A right password clears the username and the address.
//
// Requests from home neither count nor get turned away, so a household can
// always sign in at home, even while someone outside is guessing at an
// account; a right password there clears the username too. Behind a proxy
// the address is the proxy's, so only the username counts there.

const (
	signInFreeTries     = 5  // per username
	signInFreeAddrTries = 20 // per address, which may try many usernames
	signInFirstLock     = 30 * time.Second
	signInMaxLock       = 15 * time.Minute
	// signInForget is how long after its last wrong password a username or
	// address starts over.
	signInForget = 24 * time.Hour
)

type signInGuard struct {
	mu   sync.Mutex
	keys map[string]*signInFails
	now  func() time.Time // for tests; nil is time.Now
}

type signInFails struct {
	n     int
	last  time.Time
	until time.Time
}

// signInKeys names what a request's sign-in counts against, and reports
// whether it comes from home, where nothing counts but a right password
// still clears the username.
func (s *Server) signInKeys(r *http.Request, username string) (keys []string, home bool) {
	keys = []string{"user:" + strings.ToLower(strings.TrimSpace(username))}
	if s.fromHome(r) && !crossOrigin(r) {
		return keys, true
	}
	if direct(r) {
		if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
			keys = append(keys, "addr:"+host)
		}
	}
	return keys, false
}

// direct reports whether a request didn't come through a proxy.
func direct(r *http.Request) bool {
	for _, h := range []string{"X-Forwarded-For", "X-Real-Ip", "Forwarded"} {
		if r.Header.Get(h) != "" {
			return false
		}
	}
	return true
}

func (g *signInGuard) clock() time.Time {
	if g.now != nil {
		return g.now()
	}
	return time.Now()
}

// locked reports how long until any of keys may try again.
func (g *signInGuard) locked(keys []string) time.Duration {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := g.clock()
	var wait time.Duration
	for _, k := range keys {
		if f := g.keys[k]; f != nil {
			wait = max(wait, f.until.Sub(now))
		}
	}
	return wait
}

// record counts a sign-in: a right one clears keys, a wrong one counts
// against them.
func (g *signInGuard) record(keys []string, ok bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if ok {
		for _, k := range keys {
			delete(g.keys, k)
		}
		return
	}
	now := g.clock()
	if g.keys == nil {
		g.keys = map[string]*signInFails{}
	}
	if len(g.keys) > 10_000 {
		for k, f := range g.keys {
			if now.Sub(f.last) > signInForget {
				delete(g.keys, k)
			}
		}
	}
	for _, k := range keys {
		f := g.keys[k]
		if f == nil || now.Sub(f.last) > signInForget {
			f = &signInFails{}
			g.keys[k] = f
		}
		f.n++
		f.last = now
		free := signInFreeTries
		if strings.HasPrefix(k, "addr:") {
			free = signInFreeAddrTries
		}
		if over := f.n - free; over >= 0 {
			lock := signInMaxLock
			if over < 10 && signInFirstLock<<over < signInMaxLock {
				lock = signInFirstLock << over
			}
			f.until = now.Add(lock)
		}
	}
}

func tooManySignIns(w http.ResponseWriter, wait time.Duration) {
	w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
	writeError(w, http.StatusTooManyRequests, "too many wrong passwords; try again later")
}
