//go:build unix

package main

import (
	"os"
	"syscall"
)

// restart replaces this process with the binary now at its path, with the
// same arguments and environment, so whatever supervises it (systemd, a
// container, the Mac app) sees no change.
func restart() error {
	return syscall.Exec(restartPath, os.Args, os.Environ())
}

// canRestart is whether restart works here: on Unix it always does.
func canRestart() bool { return true }
