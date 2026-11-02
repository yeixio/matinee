package credits

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Frame is how dark one picture of a video is: the share of its pixels, in
// percent, that are close to black. At is from the start of the file.
type Frame struct {
	At    time.Duration
	Black int
}

// Tuning for finding credits in the picture. Closing credits are mostly
// light writing on black, so nearly every pixel is black; even a night
// scene with letterbox bars rarely gets that dark.
const (
	// darkBlack: a picture with at least this share of black pixels is dark.
	darkBlack = 90
	// blackLuma: a pixel with less brightness than this (of 255) is black.
	blackLuma = 32
	// gap: light pictures between dark ones this short don't break a dark
	// stretch (a logo, a coloured title card).
	gap = 8 * time.Second
	// joinGap: a dark stretch at least joinMin long still belongs to the
	// credits after a lighter passage this short (a scene halfway through).
	joinGap = 90 * time.Second
	joinMin = 60 * time.Second
	// afterSlack: what plays after the credits, a scene or a bumper, is at
	// most this long, or as long as the credits themselves.
	afterSlack = 2 * time.Minute
	// toTheEnd: credits that stop closer than this to the end of the file
	// run to the end.
	toTheEnd = 20 * time.Second
)

// MinDark is the shortest dark stretch that counts as credits.
var MinDark = 30 * time.Second

// FindDark finds where closing credits are in the frames of a video's last
// part, which ends at end: the last long dark stretch, and the dark
// stretches just before it when they're long too. stop is 0 when they run
// to the end. ok is false when nothing looks like credits.
func FindDark(frames []Frame, end time.Duration) (start, stop time.Duration, ok bool) {
	type run struct{ from, to time.Duration }
	var runs []run
	for i, f := range frames {
		if f.Black < darkBlack {
			continue
		}
		to := end // a frame stands for the time until the next one
		if i+1 < len(frames) {
			to = frames[i+1].At
		}
		if n := len(runs); n > 0 && f.At-runs[n-1].to <= gap {
			runs[n-1].to = to
		} else {
			runs = append(runs, run{f.At, to})
		}
	}
	last := -1
	for i := len(runs) - 1; i >= 0; i-- {
		if runs[i].to-runs[i].from >= MinDark {
			last = i
			break
		}
	}
	if last < 0 {
		return 0, 0, false
	}
	r := runs[last]
	if end-r.to > max(afterSlack, r.to-r.from) {
		return 0, 0, false // a dark scene, with too much after it
	}
	for i := last - 1; i >= 0; i-- {
		p := runs[i]
		if r.from-p.to > joinGap || p.to-p.from < joinMin {
			break
		}
		r.from = p.from
	}
	if end-r.to < toTheEnd {
		return r.from, 0, true
	}
	return r.from, r.to, true
}

var blackframeLine = regexp.MustCompile(`pblack:(\d+)\b.*?\bt:(-?[0-9.]+)`)

// Darkness reads how dark the pictures of a file's first video stream are,
// from start for up to limit. To be quick it looks only at key frames, a
// few seconds apart in most files; when they're further apart than that it
// decodes the stretch in full, one picture a second.
func Darkness(ctx context.Context, ffmpeg, file string, start, limit time.Duration, lowPriority func(pid int)) ([]Frame, error) {
	frames, err := darkness(ctx, ffmpeg, file, start, limit, true, lowPriority)
	if err != nil || ctx.Err() != nil {
		return frames, err
	}
	if len(frames) < 2 || limit/time.Duration(len(frames)) > 12*time.Second {
		return darkness(ctx, ffmpeg, file, start, limit, false, lowPriority)
	}
	return frames, nil
}

func darkness(ctx context.Context, ffmpeg, file string, start, limit time.Duration, keyOnly bool, lowPriority func(pid int)) ([]Frame, error) {
	args := []string{"-nostdin", "-hide_banner", "-nostats", "-loglevel", "info"}
	if keyOnly {
		args = append(args, "-skip_frame", "nokey")
	}
	filter := "scale=160:-2,blackframe=amount=0:threshold=" + strconv.Itoa(blackLuma)
	if !keyOnly {
		filter = "fps=1," + filter
	}
	args = append(args, "-ss", strconv.FormatFloat(start.Seconds(), 'f', 3, 64), "-i", file,
		"-t", strconv.FormatFloat(limit.Seconds(), 'f', 3, 64), "-map", "0:v:0", "-an", "-sn", "-dn",
		"-vf", filter, "-f", "null", "-")
	cmd := exec.CommandContext(ctx, ffmpeg, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("ffmpeg %s: %w", file, err)
	}
	if lowPriority != nil {
		lowPriority(cmd.Process.Pid)
	}
	if err := cmd.Wait(); err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("ffmpeg %s: %w", file, ctx.Err())
		}
		msg := strings.TrimSpace(stderr.String())
		if i := strings.LastIndexByte(msg, '\n'); i >= 0 {
			msg = msg[i+1:]
		}
		return nil, fmt.Errorf("ffmpeg %s: %w: %s", file, err, msg)
	}
	return parseBlackframe(&stderr, start), nil
}

// parseBlackframe reads FFmpeg's blackframe lines, whose times count from
// where reading started.
func parseBlackframe(out *bytes.Buffer, start time.Duration) []Frame {
	var frames []Frame
	sc := bufio.NewScanner(out)
	for sc.Scan() {
		line := sc.Text()
		if !strings.Contains(line, "blackframe") {
			continue
		}
		m := blackframeLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		pct, _ := strconv.Atoi(m[1])
		t, err := strconv.ParseFloat(m[2], 64)
		if err != nil || t < 0 {
			continue
		}
		at := start + time.Duration(t*float64(time.Second))
		if n := len(frames); n > 0 && at <= frames[n-1].At {
			continue
		}
		frames = append(frames, Frame{At: at, Black: pct})
	}
	return frames
}
