package api

import (
	"testing"

	"github.com/yeixio/matinee/internal/health"
)

// Shares of one NAS read a moment apart are one disk, even while files are
// being written to it; disks of different sizes, or far apart in free
// space, aren't.
func TestSameDisk(t *testing.T) {
	const tb = uint64(1) << 40
	nas := health.Disk{Total: 27 * tb, Free: 2 * tb}
	for _, c := range []struct {
		other health.Disk
		want  bool
	}{
		{nas, true},
		{health.Disk{Total: 27 * tb, Free: 2*tb - 300<<20}, true},
		{health.Disk{Total: 27 * tb, Free: 2*tb + 900<<20}, true},
		{health.Disk{Total: 27 * tb, Free: 2*tb - 5<<30}, false},
		{health.Disk{Total: 4 * tb, Free: 2 * tb}, false},
	} {
		if got := sameDisk(nas, c.other); got != c.want {
			t.Errorf("sameDisk(%v, %v) = %v, want %v", nas, c.other, got, c.want)
		}
	}
}
