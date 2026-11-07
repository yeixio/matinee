package discovery

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestTXT(t *testing.T) {
	got := Announcement{Port: 7171, Version: "1.2.0"}.TXT()
	if want := []string{"product=matinee", "version=1.2.0"}; !slices.Equal(got, want) {
		t.Fatalf("without a URL: %q", got)
	}
	got = Announcement{Port: 443, URL: "https://matinee.example.com/", Version: "1.2.0"}.TXT()
	if !slices.Contains(got, "url=https://matinee.example.com") {
		t.Fatalf("with a URL: %q", got)
	}
	if slices.Contains(got, "home=1") {
		t.Fatalf("an announcer without a home port claimed one: %q", got)
	}
	got = Announcement{Port: 7172, URL: "https://matinee.example.com", Version: "1.2.0", Home: true}.TXT()
	if !slices.Contains(got, "home=1") || !slices.Contains(got, "url=https://matinee.example.com") {
		t.Fatalf("with a home port: %q", got)
	}
	got = Announcement{Port: 7171, Version: "1.2.0", UUID: "0f8fad5b-d9cb-469f-a165-70867728950e"}.TXT()
	if !slices.Contains(got, "id=0f8fad5b-d9cb-469f-a165-70867728950e") {
		t.Fatalf("with an id: %q", got)
	}
}

func TestHomeInterfacesSkipContainersAndVPNs(t *testing.T) {
	up := net.FlagUp | net.FlagMulticast
	all := []net.Interface{
		{Name: "lo", Flags: up | net.FlagLoopback},
		{Name: "enp5s0", Flags: up},
		{Name: "wlan0", Flags: up},
		{Name: "eth1", Flags: net.FlagMulticast}, // down
		{Name: "cni0", Flags: up},
		{Name: "flannel.1", Flags: up},
		{Name: "docker0", Flags: up},
		{Name: "veth1a2b3c", Flags: up},
		{Name: "tailscale0", Flags: up},
		{Name: "br-5f2e", Flags: up},
	}
	var names []string
	for _, i := range pickHome(all) {
		names = append(names, i.Name)
	}
	if strings.Join(names, ",") != "enp5s0,wlan0" {
		t.Fatalf("announced on %v, want enp5s0 and wlan0", names)
	}
	if !virtual("CNI0") || virtual("eth0") || virtual("enp5s0") || virtual("br0") {
		t.Fatal("Virtual misjudged a name")
	}
}

type fakeServer struct{ shut chan struct{} }

func (f fakeServer) Shutdown() { close(f.shut) }

// A failed announce is tried again until it works, instead of ending Run.
func TestRunRetries(t *testing.T) {
	oldReg, oldFirst := register, retryFirst
	t.Cleanup(func() { register, retryFirst = oldReg, oldFirst })
	retryFirst = time.Millisecond
	tries := 0
	up := make(chan struct{})
	srv := fakeServer{shut: make(chan struct{})}
	register = func(string, string, string, int, []string, []net.Interface) (shutdowner, error) {
		tries++
		if tries < 3 {
			return nil, errors.New("no multicast interface")
		}
		close(up)
		return srv, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error)
	go func() { done <- Run(ctx, Announcement{Port: 443}, slog.New(slog.DiscardHandler)) }()
	select {
	case <-up:
	case <-time.After(5 * time.Second):
		t.Fatal("never announced")
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	<-srv.shut
	if tries != 3 {
		t.Errorf("tries = %d, want 3", tries)
	}
}
