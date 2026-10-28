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
