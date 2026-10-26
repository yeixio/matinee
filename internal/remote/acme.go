package remote

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
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

	"golang.org/x/crypto/acme"

	"github.com/yeixio/matinee/internal/version"
)

// Let's Encrypt gives certificates for bare IP addresses, which last six
// days, so a server with no domain name still gets real HTTPS. The CA
// proves it can reach the server from the internet before it signs, which
// doubles as the outside check that the forwarded port works.

const shortLived = acme.ProfileName("shortlived")

// A challenge is how the CA checks this server answers at the address: on
// external port 443 (tls-alpn-01) or 80 (http-01). Either port may be taken
// on the router, so each is tried in turn.
type challenge struct {
	kind string
	port int
}

var challenges = []challenge{{"tls-alpn-01", 443}, {"http-01", 80}}

// obtain gets a certificate for ip from the CA.
func (m *Manager) obtain(ctx context.Context, r router, ip netip.Addr) (*tls.Certificate, error) {
	key, err := m.accountKey()
	if err != nil {
		return nil, err
	}
	client := &acme.Client{Key: key, DirectoryURL: m.ca(), UserAgent: "matinee/" + version.Version, HTTPClient: m.HTTPClient}
	dir, err := client.Discover(ctx)
	if err != nil {
		return nil, fmt.Errorf("reach %s: %w", m.ca(), err)
	}
	if _, err := client.Register(ctx, &acme.Account{}, acme.AcceptTOS); err != nil && !errors.Is(err, acme.ErrAccountAlreadyExists) {
		return nil, fmt.Errorf("create an account with the certificate authority: %w", err)
	}
	var opts []acme.OrderOption
	if _, ok := dir.Profiles[shortLived]; ok {
		opts = append(opts, acme.WithOrderIssuanceProfile(shortLived))
	}
	order, err := client.AuthorizeOrder(ctx, acme.IPIDs(ip.String()), opts...)
	if err != nil {
		return nil, fmt.Errorf("ask for a certificate: %w", err)
	}
	for _, u := range order.AuthzURLs {
		if err := m.authorize(ctx, client, r, u); err != nil {
			return nil, err
		}
	}
	if order, err = client.WaitOrder(ctx, order.URI); err != nil {
		return nil, err
	}
	certKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	csr, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{IPAddresses: []net.IP{ip.AsSlice()}}, certKey)
	if err != nil {
		return nil, err
	}
	der, _, err := client.CreateCertFromOrder(ctx, order, csr, true)
	if err != nil {
		return nil, fmt.Errorf("collect the certificate: %w", err)
	}
	leaf, err := x509.ParseCertificate(der[0])
	if err != nil {
		return nil, err
	}
	cert := &tls.Certificate{Certificate: der, PrivateKey: certKey, Leaf: leaf}
	if err := m.saveCert(cert); err != nil {
		m.Log.Warn("couldn't save the remote access certificate; it'll be fetched again after a restart", "err", err)
	}
	return cert, nil
}

// authorize proves this server answers at the address, by the first
// challenge whose port the router will forward.
func (m *Manager) authorize(ctx context.Context, client *acme.Client, r router, url string) error {
	z, err := client.GetAuthorization(ctx, url)
	if err != nil {
		return err
	}
	if z.Status == acme.StatusValid {
		return nil
	}
	var problems []string
	for _, c := range m.challengeOrder() {
		var chal *acme.Challenge
		for _, ch := range z.Challenges {
			if ch.Type == c.kind {
				chal = ch
			}
		}
		if chal == nil {
			continue
		}
		done, err := m.openChallenge(ctx, client, r, chal, z.Identifier.Value, c.port)
		if err != nil {
			// The port is taken on the router; the other challenge may do.
			problems = append(problems, fmt.Sprintf("port %d: %v", c.port, err))
			continue
		}
		err = m.answer(ctx, client, chal, url)
		done()
		if err != nil {
			// The authorization is spent once a challenge fails; the next
			// try starts over with the other challenge first.
			m.failedChallenge.Store(c.kind)
			return fmt.Errorf("the certificate authority couldn't reach this server through port %d: %w", c.port, err)
		}
		return nil
	}
	return fmt.Errorf("the router wouldn't forward port 443 or 80 for the certificate check (%s)", strings.Join(problems, "; "))
}

func (m *Manager) answer(ctx context.Context, client *acme.Client, chal *acme.Challenge, url string) error {
	if _, err := client.Accept(ctx, chal); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	_, err := client.WaitAuthorization(ctx, url)
	return err
}

// challengeOrder puts the challenge that failed last time at the back.
func (m *Manager) challengeOrder() []challenge {
	order := m.Challenges
	if order == nil {
		order = challenges
	}
	failed, _ := m.failedChallenge.Load().(string)
	if failed == "" || len(order) < 2 || order[0].kind != failed {
		return order
	}
	return append(append([]challenge{}, order[1:]...), order[0])
}

// openChallenge serves the challenge's answer and forwards the router's
// port to it, until done is called. It listens on a port of its own, since
// NAT-PMP routers keep one forward per internal port.
func (m *Manager) openChallenge(ctx context.Context, client *acme.Client, r router, chal *acme.Challenge, identifier string, ext int) (done func(), err error) {
	l, err := net.Listen("tcp", ":0")
	if err != nil {
		return nil, err
	}
	internal := l.Addr().(*net.TCPAddr).Port
	switch chal.Type {
	case "http-01":
		resp, err := client.HTTP01ChallengeResponse(chal.Token)
		if err != nil {
			l.Close()
			return nil, err
		}
		m.httpToken.Store(&httpAnswer{path: client.HTTP01ChallengePath(chal.Token), body: resp})
	case "tls-alpn-01":
		cert, err := client.TLSALPN01ChallengeCert(chal.Token, identifier)
		if err != nil {
			l.Close()
			return nil, err
		}
		m.alpnCert.Store(&cert)
	}
	srv := &http.Server{Handler: m.challengeHandler(), ReadHeaderTimeout: 10 * time.Second}
	go srv.Serve(newMixedListener(l, m.tlsConfig()))
	got, err := r.Map(ctx, internal, ext, 10*time.Minute)
	if err == nil && got != ext {
		r.Unmap(ctx, internal, got)
		err = fmt.Errorf("the router offered port %d instead", got)
	}
	done = func() {
		r.Unmap(context.WithoutCancel(ctx), internal, ext)
		srv.Close()
		m.httpToken.Store(nil)
		m.alpnCert.Store(nil)
	}
	if err != nil {
		done()
		return nil, err
	}
	return done, nil
}

type httpAnswer struct{ path, body string }

func (m *Manager) challengeHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a := m.httpToken.Load(); a != nil && r.URL.Path == a.path {
			w.Write([]byte(a.body))
			return
		}
		http.NotFound(w, r)
	})
}

// tlsConfig serves the remote certificate, or the tls-alpn-01 answer to the CA.
func (m *Manager) tlsConfig() *tls.Config {
	return &tls.Config{
		NextProtos: []string{"h2", "http/1.1", acme.ALPNProto},
		GetCertificate: func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
			for _, p := range hello.SupportedProtos {
				if p == acme.ALPNProto {
					if c := m.alpnCert.Load(); c != nil {
						return c, nil
					}
					return nil, errors.New("no certificate check in progress")
				}
			}
			if c := m.cert.Load(); c != nil {
				return c, nil
			}
			return nil, errors.New("no certificate yet")
		},
	}
}

func (m *Manager) ca() string {
	if m.CA != "" {
		return m.CA
	}
	return acme.LetsEncryptURL
}

func (m *Manager) accountKey() (crypto.Signer, error) {
	path := filepath.Join(m.Dir, "account.key")
	if b, err := os.ReadFile(path); err == nil {
		if blk, _ := pem.Decode(b); blk != nil {
			return x509.ParseECPrivateKey(blk.Bytes)
		}
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(m.Dir, 0o700); err != nil {
		return nil, err
	}
	return key, os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der}), 0o600)
}

func (m *Manager) certPath() string { return filepath.Join(m.Dir, "certificate.pem") }

func (m *Manager) saveCert(c *tls.Certificate) error {
	der, err := x509.MarshalPKCS8PrivateKey(c.PrivateKey)
	if err != nil {
		return err
	}
	var b []byte
	for _, d := range c.Certificate {
		b = append(b, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: d})...)
	}
	b = append(b, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})...)
	if err := os.MkdirAll(m.Dir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(m.certPath(), b, 0o600)
}

// loadCert reads the certificate a previous run saved, if any.
func (m *Manager) loadCert() *tls.Certificate {
	b, err := os.ReadFile(m.certPath())
	if err != nil {
		return nil
	}
	c, err := tls.X509KeyPair(b, b)
	if err != nil {
		return nil
	}
	return &c
}
