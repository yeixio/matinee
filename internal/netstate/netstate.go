// Package netstate keeps Matinee quick when the internet is down. Every
// call the server makes to a site outside goes through Tracker's
// RoundTripper, which remembers the sites that didn't answer and fails
// further calls to them at once, instead of each one waiting out its
// timeout, until it's time to try again. Pages, scans and playback never
// wait on a site that's known to be down, and the server's health page can
// say "No internet connection" instead of listing every failed lookup.
package netstate

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"
)

// Marker is in the text of every error for a call that wasn't made
// because its site is down, so logs and the health page can tell them apart.
const Marker = "waiting for the connection to come back"

// DownError is a call that wasn't made because its site didn't answer lately.
type DownError struct {
	Host  string
	Until time.Time // when it's tried again
}

func (e *DownError) Error() string {
	return fmt.Sprintf("%s isn't answering; %s", e.Host, Marker)
}

// ErrDown matches a DownError with errors.Is.
var ErrDown = errors.New(Marker)

func (e *DownError) Is(target error) bool { return target == ErrDown }

// Retry times: the first wait after a failure, and the longest.
const (
	firstWait = 30 * time.Second
	maxWait   = 5 * time.Minute
)

type host struct {
	since   time.Time // down since
	until   time.Time // tried again from then
	wait    time.Duration
	err     string
	trying  bool // a call is trying it again now
	private bool // on the home network, not the internet
}

// Tracker remembers which sites are down.
type Tracker struct {
	// Back runs, in its own goroutine, when the internet answers again
	// after being down, so work that waited can catch up.
	Back func()
	// FirstWait is how long a site that stopped answering is left alone
	// before it's tried again; the wait doubles each time, up to MaxWait.
	// Zero is 30 seconds, and five minutes.
	FirstWait, MaxWait time.Duration

	mu       sync.Mutex
	hosts    map[string]*host
	lastOK   time.Time // the last answer from a site on the internet
	lastFail time.Time // the last site on the internet found down
	now      func() time.Time
}

func New() *Tracker {
	return &Tracker{hosts: map[string]*host{}, now: time.Now}
}

// Wrap returns a RoundTripper that sends calls on with rt unless their
// site is down.
func (t *Tracker) Wrap(rt http.RoundTripper) http.RoundTripper {
	return roundTripper{t: t, next: rt}
}

type roundTripper struct {
	t    *Tracker
	next http.RoundTripper
}

func (r roundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	name := req.URL.Hostname()
	if err := r.t.allow(name); err != nil {
		if req.Body != nil {
			req.Body.Close()
		}
		return nil, err
	}
	res, err := r.next.RoundTrip(req)
	switch {
	case err == nil:
		r.t.answered(name)
	case unreachable(req.Context(), err):
		r.t.failed(name, err)
	default:
		r.t.release(name)
	}
	return res, err
}

// unreachable reports whether err means the site couldn't be reached at
// all, as opposed to the caller giving up or the site answering badly.
func unreachable(ctx context.Context, err error) bool {
	if errors.Is(err, context.Canceled) && ctx.Err() == context.Canceled {
		return false // whoever asked went away
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var ne net.Error
	if errors.As(err, &ne) {
		return true
	}
	var dns *net.DNSError
	var op *net.OpError
	return errors.As(err, &dns) || errors.As(err, &op)
}

// allow says whether a call to a site may go ahead. A site that's down
// gets one call through once its wait is over; the others fail at once.
func (t *Tracker) allow(name string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	h := t.hosts[name]
	if h == nil {
		return nil
	}
	if t.now().Before(h.until) || h.trying {
		return &DownError{Host: name, Until: h.until}
	}
	h.trying = true
	return nil
}

func (t *Tracker) release(name string) {
	t.mu.Lock()
	if h := t.hosts[name]; h != nil {
		h.trying = false
	}
	t.mu.Unlock()
}

func (t *Tracker) answered(name string) {
	t.mu.Lock()
	wasOffline := t.offline()
	_, wasDown := t.hosts[name]
	delete(t.hosts, name)
	if !isPrivate(name) {
		t.lastOK = t.now()
	}
	// It's back when a site that was down answers. Another site answering
	// while one is down only shows that one site is down.
	back := wasOffline && wasDown && !t.offline()
	if back {
		// The internet is back, so the other sites on it most likely are
		// too: they may be tried again right away rather than after their
		// wait, so what Back catches up on isn't turned away. One that's
		// really down is found so again on its first try.
		now := t.now()
		for _, h := range t.hosts {
			if !h.private && now.Before(h.until) {
				h.until = now
			}
		}
	}
	back = back && t.Back != nil
	t.mu.Unlock()
	if back {
		go t.Back()
	}
}

func (t *Tracker) failed(name string, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	h := t.hosts[name]
	if h == nil {
		h = &host{since: now, wait: cmp.Or(t.FirstWait, firstWait), private: isPrivate(name)}
		t.hosts[name] = h
	} else {
		limit := cmp.Or(t.MaxWait, maxWait)
		h.wait = min(h.wait*2, max(limit, h.wait))
	}
	h.until, h.err, h.trying = now.Add(h.wait), err.Error(), false
	if !h.private {
		t.lastFail = now
	}
}

// offline: the latest word from the internet is a failure, and a site on
// it is still down. t.mu is held.
func (t *Tracker) offline() bool {
	if !t.lastFail.After(t.lastOK) {
		return false
	}
	for _, h := range t.hosts {
		if !h.private {
			return true
		}
	}
	return false
}

// Run checks, while the internet is down, whether it's back: once a down
// site's wait is over and nothing else has tried it, it's asked for its
// front page through next (wrapped like every other call), so Back runs
// within a minute or so of the connection returning, even when nothing
// else needed the internet meanwhile.
func (t *Tracker) Run(ctx context.Context, next http.RoundTripper) {
	client := &http.Client{Transport: t.Wrap(next), Timeout: 15 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	tick := time.NewTicker(cmp.Or(t.FirstWait/2, 15*time.Second))
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		name := t.dueForCheck()
		if name == "" {
			continue
		}
		req, err := http.NewRequestWithContext(ctx, "HEAD", "https://"+name+"/", nil)
		if err != nil {
			continue
		}
		if res, err := client.Do(req); err == nil {
			res.Body.Close()
		}
	}
}

// dueForCheck picks a site on the internet that's down and may be tried
// again now, while the internet looks down.
func (t *Tracker) dueForCheck() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.offline() {
		return ""
	}
	now := t.now()
	for name, h := range t.hosts {
		if !h.private && !h.trying && !now.Before(h.until) {
			return name
		}
	}
	return ""
}

// Site is a site that's down.
type Site struct {
	Host  string `json:"host"`
	Since int64  `json:"since"` // milliseconds
	Retry int64  `json:"retry"` // when it's tried again, milliseconds
	Error string `json:"error,omitempty"`
	// Home means it's on the home network, such as Radarr or Home
	// Assistant, rather than on the internet.
	Home bool `json:"home,omitempty"`
}

// Status is how the server's connections are doing.
type Status struct {
	// Offline means sites on the internet stopped answering and none has
	// answered since.
	Offline bool   `json:"offline"`
	Since   int64  `json:"since,omitempty"` // milliseconds, while offline
	Down    []Site `json:"down"`
}

// Status says which sites are down.
func (t *Tracker) Status() Status {
	t.mu.Lock()
	defer t.mu.Unlock()
	st := Status{Offline: t.offline(), Down: []Site{}}
	for name, h := range t.hosts {
		st.Down = append(st.Down, Site{Host: name, Since: h.since.UnixMilli(), Retry: h.until.UnixMilli(), Error: h.err, Home: h.private})
		if st.Offline && !h.private && (st.Since == 0 || h.since.UnixMilli() < st.Since) {
			st.Since = h.since.UnixMilli()
		}
	}
	slices.SortFunc(st.Down, func(a, b Site) int { return strings.Compare(a.Host, b.Host) })
	return st
}

// isPrivate reports whether a host is on the home network: a private,
// loopback or link-local address, or a name with no dots or under .local,
// .lan, .home or .internal.
func isPrivate(name string) bool {
	if ip := net.ParseIP(name); ip != nil {
		return ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast()
	}
	n := strings.TrimSuffix(strings.ToLower(name), ".")
	if !strings.Contains(n, ".") {
		return true
	}
	for _, s := range []string{".local", ".lan", ".home", ".internal", ".home.arpa"} {
		if strings.HasSuffix(n, s) {
			return true
		}
	}
	return false
}
