//go:build darwin || freebsd || openbsd || netbsd

package probe

import "syscall"

// lowerPriority makes a process yield the CPU to everything else (nice 19).
// macOS and the BSDs throttle background disk use by themselves.
func lowerPriority(pid int) {
	syscall.Setpriority(syscall.PRIO_PROCESS, pid, 19)
}
