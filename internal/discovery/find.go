package discovery

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/libp2p/zeroconf/v2"
)

// These look for other apps on the home network, such as Box Office, so
// admins don't have to type their addresses. Nothing leaves the local
// network: one asks over multicast DNS, the other knocks on one port of
// the neighbours on this host's own networks.

// Browse lists the http addresses of the services of a DNS-SD type
// (such as "_boxoffice._tcp") that answer on the home network before ctx
// ends.
func Browse(ctx context.Context, service string) []string {
	entries := make(chan *zeroconf.ServiceEntry, 16)
	done := make(chan struct{})
	go func() {
		defer close(done)
		var opts []zeroconf.ClientOption
		if home := homeInterfaces(); len(home) > 0 {
			opts = append(opts, zeroconf.SelectIfaces(home))
		}
		_ = zeroconf.Browse(ctx, service, "local.", entries, opts...)
	}()
	var out []string
	for {
		select {
		case e := <-entries:
			if e == nil || e.Port <= 0 {
				continue
			}
			for _, ip := range e.AddrIPv4 {
				out = appendNew(out, fmt.Sprintf("http://%s", net.JoinHostPort(ip.String(), strconv.Itoa(e.Port))))
			}
		case <-done:
			return out
		}
	}
}

// maxSweep is the most neighbours Sweep knocks on: a /24 on each of a few
// networks.
const maxSweep = 1024

// Sweep lists the http addresses of the hosts on this host's own private
// IPv4 networks (at most the /24 around each address) that take a
// connection on port before ctx ends.
func Sweep(ctx context.Context, port int) []string {
	return sweep(ctx, homePrefixes(), port, func(ctx context.Context, addr string) bool {
		c, err := (&net.Dialer{Timeout: 500 * time.Millisecond}).DialContext(ctx, "tcp", addr)
		if err != nil {
			return false
		}
		c.Close()
		return true
	})
}

func sweep(ctx context.Context, nets []netip.Prefix, port int, open func(context.Context, string) bool) []string {
	var addrs []netip.Addr
	for _, p := range nets {
		for a := p.Masked().Addr().Next(); p.Contains(a) && len(addrs) < maxSweep; a = a.Next() {
			if !p.Contains(a.Next()) { // the broadcast address
				break
			}
			addrs = append(addrs, a)
		}
	}
	found := make([]bool, len(addrs))
	sem := make(chan struct{}, 128)
	var wg sync.WaitGroup
	for i, a := range addrs {
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			found[i] = open(ctx, netip.AddrPortFrom(a, uint16(port)).String())
		}()
	}
	wg.Wait()
	var out []string
	for i, a := range addrs {
		if found[i] {
			out = append(out, "http://"+netip.AddrPortFrom(a, uint16(port)).String())
		}
	}
	return out
}

// homePrefixes are this host's private IPv4 networks, narrowed to the /24
// around its address.
func homePrefixes() []netip.Prefix {
	var out []netip.Prefix
	for _, i := range homeInterfaces() {
		addrs, err := i.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			if n, ok := a.(*net.IPNet); ok {
				if p, ok := homePrefix(n); ok && !slices.Contains(out, p) {
					out = append(out, p)
				}
			}
		}
	}
	return out
}

func homePrefix(n *net.IPNet) (netip.Prefix, bool) {
	ip, ok := netip.AddrFromSlice(n.IP)
	if !ok {
		return netip.Prefix{}, false
	}
	ip = ip.Unmap()
	if !ip.Is4() || !ip.IsPrivate() {
		return netip.Prefix{}, false
	}
	bits, _ := n.Mask.Size()
	return netip.PrefixFrom(ip, max(bits, 24)).Masked(), true
}

func appendNew(list []string, s string) []string {
	if slices.Contains(list, s) {
		return list
	}
	return append(list, s)
}
