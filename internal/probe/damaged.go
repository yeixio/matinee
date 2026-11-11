package probe

import (
	"context"
	"errors"
	"os/exec"
	"strconv"
	"time"

	"github.com/yeixio/matinee/internal/slowfs"
)

// jumpWait is how long Damaged gives FFmpeg to read a second of a file
// after jumping to a point in it. A healthy file on a share that's
// answering takes well under one.
var jumpWait = 15 * time.Second

// Damaged tells a damaged file from a share that was just slow, after a
// background job's read of the file timed out: it jumps to a few points in
// the file (a tenth, half and nine tenths of the way in, or 1, 10 and 30
// minutes when the length isn't known) and reads a second at each. A file
// that a jump times out on is damaged, say one whose seek index points past
// its end or whose end was never written. ffmpeg is FFmpeg's path, and
// lowPriority, when set, is called with each FFmpeg's process id.
func Damaged(ctx context.Context, ffmpeg, path string, durationMs int64, lowPriority func(pid int)) bool {
	points := []float64{60, 600, 1800}
	if durationMs > 0 {
		d := float64(durationMs) / 1000
		points = []float64{d * 0.1, d * 0.5, d * 0.9}
	}
	for _, at := range points {
		jctx, cancel := context.WithTimeout(ctx, jumpWait)
		cmd := exec.CommandContext(jctx, ffmpeg, "-nostdin", "-v", "error", "-ss", strconv.FormatFloat(at, 'f', 1, 64),
			"-i", path, "-t", "1", "-f", "null", "-")
		err := cmd.Start()
		if err == nil {
			if lowPriority != nil {
				lowPriority(cmd.Process.Pid)
			}
			err = cmd.Wait()
		}
		timedOut := errors.Is(jctx.Err(), context.DeadlineExceeded)
		cancel()
		if ctx.Err() != nil {
			return false // stopping: no verdict
		}
		if timedOut {
			// Unless the share stopped answering meanwhile: then it's the
			// share's fault, and there's no verdict on the file.
			return slowfs.Answering(path)
		}
	}
	return false
}
