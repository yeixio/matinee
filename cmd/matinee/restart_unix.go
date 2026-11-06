//go:build unix

package main

import (
	"os"
	"syscall"
)

// restart replaces this process with the binary now at its path, with the
// same arguments and environment, so whatever supervises it sees no change.
func restart() error {
	return syscall.Exec(restartPath, os.Args, os.Environ())
}
