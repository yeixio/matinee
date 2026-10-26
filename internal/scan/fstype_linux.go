package scan

import "golang.org/x/sys/unix"

// Filesystems whose changes can come from another machine, so change
// notifications miss them (statfs f_type magic numbers, see statfs(2)).
var networkFSTypes = map[uint32]string{
	0x6969:     "nfs",
	0xFF534D42: "cifs",
	0xFE534D42: "smb2",
	0x517B:     "smb",
	0x65735546: "fuse",
	0x01021997: "9p",
	0x00C36400: "ceph",
	0x47504653: "gpfs",
	0x6B414653: "afs",
	0x013111A8: "ibrix",
}

// networkFS reports whether path is on a network share, and which kind.
func networkFS(path string) (string, bool) {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return "", false
	}
	name, ok := networkFSTypes[uint32(st.Type)]
	return name, ok
}
