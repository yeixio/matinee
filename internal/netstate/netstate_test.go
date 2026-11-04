package netstate

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

// fakeNet answers every call, or (down) fails it as an unreachable network would.
type fakeNet struct {
	down  atomic.Bool
	calls atomic.Int32
}

func (f *fakeNet) RoundTrip(req *http.Request) (*http.Response, error) {
	f.calls.Add(1)
	if f.down.Load() {
		return nil, &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("network is unreachable")}
	}
	return &http.Response{StatusCode: 200, Body: http.NoBody, Request: req}, nil
}

func get(t *testing.T, c *http.Client, url string) error {
	t.Helper()
	res, err := c.Get(url)
	if err == nil {
		res.Body.Close()
	}
	return err
}

// A site that doesn't answer is left alone for a while: calls to it fail
// at once without reaching the network, then one call tries it again.
func TestDownSiteFailsFastThenIsTriedAgain(t *testing.T) {
	n := &fakeNet{}
	tr := New()
	clock := time.Unix(1000, 0)
	tr.now = func() time.Time { return clock }
	backs := make(chan struct{}, 1)
	tr.Back = func() { backs <- struct{}{} }
	c := &http.Client{Transport: tr.Wrap(n)}

	n.down.Store(true)
	if err := get(t, c, "https://api.themoviedb.org/3/x"); err == nil || errors.Is(err, ErrDown) {
		t.Fatalf("first call: %v", err)
	}
	if err := get(t, c, "https://api.themoviedb.org/3/y"); !errors.Is(err, ErrDown) {
		t.Fatalf("second call should fail at once: %v", err)
	}
	if n.calls.Load() != 1 {
		t.Fatalf("%d calls reached the network", n.calls.Load())
	}
	// Other sites aren't affected.
	n.down.Store(false)
	if err := get(t, c, "https://musicbrainz.org/ws/2"); err != nil {
		t.Fatal(err)
	}
	if st := tr.Status(); st.Offline || len(st.Down) != 1 || st.Down[0].Host != "api.themoviedb.org" {
		t.Fatalf("status: %+v", st)
	}

	// Once the wait is over, a call goes through, and success clears it.
	clock = clock.Add(firstWait)
	if err := get(t, c, "https://api.themoviedb.org/3/z"); err != nil {
		t.Fatal(err)
	}
	if st := tr.Status(); len(st.Down) != 0 {
		t.Fatalf("still down: %+v", st)
	}
	select {
	case <-backs:
		t.Fatal("the internet wasn't down, only one site")
	default:
	}
}

// With every site on the internet down, the server counts as offline;
// the first answer after that runs Back.
func TestOfflineAndBack(t *testing.T) {
	n := &fakeNet{}
	tr := New()
	clock := time.Unix(1000, 0)
	tr.now = func() time.Time { return clock }
	backs := make(chan struct{}, 1)
	tr.Back = func() { backs <- struct{}{} }
	c := &http.Client{Transport: tr.Wrap(n)}

	n.down.Store(true)
	get(t, c, "https://api.themoviedb.org/")
	get(t, c, "https://image.tmdb.org/")
	// A Radarr at home that's off doesn't count either way.
	get(t, c, "http://192.168.1.20:7878/")
	st := tr.Status()
	if !st.Offline || len(st.Down) != 3 || st.Since == 0 {
		t.Fatalf("status: %+v", st)
	}
	if !st.Down[0].Home || st.Down[1].Home {
		t.Fatalf("home flags: %+v", st.Down)
	}
	// Run's check finds the internet back.
	n.down.Store(false)
	clock = clock.Add(time.Hour)
	if name := tr.dueForCheck(); name == "" {
		t.Fatal("nothing due for a check")
	}
	get(t, c, "https://image.tmdb.org/")
	select {
	case <-backs:
	case <-time.After(time.Second):
		t.Fatal("Back didn't run")
	}
	if tr.Status().Offline {
		t.Fatal("still offline")
	}
}

// A call its caller gave up on doesn't mark the site down.
func TestCanceledCallDoesNotCount(t *testing.T) {
	tr := New()
	block := http.RoundTripper(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		<-req.Context().Done()
		return nil, req.Context().Err()
	}))
	c := &http.Client{Transport: tr.Wrap(block)}
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://api.themoviedb.org/", nil)
	go cancel()
	c.Do(req)
	if st := tr.Status(); len(st.Down) != 0 {
		t.Fatalf("marked down: %+v", st)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestIsPrivate(t *testing.T) {
	for name, want := range map[string]bool{
		"192.168.1.2": true, "10.0.0.5": true, "127.0.0.1": true, "::1": true, "radarr": true,
		"homeassistant.local": true, "nas.lan": true, "api.themoviedb.org": false, "8.8.8.8": false,
	} {
		if got := isPrivate(name); got != want {
			t.Errorf("isPrivate(%s) = %v", name, got)
		}
	}
}
