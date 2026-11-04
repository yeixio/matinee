package stream

import (
	"io"
	"os"
	"time"
)

// probeBytes is how much ReadProbe reads: enough to see the speed of a
// network share, little enough not to slow anything down.
const probeBytes = 4 << 20

// ReadProbe reads a piece of the file at path from offset and says how
// long it took and how fast it went, giving up after limit (then ms is
// limit and bps how much it got through). Files elsewhere (addresses)
// aren't probed: ok is false.
func ReadProbe(path string, offset int64, limit time.Duration) (ms, bps int64, ok bool) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, false
	}
	type result struct {
		n   int64
		err error
	}
	start := time.Now()
	done := make(chan result, 1)
	go func() {
		defer f.Close()
		if size, err := f.Seek(0, io.SeekEnd); err == nil && offset+probeBytes > size {
			offset = max(size-probeBytes, 0)
		}
		n, err := io.Copy(io.Discard, io.NewSectionReader(f, offset, probeBytes))
		done <- result{n, err}
	}()
	select {
	case r := <-done:
		took := time.Since(start)
		if r.err != nil && r.n == 0 {
			return 0, 0, false
		}
		return took.Milliseconds(), int64(float64(r.n*8) / max(took.Seconds(), 0.001)), true
	case <-time.After(limit):
		// Still reading: as slow as it gets for this purpose.
		return limit.Milliseconds(), 0, true
	}
}
