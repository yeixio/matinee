// Package discovery announces a Matinee server on the local network with
// DNS-SD over multicast DNS, so the apps can list it without anyone typing
// an address.
package discovery

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/libp2p/zeroconf/v2"
)

// Service is the DNS-SD service type the apps browse for.
const Service = "_matinee._tcp"

// Announcement is what the apps see in their list of servers nearby.
type Announcement struct {
	Name    string // shown in the list, such as "Matinee"
	Port    int    // where this host serves Matinee
	URL     string // optional: the server's address away from home
	Version string
	// Home means this host's IP and Port reach Matinee directly from the home
	// network, so devices there can use it without signing in. Otherwise
	// apps connect to URL.
	Home bool
}

// TXT is the record the apps read: who's answering, and where to connect.
func (a Announcement) TXT() []string {
	txt := []string{"product=matinee", "version=" + a.Version}
	if a.URL != "" {
		txt = append(txt, "url="+strings.TrimRight(a.URL, "/"))
	}
	if a.Home {
		txt = append(txt, "home=1")
	}
	return txt
}

// Run announces a until ctx ends.
func Run(ctx context.Context, a Announcement, log *slog.Logger) error {
	name := a.Name
	if name == "" {
		name = "Matinee"
	}
	server, err := zeroconf.Register(name, Service, "local.", a.Port, a.TXT(), nil)
	if err != nil {
		return fmt.Errorf("announce on the local network: %w", err)
	}
	log.Info("announcing on the local network", "name", name, "port", a.Port, "url", a.URL)
	<-ctx.Done()
	server.Shutdown()
	return nil
}
