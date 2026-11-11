//go:build !unix

package main

import (
	"errors"
	"os"
)

// restartExitCode asks the Windows tray app (cmd/matinee-tray), which
// starts the server, to start it again at once.
const restartExitCode = 75

// restart has the tray app start the server again: Windows can't replace a
// running process with another.
func restart() error {
	if !canRestart() {
		return errors.New("restarting isn't supported here")
	}
	os.Exit(restartExitCode)
	return nil
}

// canRestart is whether the tray app started this server, so it starts it
// again once it stops.
func canRestart() bool { return os.Getenv("MATINEE_STOP_ON_STDIN_CLOSE") == "1" }
