//go:build !linux && !windows && !darwin && !freebsd && !openbsd && !netbsd

package probe

func lowerPriority(pid int) {}
