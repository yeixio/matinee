package remote

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/yeixio/matinee/internal/connect"
)

// Matinee Connect, the service that lets the apps find this server after
// its address changes, is a subscription bought for this server from its
// Settings (internal/connect/pass.go). yeix.io opens a Stripe checkout for
// this server's id; once it's paid, the server trades the checkout with
// the service for a pass, and again as the subscription renews. The
// Checkout Session's id is the proof of purchase, so it's kept with the
// server's key. Router-based remote access needs none of this and stays
// free.

// ConnectStatus is what Settings shows about the subscription.
type ConnectStatus struct {
	// State is none, checkout (waiting for payment), active, or ended.
	State string     `json:"state"`
	Until *time.Time `json:"until,omitempty"`
	// Checkout is the open checkout's page, while it's open.
	Checkout string `json:"checkout,omitempty"`
	Problem  string `json:"problem,omitempty"`
}

const (
	ConnectNone     = "none"
	ConnectCheckout = "checkout"
	ConnectActive   = "active"
	ConnectEnded    = "ended"
)

// subscription is what's kept in <dir>/subscription.json.
type subscription struct {
	// The open checkout: its session, page, and when it opened and expires.
	Checkout    string `json:"checkout,omitempty"`
	CheckoutURL string `json:"checkoutURL,omitempty"`
	Opened      int64  `json:"opened,omitempty"`
	Expires     int64  `json:"expires,omitempty"`
	// The paid checkout, the pass it bought, and until when.
	Session string `json:"session,omitempty"`
	Pass    string `json:"pass,omitempty"`
	Until   int64  `json:"until,omitempty"`
	Ended   bool   `json:"ended,omitempty"`
}

// How soon after a checkout opens it's asked about every few seconds, so
// the subscription starts soon after paying; after that, every minute.
const (
	checkoutQuick = 30 * time.Minute
	renewBefore   = 4 * 24 * time.Hour
)

// Errors for Settings.
var (
	ErrNoBilling    = errors.New("this server doesn't use Matinee Connect")
	ErrNoSubscriber = errors.New("this server has no Matinee Connect subscription bought in its settings")
)

func (m *Manager) subPath() string { return filepath.Join(m.Dir, "subscription.json") }

func (m *Manager) loadSub() subscription {
	var s subscription
	if b, err := os.ReadFile(m.subPath()); err == nil {
		json.Unmarshal(b, &s)
	}
	return s
}

// saveSub keeps s; the caller holds subMu.
func (m *Manager) saveSub(s subscription) error {
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(m.Dir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(m.subPath(), b, 0o600)
}

// pass is the current pass, or "" without one.
func (m *Manager) pass() string {
	m.subMu.Lock()
	defer m.subMu.Unlock()
	s := m.loadSub()
	if s.Pass == "" || time.Now().Unix() >= s.Until {
		return ""
	}
	return s.Pass
}

// needsPass reports whether the service is Yeix's paid one.
func (m *Manager) needsPass() bool { return m.Billing != nil }

// ConnectStatus reports the subscription, or nil when the server doesn't
// use the paid service.
func (m *Manager) ConnectStatus() *ConnectStatus {
	if m.Connect == nil || m.Billing == nil {
		return nil
	}
	m.subMu.Lock()
	s := m.loadSub()
	problem := m.subProblem
	m.subMu.Unlock()
	now := time.Now().Unix()
	st := &ConnectStatus{State: ConnectNone, Problem: problem}
	switch {
	case s.Pass != "" && now < s.Until:
		until := time.Unix(s.Until, 0)
		st.State, st.Until = ConnectActive, &until
	case s.Checkout != "" && now < s.Expires:
		st.State, st.Checkout = ConnectCheckout, s.CheckoutURL
	case s.Session != "":
		st.State = ConnectEnded
	}
	return st
}

// Plans lists what Matinee Connect costs.
func (m *Manager) Plans(ctx context.Context) ([]connect.Plan, error) {
	if m.Connect == nil || m.Billing == nil {
		return nil, ErrNoBilling
	}
	return m.Billing.Plans(ctx)
}

// Subscribe opens a checkout for this server on plan and returns its page.
func (m *Manager) Subscribe(ctx context.Context, plan string) (string, error) {
	if m.Connect == nil || m.Billing == nil {
		return "", ErrNoBilling
	}
	co, err := m.Billing.Checkout(ctx, m.Connect.ID(), plan)
	if err != nil {
		return "", err
	}
	m.subMu.Lock()
	s := m.loadSub()
	s.Checkout, s.CheckoutURL, s.Opened, s.Expires = co.SessionID, co.URL, time.Now().Unix(), co.Expires
	err = m.saveSub(s)
	m.subProblem = ""
	m.subMu.Unlock()
	m.pokeSub()
	return co.URL, err
}

// ManageURL opens Stripe's page for changing the card or cancelling.
func (m *Manager) ManageURL(ctx context.Context) (string, error) {
	if m.Connect == nil || m.Billing == nil {
		return "", ErrNoBilling
	}
	m.subMu.Lock()
	s := m.loadSub()
	m.subMu.Unlock()
	if s.Session == "" {
		return "", ErrNoSubscriber
	}
	return m.Billing.Portal(ctx, s.Session)
}

func (m *Manager) pokeSub() {
	select {
	case m.subWake <- struct{}{}:
	default:
	}
}

// runSubscription trades checkouts and renewals for passes until ctx ends.
func (m *Manager) runSubscription(ctx context.Context) {
	for {
		wait := m.tradeSub(ctx)
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-m.subWake:
		case <-t.C:
		}
		t.Stop()
	}
}

// tradeSub does what the subscription needs now and says when to look
// again.
func (m *Manager) tradeSub(ctx context.Context) time.Duration {
	m.subMu.Lock()
	s := m.loadSub()
	m.subMu.Unlock()
	now := time.Now()
	switch {
	case s.Checkout != "" && now.Unix() >= s.Expires:
		m.updateSub(func(s *subscription) { s.Checkout, s.CheckoutURL = "", "" })
		return time.Minute
	case s.Checkout != "":
		p, err := m.Connect.Subscribe(ctx, s.Checkout)
		switch {
		case err == nil:
			m.updateSub(func(s *subscription) {
				s.Session, s.Pass, s.Until, s.Ended = s.Checkout, p.Pass, p.Until, false
				s.Checkout, s.CheckoutURL = "", ""
			})
			m.Log.Info("Matinee Connect subscription started", "until", time.Unix(p.Until, 0))
			// Register right away rather than at the next check.
			m.mu.Lock()
			m.lastFail = time.Time{}
			m.mu.Unlock()
			m.poke()
			return time.Minute
		case errors.Is(err, connect.ErrEnded):
			m.updateSub(func(s *subscription) { s.Checkout, s.CheckoutURL = "", "" })
			return time.Minute
		}
		if !errors.Is(err, connect.ErrNotPaid) {
			m.Log.Debug("couldn't ask about the Matinee Connect checkout", "err", err)
		}
		if now.Sub(time.Unix(s.Opened, 0)) < checkoutQuick {
			return 5 * time.Second
		}
		return time.Minute
	case s.Session != "" && !s.Ended && time.Until(time.Unix(s.Until, 0)) < renewBefore:
		p, err := m.Connect.Subscribe(ctx, s.Session)
		switch {
		case err == nil:
			m.updateSub(func(s *subscription) { s.Pass, s.Until = p.Pass, p.Until })
			if time.Until(time.Unix(p.Until, 0)) < renewBefore {
				// Paid only to the end of the grace: ask again soon.
				return time.Hour
			}
		case errors.Is(err, connect.ErrEnded):
			m.updateSub(func(s *subscription) { s.Ended, s.Pass, s.Until = true, "", 0 })
			m.Log.Info("the Matinee Connect subscription has ended")
		default:
			m.Log.Warn("couldn't renew the Matinee Connect pass", "err", err)
			return time.Hour
		}
		return time.Minute
	case s.Session != "" && !s.Ended:
		return min(time.Until(time.Unix(s.Until, 0))-renewBefore, 12*time.Hour)
	}
	return 12 * time.Hour
}

func (m *Manager) updateSub(f func(*subscription)) {
	m.subMu.Lock()
	defer m.subMu.Unlock()
	s := m.loadSub()
	f(&s)
	if err := m.saveSub(s); err != nil {
		m.Log.Warn("couldn't save the Matinee Connect subscription", "err", err)
	}
}
