package slowfs

import (
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

// A file that keeps timing out with its share answering is given up on
// after GiveUpAfter timeouts; one on a share that's stalled isn't counted.
func TestTimeoutsGiveUp(t *testing.T) {
	var to Timeouts
	file := filepath.Join(t.TempDir(), "a.mkv")
	os.WriteFile(file, []byte("x"), 0o644)
	for i := 1; i < GiveUpAfter; i++ {
		if to.Add(1, file) {
			t.Fatalf("gave up after %d", i)
		}
	}
	if !to.Add(1, file) {
		t.Fatalf("didn't give up after %d", GiveUpAfter)
	}

	old := Wait
	Wait = 20 * time.Millisecond
	t.Cleanup(func() { Wait = old })
	stalled := "/stalled-share/media/b.mkv"
	release := make(chan struct{})
	defer close(release)
	Do(stalled, func() (int, error) { <-release; return 0, nil })
	for range GiveUpAfter + 1 {
		if to.Add(2, stalled) {
			t.Fatal("gave up on a file whose share isn't answering")
		}
	}
}
