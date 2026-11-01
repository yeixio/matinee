//go:build !linux && !windows && !darwin && !freebsd && !netbsd && !openbsd

package health

import "runtime"

func read() reading {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	return reading{scope: ScopeProcess, procMem: ms.Sys}
}

func diskUsage(string) (Disk, bool) { return Disk{}, false }
