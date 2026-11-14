package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
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

// A negative wait keeps trying past any deadline, until it's stopped.
func TestWaitForever(t *testing.T) {
	conn, err := sql.Open("pgx", "postgres://matinee:x@127.0.0.1:1/matinee?connect_timeout=1")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	ctx, cancel := context.WithCancel(context.Background())
	var tries int
	err = waitUntilReady(ctx, conn, -1, func(error, time.Duration) {
		tries++
		if tries == 3 {
			cancel()
		}
	})
	if !errors.Is(err, context.Canceled) || tries != 3 {
		t.Fatalf("err = %v after %d waits", err, tries)
	}
}

// A wrong password or a missing database isn't waited for.
func TestPermanentErrors(t *testing.T) {
	for code, want := range map[string]bool{"28P01": true, "28000": true, "3D000": true, "57P03": false} {
		if got := permanent(fmt.Errorf("connect: %w", &pgconn.PgError{Code: code})); got != want {
			t.Errorf("%s: permanent = %v", code, got)
		}
	}
	if permanent(errors.New("connection refused")) {
		t.Error("a refused connection is permanent")
	}
}
