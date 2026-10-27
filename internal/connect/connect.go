// Package connect is the rendezvous service that lets the apps find a home
// server whose internet address changed, and the server's side of it.
//
// It only ever carries addresses. Servers register the address the service
// sees them at; apps look a server up by its id and connect to it
// directly. No media, API calls or credentials pass through the service,
// and there is no relay to fall back to: when the internet can't reach a
// server, the server's Settings say what to change.
//
// A server's id comes from its public key, so only that server can
// register addresses under its id, and ids can't be guessed. The service
// registers only the address a request comes from, never one it's told, so
// nobody can point a server's id, or the service's reachability check, at
// someone else's machine.
package connect

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base32"
	"net"
	"net/netip"
	"strconv"
	"strings"
)

// DefaultURL is the service Yeix hosts for every Matinee server.
const DefaultURL = "https://connect.matinee.yeix.io"

// ID is the server id for public key pub: 24 characters of base32, 120
// bits of the key's SHA-256.
func ID(pub ed25519.PublicKey) string {
	sum := sha256.Sum256(pub)
	return strings.ToLower(base32.StdEncoding.EncodeToString(sum[:15]))
}

// validID reports whether id looks like one ID makes.
func validID(id string) bool {
	if len(id) != 24 {
		return false
	}
	for _, c := range id {
		if (c < 'a' || c > 'z') && (c < '2' || c > '7') {
			return false
		}
	}
	return true
}

// signed is what a server sends to change its registration: Payload is
// JSON, and Sig signs context+Payload with the key.
type signed struct {
	Key     []byte `json:"key"`
	Payload []byte `json:"payload"`
	Sig     []byte `json:"sig"`
}

const signContext = "matinee-connect v1\n"

// registration is a signed payload.
type registration struct {
	// Time (Unix seconds) keeps an old request from being replayed.
	Time int64 `json:"time"`
	// Port is the router's external port that reaches the server; 0 takes
	// the registration down.
	Port int `json:"port"`
}

// Registered is the service's answer to a registration.
type Registered struct {
	ID  string `json:"id"`
	URL string `json:"url,omitempty"`
	// TTL is how long, in seconds, the registration lasts without another.
	TTL int `json:"ttl"`
}

// Checked is the service's answer to a reachability check.
type Checked struct {
	// IP is the address the request came from: the home's public address.
	IP string `json:"ip"`
	// Reachable means the service could open a connection to IP on the
	// port asked about.
	Reachable bool `json:"reachable"`
}

// Found is the service's answer to a lookup.
type Found struct {
	ID string `json:"id"`
	// URLs are where the server said it was, most recent first.
	URLs    []string `json:"urls"`
	Updated int64    `json:"updated"`
}

// URL is the HTTPS address of ip on port.
func URL(ip netip.Addr, port int) string {
	if port == 443 {
		if ip.Is6() {
			return "https://[" + ip.String() + "]"
		}
		return "https://" + ip.String()
	}
	return "https://" + net.JoinHostPort(ip.String(), strconv.Itoa(port))
}
