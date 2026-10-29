package live

import (
	"context"
	"testing"
	"time"
)

func TestQuietClientsGoOffline(t *testing.T) {
	h := New()
	clock := time.Unix(1_800_000_000, 0)
	h.now = func() time.Time { return clock }
	events, cancel := h.Subscribe()
	defer cancel()
	next := func() Event {
		t.Helper()
		select {
		case e := <-events:
			return e
		default:
			t.Fatal("no event")
			return Event{}
		}
	}
	all := func(Client) bool { return true }

	hello := Hello{Name: "TV", Platform: "tvos", Commands: []string{CommandPause}}
	h.Report("tv-123456", 1, User{ID: 1}, hello, &Playing{Item: Item{ID: 7, Title: "Big"}, PositionMs: 10}, false)
	if e := next(); e.Type != EventPlaybackStarted || e.Item.ID != 7 {
		t.Fatalf("%+v", e)
	}
	next() // client.updated

	// Playing with no report for a minute counts as stopped, though the
	// client still asks for commands.
	clock = clock.Add(40 * time.Second)
	h.Poll(context.Background(), "tv-123456", 1, 0)
	clock = clock.Add(25 * time.Second)
	h.sweep()
	if e := next(); e.Type != EventPlaybackStopped || e.Finished {
		t.Fatalf("%+v", e)
	}
	if e := next(); e.Type != EventClientUpdated || e.Client.Playing != nil || !e.Client.Online {
		t.Fatalf("%+v", e)
	}

	// Not asking for commands goes offline, then gets forgotten.
	clock = clock.Add(time.Minute)
	h.sweep()
	if e := next(); e.Type != EventClientUpdated || e.Client.Online {
		t.Fatalf("%+v", e)
	}
	if _, err := h.Send("tv-123456", all, Command{Type: CommandPause}); err != ErrOffline {
		t.Fatalf("send to offline: %v", err)
	}
	clock = clock.Add(2 * time.Hour)
	h.sweep()
	if e := next(); e.Type != EventClientRemoved || e.ClientID != "tv-123456" {
		t.Fatalf("%+v", e)
	}
	if len(h.Clients(all)) != 0 {
		t.Fatal("still listed")
	}
}

func TestSlowSubscriberIsCutOff(t *testing.T) {
	h := New()
	events, cancel := h.Subscribe()
	defer cancel()
	for range 300 {
		h.Publish(Event{Type: EventItemAdded})
	}
	n := 0
	for range events {
		n++
	}
	if n != 256 {
		t.Fatalf("got %d events before the feed closed", n)
	}
}

func TestCloseEndsHeldPolls(t *testing.T) {
	h := New()
	h.Report("web-123456", 1, User{ID: 1}, Hello{Name: "Web"}, nil, false)
	done := make(chan time.Duration)
	go func() {
		start := time.Now()
		h.Poll(context.Background(), "web-123456", 1, 25*time.Second)
		done <- time.Since(start)
	}()
	time.Sleep(50 * time.Millisecond)
	if h.Closing() {
		t.Fatal("closing before Close")
	}
	h.Close()
	h.Close() // twice is fine
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("held poll still waiting after Close")
	}
	if !h.Closing() {
		t.Fatal("not closing after Close")
	}

	// Once closing, a poll waits a little rather than none, so players
	// that ask again at once don't spin.
	start := time.Now()
	h.Poll(context.Background(), "web-123456", 1, 25*time.Second)
	if d := time.Since(start); d < closingWait/2 || d > closingWait+time.Second {
		t.Fatalf("poll after Close took %v", d)
	}
}
