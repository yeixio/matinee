//go:build !windows

package update

import (
	"os"
	"syscall"
)

// Exec replaces this process with exe, keeping its arguments and
// environment, so whatever started the server (systemd, launchd, a shell)
// sees the same process carry on as the new version.
func Exec(exe string) error {
	return syscall.Exec(exe, append([]string{exe}, os.Args[1:]...), os.Environ())
}
