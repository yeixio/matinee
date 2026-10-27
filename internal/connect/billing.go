package connect

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// DefaultBillingURL is where Matinee Connect is bought: yeix.io opens a
// Stripe checkout for one server and its customer portal.
const DefaultBillingURL = "https://yeix.io"

// Billing is yeix.io's side of buying Matinee Connect.
type Billing struct {
	URL        string
	HTTPClient *http.Client
}

// Plan is a way to pay, as yeix.io offers it.
type Plan struct {
	// ID is monthly or yearly.
	ID string `json:"id"`
	// Price is ready to show, such as "$3.99", and Interval month or year.
	Price    string `json:"price"`
	Interval string `json:"interval"`
}

// Checkout is an open Stripe checkout for a server's subscription.
type Checkout struct {
	URL       string `json:"url"`
	SessionID string `json:"session_id"`
	Expires   int64  `json:"expires"`
}

// Plans lists what can be bought.
func (b *Billing) Plans(ctx context.Context) ([]Plan, error) {
	var out struct {
		Plans []Plan `json:"plans"`
	}
	return out.Plans, b.do(ctx, http.MethodGet, "/api/matinee/connect/plans", nil, &out)
}

// Checkout opens a checkout for server id's subscription on plan.
func (b *Billing) Checkout(ctx context.Context, id, plan string) (Checkout, error) {
	var out Checkout
	err := b.do(ctx, http.MethodPost, "/api/matinee/connect/checkout", map[string]string{"server": id, "plan": plan}, &out)
	if err == nil && (!validSession(out.SessionID) || !strings.HasPrefix(out.URL, "https://")) {
		err = errors.New("billing gave an unexpected checkout")
	}
	return out, err
}

// Portal opens Stripe's customer portal for the subscription session bought.
func (b *Billing) Portal(ctx context.Context, session string) (string, error) {
	var out struct {
		URL string `json:"url"`
	}
	if err := b.do(ctx, http.MethodPost, "/api/matinee/connect/portal", map[string]string{"checkout_session": session}, &out); err != nil {
		return "", err
	}
	if !strings.HasPrefix(out.URL, "https://") {
		return "", errors.New("billing gave an unexpected page")
	}
	return out.URL, nil
}

func (b *Billing) do(ctx context.Context, method, path string, in, out any) error {
	var body *bytes.Reader
	if in != nil {
		j, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(j)
	} else {
		body = bytes.NewReader(nil)
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimSuffix(b.URL, "/")+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	hc := b.HTTPClient
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var e struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		json.NewDecoder(resp.Body).Decode(&e)
		if e.Error.Message != "" {
			return errors.New(e.Error.Message)
		}
		return fmt.Errorf("%s answered %d", b.URL, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
