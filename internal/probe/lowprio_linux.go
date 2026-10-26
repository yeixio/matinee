package probe

import "syscall"

// lowerPriority makes a process yield the CPU (nice 19) and the disk (the
// idle I/O class, like ionice -c3) to everything else.
func lowerPriority(pid int) {
	syscall.Setpriority(syscall.PRIO_PROCESS, pid, 19)
	const ioprioWhoProcess, ioprioClassIdle, ioprioClassShift = 1, 3, 13
	syscall.Syscall(syscall.SYS_IOPRIO_SET, ioprioWhoProcess, uintptr(pid), ioprioClassIdle<<ioprioClassShift)
}
