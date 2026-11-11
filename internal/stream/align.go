package stream

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/yeixio/matinee/internal/store"
)

// alignedRate is the sample rate the sound of a copied picture is made at,
// so the alignment below can count in samples, and aacFrame how many
// samples one AAC frame holds (its encoder delay is one of them).
const (
	alignedRate = 48000
	aacFrame    = 1024
)

// alignTimeout is how long finding where a copied picture starts may take
// before the session starts without lining the sound up.
var alignTimeout = 10 * time.Second

// Without edit lists (see the fMP4 options in start), every track of an
// fMP4 piece starts at time zero, whatever time its first sample really
// has. A copied picture starts on the keyframe at or before where playback
// starts, and its decoding a few frames before that keyframe shows; the
// sound starts where playback starts. FFmpeg covered the difference by
// stretching the sound's first sample, which Apple's players play at its
// usual length, so everything after it was heard early: by up to a second
// on a film with a keyframe every second, more with fewer keyframes.
//
// So the sound starts where the picture's decoding does: silence is put
// in front of it back to that moment, less the AAC encoder delay, whose
// frame comes first. Then both tracks start at zero together and no sample
// is stretched.

// firstCopiedDTS is where the copied picture's decoding starts, in seconds
// against startSec (negative: before it), as FFmpeg sees it when it copies
// the picture from startSec.
func (t *Transcoder) firstCopiedDTS(ctx context.Context, it store.Item, startSec float64) (float64, error) {
	ctx, cancel := context.WithTimeout(ctx, alignTimeout)
	defer cancel()
	args := []string{"-hide_banner", "-loglevel", "error", "-nostdin"}
	if startSec > 0 {
		args = append(args, "-ss", strconv.FormatFloat(startSec, 'f', 3, 64))
	}
	args = append(args, Input(it)...)
	args = append(args, "-map", "0:v:0", "-c:v", "copy", "-frames:v", "1", "-f", "framecrc", "-")
	cmd := exec.CommandContext(ctx, t.FFmpeg, args...)
	cmd.SysProcAttr = procAttr()
	out, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("find where the picture starts: %w", err)
	}
	return parseFirstDTS(string(out))
}

// parseFirstDTS reads the first packet's decoding time from FFmpeg's
// framecrc output, in seconds.
func parseFirstDTS(out string) (float64, error) {
	num, den := int64(0), int64(0)
	for _, l := range strings.Split(out, "\n") {
		l = strings.TrimSpace(l)
		if tb, ok := strings.CutPrefix(l, "#tb 0:"); ok {
			n, d, ok := strings.Cut(strings.TrimSpace(tb), "/")
			if !ok {
				break
			}
			num, _ = strconv.ParseInt(n, 10, 64)
			den, _ = strconv.ParseInt(d, 10, 64)
			continue
		}
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		f := strings.Split(l, ",")
		if len(f) < 3 || den == 0 {
			break
		}
		dts, err := strconv.ParseInt(strings.TrimSpace(f[1]), 10, 64)
		if err != nil {
			break
		}
		return float64(dts*num) / float64(den), nil
	}
	return 0, errors.New("no packet in FFmpeg's framecrc output")
}

// AlignFilter is the audio filter that starts the sound dts seconds from
// where playback starts (see firstCopiedDTS), padding it with silence (or
// trimming it, after) so its first AAC frame, the encoder delay, lines up
// with the picture's first one. It fills any gap later in the sound with
// silence too, so no sample is stretched there either.
func AlignFilter(dts float64) string {
	first := int64(dts*alignedRate+0.5*sign(dts)) + aacFrame
	return "aresample=async=1:first_pts=" + strconv.FormatInt(first, 10)
}

func sign(f float64) float64 {
	if f < 0 {
		return -1
	}
	return 1
}
