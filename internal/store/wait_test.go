package store

import (
	"strings"
	"testing"
	"time"
)

// A PostgreSQL server that doesn't answer is tried again until the wait is
// up, and then the error says how long it waited.
func TestOpenWaitsForPostgres(t *testing.T) {
	var tries int
	start := time.Now()
	_, err := OpenWith("postgres://matinee:x@127.0.0.1:1/matinee?connect_timeout=1", Options{
		Wait:    1500 * time.Millisecond,
		Waiting: func(error, time.Duration) { tries++ },
	})
	if err == nil {
		t.Fatal("opened a database that isn't there")
	}
	if tries < 1 {
		t.Errorf("didn't try again (%d waits)", tries)
	}
	if took := time.Since(start); took < time.Second {
		t.Errorf("gave up after %s, before the wait was up", took)
	}
	if !strings.Contains(err.Error(), "didn't answer for") {
		t.Errorf("error = %v", err)
	}
}

// Without a wait, it tries once.
func TestOpenWithoutWaitTriesOnce(t *testing.T) {
	var tries int
	_, err := OpenWith("postgres://matinee:x@127.0.0.1:1/matinee?connect_timeout=1", Options{
		Waiting: func(error, time.Duration) { tries++ },
	})
	if err == nil || tries != 0 {
		t.Fatalf("err = %v, waits = %d", err, tries)
	}
}
