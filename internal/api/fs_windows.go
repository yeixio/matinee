package api

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// isDriveList is whether path asks for the drives: / or \ on its own, which
// would otherwise mean the root of whichever drive the server started on.
func isDriveList(path string) bool { return path == "/" || path == `\` }

// listDrives lists the drives that can be read, network drives mapped for the
// account the server runs as included.
func listDrives() []string {
	drives := []string{}
	mask, err := windows.GetLogicalDrives()
	if err != nil {
		return drives
	}
	for i := range 26 {
		if mask&(1<<i) == 0 {
			continue
		}
		d := string(rune('A'+i)) + `:\`
		// An empty card reader or DVD drive can't be read.
		if _, err := os.Stat(d); err == nil {
			drives = append(drives, d)
		}
	}
	return drives
}

// parentDir goes up from a drive's root to the list of drives. A network
// share's root (\\nas\media) has nothing above it to go to.
func parentDir(dir string) string {
	p := filepath.Dir(dir)
	if p == dir && len(filepath.VolumeName(dir)) == 2 {
		return "/"
	}
	return p
}
