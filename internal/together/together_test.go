package together

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestSessionFollowsEveryone(t *testing.T) {
	h := New()
	var at atomic.Int64
	at.Store(time.Unix(1000, 0).UnixNano())
	h.now = func() time.Time { return time.Unix(0, at.Load()) }
	tick := func(d time.Duration) { at.Add(int64(d)) }
	ann := Person{UserID: 1, Name: "Ann", ClientID: "ann-phone"}
	bob := Person{UserID: 2, Name: "Bob", ClientID: "bob-tv"}

	st, err := h.Start(ann, 42, 1000, true, "")
	if err != nil {
		t.Fatal(err)
	}
	if !validCode(st.Code) || st.Version != 1 {
		t.Fatalf("started %+v", st)
	}
	if _, err := h.Join(st.Code, bob); err != nil {
		t.Fatal(err)
	}
	// Codes are forgiving about case and dashes.
	if _, err := h.Peek(st.Code[:3] + "-" + st.Code[3:]); err != nil {
		t.Fatalf("peek with a dash: %v", err)
	}

	// Ann waits; Bob plays; Ann hears it right away.
	got := make(chan State)
	go func() {
		s, err := h.Wait(context.Background(), st.Code, ann, 2, time.Second*5)
		if err != nil {
			t.Error(err)
		}
		got <- s
	}()
	time.Sleep(20 * time.Millisecond)
	tick(3 * time.Second)
	if _, err := h.Act(st.Code, bob, ActionPlay, 1500, 0); err != nil {
		t.Fatal(err)
	}
	select {
	case s := <-got:
		if s.Paused || s.PositionMs != 1500 || s.UpdatedAt != h.now().UnixMilli() || s.Last.By != "Bob" {
			t.Fatalf("Ann heard %+v", s)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Ann never heard")
	}

	// Someone else's player can't be used.
	if _, err := h.Act(st.Code, Person{UserID: 3, ClientID: "bob-tv"}, ActionPause, 0, 0); err != ErrNotMember {
		t.Fatalf("spoofed act: %v", err)
	}
	if _, err := h.Act(st.Code, bob, "rewind", 0, 0); err != ErrAction {
		t.Fatalf("unknown action: %v", err)
	}

	// Bob's TV goes quiet; he drops out and Ann stays host.
	tick(memberTimeout + time.Second)
	h.Wait(context.Background(), st.Code, ann, 0, 0)
	h.mu.Lock()
	h.sweep()
	h.mu.Unlock()
	s, _ := h.Peek(st.Code)
	if len(s.Members) != 1 || s.Members[0].Name != "Ann" || !s.Members[0].Host {
		t.Fatalf("members after Bob dropped: %+v", s.Members)
	}

	// Ann's phone starts something else: she leaves the old session, which
	// lasts a while empty, then goes.
	other, _ := h.Start(ann, 7, 0, false, "")
	if other.Code == st.Code {
		t.Fatal("same code")
	}
	s, _ = h.Peek(st.Code)
	if len(s.Members) != 0 {
		t.Fatalf("old session members: %+v", s.Members)
	}
	tick(emptyTimeout + time.Second)
	h.Wait(context.Background(), other.Code, ann, 0, 0)
	h.mu.Lock()
	h.sweep()
	h.mu.Unlock()
	if _, err := h.Peek(st.Code); err != ErrNoSession {
		t.Fatalf("old session still there: %v", err)
	}
}

func TestResumeAfterRestart(t *testing.T) {
	h := New()
	ann := Person{UserID: 1, Name: "Ann", ClientID: "ann-phone"}
	// The server restarted: the code Ann had is free, and she starts it again.
	st, err := h.Start(ann, 42, 60000, false, "abcdef")
	if err != nil {
		t.Fatal(err)
	}
	if st.Code != "ABCDEF" {
		t.Fatalf("resumed as %q", st.Code)
	}
	// Bob comes back after her and finds it going: he joins instead.
	if _, err := h.Start(Person{UserID: 2, ClientID: "bob-tv"}, 42, 61000, false, "ABCDEF"); err != ErrExists {
		t.Fatalf("second resume: %v", err)
	}
	// Ended sessions stay ended.
	h.End(st.Code, ann)
	if _, err := h.Start(ann, 42, 0, false, "ABCDEF"); err != ErrNoSession {
		t.Fatalf("resume ended: %v", err)
	}
	s, err := h.Wait(context.Background(), "ABCDEF", ann, 5, time.Second)
	if err != nil || !s.Ended {
		t.Fatalf("wait on ended: %+v %v", s, err)
	}
	// Codes that couldn't be ours are refused.
	if _, err := h.Start(ann, 42, 0, false, "0O0O0O"); err != ErrNoSession {
		t.Fatalf("bad code: %v", err)
	}
}

func TestCloseWakesWaiters(t *testing.T) {
	h := New()
	ann := Person{UserID: 1, Name: "Ann", ClientID: "ann-phone"}
	st, _ := h.Start(ann, 42, 0, false, "")
	done := make(chan struct{})
	go func() {
		h.Wait(context.Background(), st.Code, ann, st.Version, MaxWait)
		close(done)
	}()
	time.Sleep(20 * time.Millisecond)
	h.Close()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("wait didn't end on Close")
	}
}
