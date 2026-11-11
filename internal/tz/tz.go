// Package tz is the server's time zone. It starts as the machine's (or the
// TZ environment variable's), and an admin can pick another in Settings,
// which is what the logs, holiday rows and anything else that goes by the
// server's clock use from the server's next start. It can't change while
// the server runs: Go's local time zone is read without a lock everywhere.
package tz

import (
	"context"
	"os"
	"time"
	_ "time/tzdata" // so a time zone can be picked on a machine without zoneinfo, like a minimal container
)

// Setting is the IANA name of the time zone the admin picked, such as
// America/Juneau; empty means the machine's own.
const Setting = "time_zone"

// inUse is the name of the time zone Load applied, "" for the machine's.
var inUse string

// System is the name of the machine's own time zone when it's known (from
// TZ), or "".
func System() string {
	return os.Getenv("TZ")
}

// InUse is the name of the time zone the server runs in: the admin's pick
// as of its start, or the machine's (System), or "" when that isn't known.
func InUse() string {
	if inUse != "" {
		return inUse
	}
	return System()
}

// Valid says whether name is a time zone this server knows; "" stands for
// the machine's own and is valid too.
func Valid(name string) bool {
	if name == "" {
		return true
	}
	if name == "Local" {
		return false
	}
	_, err := time.LoadLocation(name)
	return err == nil
}

// Store reads settings, as store.Store does.
type Store interface {
	Setting(ctx context.Context, key string) (string, error)
}

// Load switches the server to the time zone the admin picked. Call it once
// at startup, before anything else runs.
func Load(ctx context.Context, db Store) error {
	name, err := db.Setting(ctx, Setting)
	if err != nil || name == "" {
		return err
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil // no longer known to this build: keep the machine's
	}
	time.Local, inUse = loc, name
	return nil
}
