// Package discovery announces a Matinee server on the local network with
// DNS-SD over multicast DNS, so the apps can list it without anyone typing
// an address.
package discovery

import (
	"context"
	"fmt"
	"log/slog"
	"net"
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
	server, err := zeroconf.Register(name, Service, "local.", a.Port, a.TXT(), homeInterfaces())
	if err != nil {
		return fmt.Errorf("announce on the local network: %w", err)
	}
	log.Info("announcing on the local network", "name", name, "port", a.Port, "url", a.URL)
	<-ctx.Done()
	server.Shutdown()
	return nil
}

// virtualPrefixes name the interfaces of containers, Kubernetes networks and
// VPNs. A server on a cluster node or a Docker host has several; announcing
// their addresses sends some phones to one they can't reach, and Android
// apps then hang trying it.
var virtualPrefixes = []string{
	"cni", "flannel", "docker", "veth", "br-", "cali", "cilium", "vxlan", "kube",
	"tunl", "tun", "tap", "wg", "tailscale", "zt", "lxc", "lxd", "virbr", "vmnet", "podman", "weave", "genev",
}

// virtual reports whether an interface belongs to containers or a VPN
// rather than the home network.
func virtual(name string) bool {
	name = strings.ToLower(name)
	for _, p := range virtualPrefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// homeInterfaces lists the interfaces facing the home network. When there's
// none it can tell, it returns nil, which announces on all of them.
func homeInterfaces() []net.Interface {
	all, err := net.Interfaces()
	if err != nil {
		return nil
	}
	return pickHome(all)
}

func pickHome(all []net.Interface) []net.Interface {
	var out []net.Interface
	for _, i := range all {
		if i.Flags&net.FlagUp == 0 || i.Flags&net.FlagLoopback != 0 || i.Flags&net.FlagMulticast == 0 || virtual(i.Name) {
			continue
		}
		out = append(out, i)
	}
	return out
}
