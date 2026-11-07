package scan

import (
	"errors"
	"os"
	"testing"
	"time"
)

// A library folder that doesn't answer is skipped after rootWait, and isn't
// read again while the first read is still stuck.
func TestReadRootGivesUp(t *testing.T) {
	oldWait, oldRead := rootWait, readDir
	t.Cleanup(func() { rootWait, readDir = oldWait, oldRead })
	rootWait = 20 * time.Millisecond
	release := make(chan struct{})
	reads := 0
	readDir = func(string) ([]os.DirEntry, error) {
		reads++
		<-release
		return nil, nil
	}
	var s Scanner
	if _, err := s.readRoot("/media/movies"); !errors.Is(err, errNotAnswering) {
		t.Fatalf("hung share: err = %v", err)
	}
	if _, err := s.readRoot("/media/movies"); !errors.Is(err, errNotAnswering) {
		t.Fatalf("still hung: err = %v", err)
	}
	close(release)
	deadline := time.Now().Add(time.Second)
	for {
		s.mu.Lock()
		stuck := s.stuck["/media/movies"]
		s.mu.Unlock()
		if !stuck {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the stuck read never cleared")
		}
		time.Sleep(time.Millisecond)
	}
	if reads != 1 {
		t.Errorf("reads = %d, want 1 while it was stuck", reads)
	}
	if _, err := s.readRoot("/media/movies"); err != nil {
		t.Fatalf("after it answered: %v", err)
	}
}
