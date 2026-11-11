//go:build unix

package stream

import (
	"os"
	"syscall"
)

// pauseProcess stops p where it is (SIGSTOP), so a transcode saving space
// doesn't run further ahead of its player; resumeProcess lets it go on.
func pauseProcess(p *os.Process) bool { return p.Signal(syscall.SIGSTOP) == nil }

func resumeProcess(p *os.Process) { p.Signal(syscall.SIGCONT) }

// sameDevice says whether two files are on the same disk.
func sameDevice(a, b os.FileInfo) bool {
	sa, ok1 := a.Sys().(*syscall.Stat_t)
	sb, ok2 := b.Sys().(*syscall.Stat_t)
	return !ok1 || !ok2 || sa.Dev == sb.Dev
}
