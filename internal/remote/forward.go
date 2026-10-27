package remote

import (
	"context"
	"fmt"
	"net/netip"
	"time"
)

// forwardedPort stands in for a router that can't open ports by itself:
// the admin forwarded the router's external port ext to this server's
// remote access port, and the connect service tells the server its public
// address and whether the forward works.
type forwardedPort struct {
	ext      int
	internal int
	check    func(ctx context.Context, port int) (netip.Addr, bool, error)
}

func (f *forwardedPort) Name() string { return "" }

func (f *forwardedPort) ExternalIP(ctx context.Context) (netip.Addr, error) {
	ip, ok, err := f.check(ctx, f.ext)
	if err != nil {
		return ip, fmt.Errorf("couldn't check the port forward: %w", err)
	}
	if !ok {
		return ip, &forwardMissing{ip: ip, ext: f.ext, internal: f.internal}
	}
	return ip, nil
}

// Map can only use the forward the admin set up, and only for the
// server's own port; the certificate check is served there too.
func (f *forwardedPort) Map(ctx context.Context, internal, ext int, lease time.Duration) (int, error) {
	if internal != f.internal {
		return 0, fmt.Errorf("only port %d is forwarded", f.ext)
	}
	return f.ext, nil
}

func (f *forwardedPort) Unmap(ctx context.Context, internal, ext int) error { return nil }

// forwardMissing is the error for a forward the internet can't get through.
type forwardMissing struct {
	ip            netip.Addr
	ext, internal int
}

func (e *forwardMissing) Error() string {
	return fmt.Sprintf("the internet can't reach this server through port %d at %s. In the router's settings, forward TCP port %d to port %d on this computer. If it already is, the internet provider may share one address among customers; ask them for a public address", e.ext, e.ip, e.ext, e.internal)
}
