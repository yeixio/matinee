package connect

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Client is a server's side of the service.
type Client struct {
	URL        string // the service, such as DefaultURL
	Key        ed25519.PrivateKey
	HTTPClient *http.Client
}

// ipv4Client reaches the service over IPv4, the family the server's
// certificate is for, so the service sees the address the apps should use.
var ipv4Client = func() *http.Client {
	t := http.DefaultTransport.(*http.Transport).Clone()
	d := &net.Dialer{Timeout: 10 * time.Second}
	t.DialContext = func(ctx context.Context, _, addr string) (net.Conn, error) { return d.DialContext(ctx, "tcp4", addr) }
	return &http.Client{Transport: t}
}()

// LoadKey reads the server's key from path, or makes and saves one. The
// server's id follows from it, so it's kept for good.
func LoadKey(path string) (ed25519.PrivateKey, error) {
	if b, err := os.ReadFile(path); err == nil {
		if blk, _ := pem.Decode(b); blk != nil && len(blk.Bytes) == ed25519.SeedSize {
			return ed25519.NewKeyFromSeed(blk.Bytes), nil
		}
		return nil, fmt.Errorf("%s isn't a server key", path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	b := pem.EncodeToMemory(&pem.Block{Type: "MATINEE SERVER KEY", Bytes: key.Seed()})
	return key, os.WriteFile(path, b, 0o600)
}

// ID is this server's id.
func (c *Client) ID() string { return ID(c.Key.Public().(ed25519.PublicKey)) }

// LookupURL is where the apps look this server up.
func (c *Client) LookupURL() string {
	return strings.TrimSuffix(c.URL, "/") + "/v1/servers/" + c.ID()
}

// Register tells the service this server is reachable on the router's
// external port, at whatever address the request comes from; port 0 takes
// the registration down.
func (c *Client) Register(ctx context.Context, port int) (Registered, error) {
	payload, err := json.Marshal(registration{Time: time.Now().Unix(), Port: port})
	if err != nil {
		return Registered{}, err
	}
	in := signed{
		Key:     c.Key.Public().(ed25519.PublicKey),
		Payload: payload,
		Sig:     ed25519.Sign(c.Key, append([]byte(signContext), payload...)),
	}
	var out Registered
	return out, c.post(ctx, "/v1/register", in, &out)
}

// Check asks the service for this home's public address and whether it
// can reach the server there on the router's external port.
func (c *Client) Check(ctx context.Context, port int) (netip.Addr, bool, error) {
	var out Checked
	if err := c.post(ctx, "/v1/check", map[string]int{"port": port}, &out); err != nil {
		return netip.Addr{}, false, err
	}
	ip, err := netip.ParseAddr(out.IP)
	return ip, out.Reachable, err
}

func (c *Client) post(ctx context.Context, path string, in, out any) error {
	b, err := json.Marshal(in)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSuffix(c.URL, "/")+path, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	hc := c.HTTPClient
	if hc == nil {
		hc = ipv4Client
	}
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var e struct {
			Error string `json:"error"`
		}
		json.NewDecoder(resp.Body).Decode(&e)
		return fmt.Errorf("%s answered %d %s", c.URL, resp.StatusCode, e.Error)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
