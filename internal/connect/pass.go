package connect

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// The service Yeix hosts is a paid subscription, bought for one server
// from its Settings through yeix.io's Stripe checkout. The server trades
// the paid Checkout Session with the service for a pass, which it sends
// with each registration and port check; the service turns away servers
// without one. Every period is paid before it starts, so a pass lasts to
// the end of the paid period and a few days more for the renewal to go
// through. A service run without a Stripe key needs no pass, for people
// who run their own.

// Product is the Checkout Session metadata yeix.io marks a Matinee Connect
// subscription with; the session's server metadata is the server's id.
const Product = "matinee-connect"

// grace is how long a pass outlasts the paid period, for a renewal's
// payment to go through.
const grace = 3 * 24 * time.Hour

// pass is what a pass vouches for.
type pass struct {
	ID    string `json:"id"`
	Until int64  `json:"until"`
}

// makePass signs p with key: v1.<payload>.<mac>, both base64url.
func makePass(key []byte, p pass) string {
	b, _ := json.Marshal(p)
	payload := base64.RawURLEncoding.EncodeToString(b)
	return "v1." + payload + "." + base64.RawURLEncoding.EncodeToString(mac(key, payload))
}

// checkPass reports whether s is a pass key signed for id that's still good.
func checkPass(key []byte, s, id string, now time.Time) bool {
	parts := strings.Split(s, ".")
	if len(parts) != 3 || parts[0] != "v1" {
		return false
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || !hmac.Equal(sig, mac(key, parts[1])) {
		return false
	}
	b, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return false
	}
	var p pass
	return json.Unmarshal(b, &p) == nil && p.ID == id && now.Unix() < p.Until
}

func mac(key []byte, payload string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte("matinee-connect pass\n" + payload))
	return h.Sum(nil)
}

// Stripe reads Checkout Sessions, with a restricted key that can read
// Checkout Sessions and Subscriptions.
type Stripe struct {
	Key        string
	BaseURL    string // default https://api.stripe.com
	HTTPClient *http.Client
}

// Why a session doesn't buy a pass.
var (
	errNotPaid  = errors.New("the checkout isn't paid yet")
	errNotFound = errors.New("no Matinee Connect checkout for this server")
	errEnded    = errors.New("the subscription has ended")
)

type checkoutSession struct {
	Status        string            `json:"status"`
	PaymentStatus string            `json:"payment_status"`
	Metadata      map[string]string `json:"metadata"`
	Subscription  *struct {
		Status string `json:"status"`
		// Older API versions keep the period on the subscription, newer
		// ones on its items.
		CurrentPeriodStart int64 `json:"current_period_start"`
		CurrentPeriodEnd   int64 `json:"current_period_end"`
		Items              struct {
			Data []struct {
				CurrentPeriodStart int64 `json:"current_period_start"`
				CurrentPeriodEnd   int64 `json:"current_period_end"`
			} `json:"data"`
		} `json:"items"`
	} `json:"subscription"`
}

// paidUntil reads the Checkout Session and says until when the server id
// it was bought for is paid up, grace included.
func (s *Stripe) paidUntil(ctx context.Context, session, id string) (time.Time, error) {
	base := s.BaseURL
	if base == "" {
		base = "https://api.stripe.com"
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	u := base + "/v1/checkout/sessions/" + url.PathEscape(session) + "?expand[]=subscription"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return time.Time{}, err
	}
	req.Header.Set("Authorization", "Bearer "+s.Key)
	hc := s.HTTPClient
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return time.Time{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return time.Time{}, errNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return time.Time{}, fmt.Errorf("stripe answered %d", resp.StatusCode)
	}
	var cs checkoutSession
	if err := json.NewDecoder(resp.Body).Decode(&cs); err != nil {
		return time.Time{}, err
	}
	if cs.Metadata["product"] != Product || cs.Metadata["server"] != id {
		return time.Time{}, errNotFound
	}
	if cs.Status != "complete" || cs.PaymentStatus != "paid" || cs.Subscription == nil {
		return time.Time{}, errNotPaid
	}
	sub := cs.Subscription
	start, end := sub.CurrentPeriodStart, sub.CurrentPeriodEnd
	if len(sub.Items.Data) > 0 {
		start, end = sub.Items.Data[0].CurrentPeriodStart, sub.Items.Data[0].CurrentPeriodEnd
	}
	switch sub.Status {
	case "active", "trialing":
		return time.Unix(end, 0).Add(grace), nil
	case "past_due":
		// The period has moved on but its renewal isn't paid: only the
		// grace counts, from the period's start.
		return time.Unix(start, 0).Add(grace), nil
	default:
		return time.Time{}, errEnded
	}
}
