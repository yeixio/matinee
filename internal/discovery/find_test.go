package discovery

import (
	"context"
	"net"
	"net/netip"
	"slices"
	"sync/atomic"
	"testing"
)

func TestHomePrefix(t *testing.T) {
	for in, want := range map[string]string{
		"192.168.1.20/24": "192.168.1.0/24",
		"10.0.5.9/16":     "10.0.5.0/24",
		"172.16.3.4/28":   "172.16.3.0/28",
		"8.8.8.8/24":      "",
		"fd00::1/64":      "",
	} {
		ip, n, _ := net.ParseCIDR(in)
		n.IP = ip
		p, ok := homePrefix(n)
		if got := ""; ok {
			got = p.String()
			if got != want {
				t.Errorf("%s: %s, want %s", in, got, want)
			}
		} else if want != "" {
			t.Errorf("%s: skipped, want %s", in, want)
		}
	}
}

func TestSweep(t *testing.T) {
	open := map[string]bool{"192.168.1.7:7272": true, "192.168.1.200:7272": true}
	got := sweep(context.Background(), []netip.Prefix{netip.MustParsePrefix("192.168.1.0/24")}, 7272, func(_ context.Context, addr string) bool {
		return open[addr]
	})
	if want := []string{"http://192.168.1.7:7272", "http://192.168.1.200:7272"}; !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	// The network and broadcast addresses aren't asked.
	var n atomic.Int32
	sweep(context.Background(), []netip.Prefix{netip.MustParsePrefix("10.0.0.0/30")}, 1, func(_ context.Context, addr string) bool {
		n.Add(1)
		return false
	})
	if n.Load() != 2 {
		t.Fatalf("a /30 asked %d hosts, want 2", n.Load())
	}
}
