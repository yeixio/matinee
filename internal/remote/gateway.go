package remote

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"time"

	"github.com/huin/goupnp/dcps/internetgateway2"
	"github.com/jackpal/gateway"
	natpmp "github.com/jackpal/go-nat-pmp"
)

// A router that opens ports when asked, over UPnP or NAT-PMP (which PCP
// routers answer too).
type router interface {
	// Name says how the router was reached, for Settings.
	Name() string
	// ExternalIP is the router's own address on the internet side.
	ExternalIP(ctx context.Context) (netip.Addr, error)
	// Map forwards external TCP port ext to this host's port internal for
	// lease, and returns the external port the router gave, which can differ.
	Map(ctx context.Context, internal, ext int, lease time.Duration) (int, error)
	// Unmap removes a forward Map made.
	Unmap(ctx context.Context, internal, ext int) error
}

const mappingName = "Matinee"

// findRouter asks the network's router for port forwarding, NAT-PMP first
// since it's quick to answer or fail, then UPnP.
func findRouter(ctx context.Context) (router, error) {
	var problems []error
	if r, err := findNATPMP(ctx); err == nil {
		return r, nil
	} else {
		problems = append(problems, fmt.Errorf("NAT-PMP: %w", err))
	}
	if r, err := findUPnP(ctx); err == nil {
		return r, nil
	} else {
		problems = append(problems, fmt.Errorf("UPnP: %w", err))
	}
	return nil, errors.Join(problems...)
}

type natPMP struct{ c *natpmp.Client }

func findNATPMP(ctx context.Context) (router, error) {
	gw, err := gateway.DiscoverGateway()
	if err != nil {
		return nil, err
	}
	r := natPMP{natpmp.NewClientWithTimeout(gw, 2*time.Second)}
	if _, err := r.ExternalIP(ctx); err != nil {
		return nil, err
	}
	return r, nil
}

func (natPMP) Name() string { return "NAT-PMP" }

func (r natPMP) ExternalIP(context.Context) (netip.Addr, error) {
	res, err := r.c.GetExternalAddress()
	if err != nil {
		return netip.Addr{}, err
	}
	return netip.AddrFrom4(res.ExternalIPAddress), nil
}

func (r natPMP) Map(_ context.Context, internal, ext int, lease time.Duration) (int, error) {
	res, err := r.c.AddPortMapping("tcp", internal, ext, int(lease/time.Second))
	if err != nil {
		return 0, err
	}
	return int(res.MappedExternalPort), nil
}

func (r natPMP) Unmap(_ context.Context, internal, _ int) error {
	_, err := r.c.AddPortMapping("tcp", internal, 0, 0)
	return err
}

// upnpConn is what the WANIPConnection and WANPPPConnection services share.
type upnpConn interface {
	GetExternalIPAddressCtx(ctx context.Context) (string, error)
	AddPortMappingCtx(ctx context.Context, remoteHost string, ext uint16, proto string, internal uint16, client string, enabled bool, desc string, lease uint32) error
	DeletePortMappingCtx(ctx context.Context, remoteHost string, ext uint16, proto string) error
	LocalAddr() net.IP
}

type upnp struct {
	c    upnpConn
	name string
}

func findUPnP(ctx context.Context) (router, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if cs, _, err := internetgateway2.NewWANIPConnection2ClientsCtx(ctx); err == nil && len(cs) > 0 {
		return upnp{cs[0], "UPnP"}, nil
	}
	if cs, _, err := internetgateway2.NewWANIPConnection1ClientsCtx(ctx); err == nil && len(cs) > 0 {
		return upnp{cs[0], "UPnP"}, nil
	}
	if cs, _, err := internetgateway2.NewWANPPPConnection1ClientsCtx(ctx); err == nil && len(cs) > 0 {
		return upnp{cs[0], "UPnP"}, nil
	}
	return nil, errors.New("no router answered")
}

func (r upnp) Name() string { return r.name }

func (r upnp) ExternalIP(ctx context.Context) (netip.Addr, error) {
	s, err := r.c.GetExternalIPAddressCtx(ctx)
	if err != nil {
		return netip.Addr{}, err
	}
	return netip.ParseAddr(s)
}

func (r upnp) Map(ctx context.Context, internal, ext int, lease time.Duration) (int, error) {
	local := r.c.LocalAddr()
	if local == nil {
		return 0, errors.New("couldn't tell this host's address on the router's network")
	}
	err := r.c.AddPortMappingCtx(ctx, "", uint16(ext), "TCP", uint16(internal), local.String(), true, mappingName, uint32(lease/time.Second))
	if err != nil && lease > 0 {
		// Some routers only take permanent forwards (lease 0); Unmap removes
		// it when the server stops or remote access is turned off.
		err = r.c.AddPortMappingCtx(ctx, "", uint16(ext), "TCP", uint16(internal), local.String(), true, mappingName, 0)
	}
	if err != nil {
		return 0, err
	}
	return ext, nil
}

func (r upnp) Unmap(ctx context.Context, _, ext int) error {
	return r.c.DeletePortMappingCtx(ctx, "", uint16(ext), "TCP")
}
