//go:build !unix

package stream

import "os"

// Windows has no SIGSTOP: a transcode saving space only clears pieces
// behind its player there.
func pauseProcess(*os.Process) bool { return false }

func resumeProcess(*os.Process) {}

func sameDevice(a, b os.FileInfo) bool { return true }
