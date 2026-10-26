//go:build unix

package hwaccel

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// deviceProblem explains why the render node at path can't be opened, or
// returns "" when it can (or when the reason isn't a permission).
func deviceProblem(path string) string {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err == nil {
		f.Close()
		return ""
	}
	switch {
	case errors.Is(err, syscall.EACCES):
		gid := "the group that owns it"
		if st, err := os.Stat(path); err == nil {
			if sys, ok := st.Sys().(*syscall.Stat_t); ok {
				gid = fmt.Sprintf("group %d, which owns it", sys.Gid)
			}
		}
		return fmt.Sprintf("Matinee isn't allowed to open the GPU at %s. Add Matinee's user to %s "+
			"(Helm: gpu.vaapi.groups; Docker: group_add).", path, gid)
	case errors.Is(err, syscall.EPERM):
		// The container's device rules, not file permissions.
		return fmt.Sprintf("The container isn't allowed to use the GPU at %s. With Helm, set gpu.type: vaapi; "+
			"with Docker, pass it in with devices.", path)
	}
	return ""
}
