package slowfs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRoot(t *testing.T) {
	for path, want := range map[string]string{
		"/media/movies/Milk (2008)/Milk.mkv": "/media/movies",
		"/media/movies":                      "/media/movies",
		"/media":                             "/media",
		"//nas/media/Films/a.mkv":            "//nas/media",
	} {
		if got := Root(path); got != want {
			t.Errorf("Root(%q) = %q, want %q", path, got, want)
		}
	}
}

// A call that hangs gives up after Wait; later calls on the same share
// give up at once until it comes back, and other shares aren't held up.
func TestDoGivesUpOnAHungShare(t *testing.T) {
	old := Wait
	Wait = 50 * time.Millisecond
	t.Cleanup(func() { Wait = old })
	release := make(chan struct{})
	start := time.Now()
	_, err := Do("/media/movies/a.mkv", func() (int, error) { <-release; return 1, nil })
	if !errors.Is(err, ErrNotAnswering) || time.Since(start) > time.Second {
		t.Fatalf("hung call: %v after %v", err, time.Since(start))
	}
	if s := Stuck(); len(s) != 1 || s[0].Root != "/media/movies" {
		t.Fatalf("stuck: %+v", s)
	}
	ran := false
	if _, err := Do("/media/movies/b.mkv", func() (int, error) { ran = true; return 2, nil }); !errors.Is(err, ErrNotAnswering) || ran {
		t.Fatalf("second call on a stuck share: ran %v, %v", ran, err)
	}
	if v, err := Do("/media/tv/c.mkv", func() (int, error) { return 3, nil }); err != nil || v != 3 {
		t.Fatalf("another share: %v %v", v, err)
	}
	close(release)
	for deadline := time.Now().Add(time.Second); len(Stuck()) > 0; time.Sleep(5 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("still stuck after the call came back")
		}
	}
	if v, err := Do("/media/movies/b.mkv", func() (int, error) { return 4, nil }); err != nil || v != 4 {
		t.Fatalf("after it answered: %v %v", v, err)
	}
}

// A file that keeps timing out with its share answering is checked for
// damage after GiveUpAfter timeouts. One whose share stalls while it's read
// is tried later, until the share has stalled StallsBeforeSetAside times
// during its reads.
func TestTimeoutsGiveUp(t *testing.T) {
	ctx := t.Context()
	var to Timeouts
	file := filepath.Join(t.TempDir(), "a.mkv")
	os.WriteFile(file, []byte("x"), 0o644)
	for i := 1; i < GiveUpAfter; i++ {
		if v := to.Add(ctx, nil, 1, file, "k"); v != TryLater {
			t.Fatalf("gave up after %d: %v", i, v)
		}
	}
	if v := to.Add(ctx, nil, 1, file, "k"); v != CheckFile {
		t.Fatalf("didn't give up after %d: %v", GiveUpAfter, v)
	}

	old := Wait
	Wait = 20 * time.Millisecond
	t.Cleanup(func() { Wait = old })
	stalled := "/stalled-share/media/b.mkv"
	release := make(chan struct{})
	defer close(release)
	Do(stalled, func() (int, error) { <-release; return 0, nil })
	for range GiveUpAfter + 1 {
		if v := to.Add(ctx, nil, 2, stalled, "k"); v != TryLater {
			t.Fatalf("gave up on a file whose share isn't answering, with nowhere to count stalls: %v", v)
		}
	}
	counts := fakeStalls{}
	for i := 1; i < StallsBeforeSetAside; i++ {
		if v := to.Add(ctx, counts, 2, stalled, "k"); v != TryLater {
			t.Fatalf("set aside after %d stalls: %v", i, v)
		}
	}
	if v := to.Add(ctx, counts, 2, stalled, "k"); v != SetAside {
		t.Fatalf("didn't set aside after %d stalls: %v", StallsBeforeSetAside, v)
	}
}

type fakeStalls map[int64]int

func (f fakeStalls) AddShareStall(_ context.Context, id int64, _ string, _ time.Duration) (int, error) {
	f[id]++
	return f[id], nil
}
