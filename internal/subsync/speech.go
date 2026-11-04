// Package subsync lines a subtitle file up with its video. Subtitles found
// online or ripped for another release often show too early or too late,
// or drift further off as the film goes on because they were timed for a
// different frame rate. subsync listens to the video for where people are
// talking, reads where the subtitles say lines are spoken, and finds the
// shift (and, when it fits better, the change of speed) that lines the two
// up best.
package subsync

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os/exec"
	"strconv"
	"strings"
)

// FrameMs is how long each step of a speech map is.
const FrameMs = 20

const (
	sampleRate     = 8000
	frameSamples   = sampleRate * FrameMs / 1000
	framesPerSec   = 1000 / FrameMs
	floorWindow    = 4 * framesPerSec // the noise floor is the quietest moment this close
	modulateWindow = framesPerSec / 4 // speech rises and falls with each syllable
)

// Speech is a video's speech map: for each FrameMs step, how likely it is
// that someone is talking, from 0 to 254, or Unheard.
type Speech []byte

// Unheard marks the parts of a speech map that weren't listened to.
const Unheard = 255

// Window is a part of a video to listen to.
type Window struct{ StartMs, LenMs int64 }

// Windows picks the parts of a long video worth listening to: three
// stretches spread over it, which is enough to tell both how far the
// subtitles are off and whether they drift, without reading the whole file.
// A shorter video is listened to whole (nil).
func Windows(durationMs int64) []Window {
	const part = 12 * 60 * 1000
	if durationMs < 4*part {
		return nil
	}
	var out []Window
	for _, at := range []float64{0.08, 0.45, 0.8} {
		out = append(out, Window{StartMs: int64(float64(durationMs) * at), LenMs: part})
	}
	return out
}

// Audio says which of a video's audio streams to listen to.
type Audio struct {
	Stream int // among the file's audio streams, as in FFmpeg's 0:a:N
	// Center listens to the front center channel only, where films and
	// series put the dialogue, when the stream has one (5.1 and up).
	Center bool
}

// Listen decodes one audio stream of a video and maps where people talk.
// lowPriority, when set, is given FFmpeg's process id once it starts, so
// listening can make way for everything else on the server.
// With windows, only those parts are listened to, and the rest of the map
// is Unheard.
func Listen(ctx context.Context, ffmpeg, file string, a Audio, windows []Window, lowPriority func(pid int)) (Speech, error) {
	if len(windows) == 0 {
		return listenCenter(ctx, ffmpeg, file, a, Window{}, lowPriority)
	}
	var out Speech
	for _, w := range windows {
		sp, err := listenCenter(ctx, ffmpeg, file, a, w, lowPriority)
		if err != nil {
			return nil, err
		}
		at := int(w.StartMs / FrameMs)
		for len(out) < at {
			out = append(out, Unheard)
		}
		out = append(out[:at], sp...)
	}
	return out, nil
}

func listenCenter(ctx context.Context, ffmpeg, file string, a Audio, w Window, lowPriority func(pid int)) (Speech, error) {
	sp, err := listen(ctx, ffmpeg, file, a, w, lowPriority)
	if err != nil && a.Center && ctx.Err() == nil {
		// The layout didn't have a center channel after all: mix it down.
		a.Center = false
		return listen(ctx, ffmpeg, file, a, w, lowPriority)
	}
	return sp, err
}

func listen(ctx context.Context, ffmpeg, file string, a Audio, w Window, lowPriority func(pid int)) (Speech, error) {
	// Voices sit between about 200 Hz and 3.4 kHz; most rumble, hiss and
	// cymbals don't, so cutting the rest makes talking stand out.
	filter := "highpass=f=200,lowpass=f=3400"
	if a.Center {
		filter = "pan=mono|c0=FC," + filter
	}
	args := []string{"-nostdin", "-v", "error"}
	if w.LenMs > 0 {
		args = append(args, "-ss", ms(w.StartMs), "-t", ms(w.LenMs))
	}
	args = append(args, "-i", file, "-map", "0:a:"+strconv.Itoa(a.Stream),
		"-vn", "-sn", "-dn", "-af", filter, "-ac", "1", "-ar", strconv.Itoa(sampleRate), "-f", "s16le", "-")
	cmd := exec.CommandContext(ctx, ffmpeg, args...)
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	if lowPriority != nil {
		lowPriority(cmd.Process.Pid)
	}
	levels, readErr := frameLevels(bufio.NewReaderSize(out, 64<<10))
	if err := cmd.Wait(); err != nil {
		return nil, fmt.Errorf("ffmpeg audio: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	if readErr != nil {
		return nil, readErr
	}
	if len(levels) < 10*framesPerSec {
		return nil, errors.New("too little audio to listen to")
	}
	return Detect(levels), nil
}

// frameLevels reads 16-bit mono samples and returns each frame's loudness
// in decibels below full scale.
func frameLevels(r io.Reader) ([]float64, error) {
	buf := make([]byte, frameSamples*2)
	var out []float64
	for {
		n, err := io.ReadFull(r, buf)
		if n == len(buf) {
			var sum float64
			for i := 0; i < n; i += 2 {
				v := float64(int16(binary.LittleEndian.Uint16(buf[i:]))) / 32768
				sum += v * v
			}
			out = append(out, 10*math.Log10(sum/frameSamples+1e-10))
		}
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return out, nil
		}
		if err != nil {
			return out, err
		}
	}
}

// Detect turns frame loudness (in dB) into a speech map. Speech is louder
// than the quiet moments around it, and it rises and falls several times a
// second with each syllable, where music and background noise mostly hold
// steady. A frame scores high when it has both.
func Detect(levels []float64) Speech {
	n := len(levels)
	floor := slidingMin(smooth(levels, 5), floorWindow)
	// How much the loudness moves around each frame: its spread over a
	// quarter second.
	mean, sq := make([]float64, n+1), make([]float64, n+1)
	for i, v := range levels {
		mean[i+1] = mean[i] + v
		sq[i+1] = sq[i] + v*v
	}
	out := make(Speech, n)
	for i, v := range levels {
		if v < -60 {
			continue // silence
		}
		lo, hi := max(0, i-modulateWindow), min(n, i+modulateWindow+1)
		k := float64(hi - lo)
		m := (mean[hi] - mean[lo]) / k
		spread := math.Sqrt(max(0, (sq[hi]-sq[lo])/k-m*m))
		above := clamp((v-floor[i]-4)/14, 0, 1)
		moves := clamp((spread-1.5)/5, 0, 1)
		out[i] = byte(math.Round(254 * math.Sqrt(above*moves)))
	}
	return out
}

func smooth(v []float64, w int) []float64 {
	sum := make([]float64, len(v)+1)
	for i, x := range v {
		sum[i+1] = sum[i] + x
	}
	out := make([]float64, len(v))
	for i := range v {
		lo, hi := max(0, i-w), min(len(v), i+w+1)
		out[i] = (sum[hi] - sum[lo]) / float64(hi-lo)
	}
	return out
}

// slidingMin is the smallest value within w steps of each one.
func slidingMin(v []float64, w int) []float64 {
	out := make([]float64, len(v))
	var q []int // indexes whose values increase
	j := 0      // next index to add
	for i := range v {
		for ; j < len(v) && j <= i+w; j++ {
			for len(q) > 0 && v[q[len(q)-1]] >= v[j] {
				q = q[:len(q)-1]
			}
			q = append(q, j)
		}
		for q[0] < i-w {
			q = q[1:]
		}
		out[i] = v[q[0]]
	}
	return out
}

func ms(v int64) string { return strconv.FormatFloat(float64(v)/1000, 'f', 3, 64) }

func clamp(v, lo, hi float64) float64 { return math.Max(lo, math.Min(hi, v)) }
