package remote

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yeixio/matinee/internal/connect"
)

// TestSubscription buys Matinee Connect through pretend billing and
// Stripe: the checkout is unpaid at first, then paid, and the server gets
// a pass the service accepts.
func TestSubscription(t *testing.T) {
	key, err := connect.LoadKey(filepath.Join(t.TempDir(), "server.key"))
	if err != nil {
		t.Fatal(err)
	}
	id := (&connect.Client{Key: key}).ID()
	var paid atomic.Bool
	end := time.Now().Add(30 * 24 * time.Hour).Unix()
	stripe := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/cs_test1") {
			http.NotFound(w, r)
			return
		}
		status, payment := "open", "unpaid"
		if paid.Load() {
			status, payment = "complete", "paid"
		}
		fmt.Fprintf(w, `{"status":%q,"payment_status":%q,"metadata":{"product":"matinee-connect","server":%q},
			"subscription":{"status":"active","current_period_start":%d,"current_period_end":%d}}`, status, payment, id, time.Now().Unix(), end)
	}))
	defer stripe.Close()
	svc := &connect.Service{AllowPrivate: true, Stripe: &connect.Stripe{Key: "rk", BaseURL: stripe.URL}, PassKey: []byte("secret")}
	service := httptest.NewServer(svc.Handler())
	defer service.Close()
	var asked string
	billing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/matinee/connect/checkout":
			b, _ := io.ReadAll(r.Body)
			asked = string(b)
			fmt.Fprintf(w, `{"url":"https://checkout.example/pay","session_id":"cs_test1","expires":%d}`, time.Now().Add(time.Hour).Unix())
		case "/api/matinee/connect/portal":
			io.WriteString(w, `{"url":"https://billing.example/manage"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer billing.Close()

	m := &Manager{Dir: t.TempDir(), Log: slog.New(slog.DiscardHandler),
		Connect: &connect.Client{URL: service.URL, Key: key}, Billing: &connect.Billing{URL: billing.URL}, subWake: make(chan struct{}, 1)}
	ctx := context.Background()
	if st := m.ConnectStatus(); st.State != ConnectNone {
		t.Fatalf("before: %+v", st)
	}
	if _, err := m.ManageURL(ctx); err != ErrNoSubscriber {
		t.Fatalf("manage before buying: %v", err)
	}
	url, err := m.Subscribe(ctx, "yearly")
	if err != nil || url != "https://checkout.example/pay" || !strings.Contains(asked, id) || !strings.Contains(asked, "yearly") {
		t.Fatalf("subscribe: %q %v, asked %s", url, err, asked)
	}
	// Not paid yet: asked again in a few seconds.
	if wait := m.tradeSub(ctx); wait != 5*time.Second || m.ConnectStatus().State != ConnectCheckout || m.pass() != "" {
		t.Fatalf("unpaid: %v %+v", wait, m.ConnectStatus())
	}
	paid.Store(true)
	m.tradeSub(ctx)
	st := m.ConnectStatus()
	if st.State != ConnectActive || st.Until == nil || st.Until.Unix() != time.Unix(end, 0).Add(3*24*time.Hour).Unix() || m.pass() == "" {
		t.Fatalf("paid: %+v", st)
	}
	if _, err := m.Connect.Register(ctx, 7443, m.pass()); err != nil {
		t.Fatalf("register with the pass: %v", err)
	}
	if u, err := m.ManageURL(ctx); err != nil || u != "https://billing.example/manage" {
		t.Fatalf("manage: %q %v", u, err)
	}
	// Nothing to do until the renewal is near.
	if wait := m.tradeSub(ctx); wait < 12*time.Hour-time.Minute {
		t.Fatalf("next look in %v", wait)
	}
}
