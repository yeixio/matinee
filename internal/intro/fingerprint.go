// Package intro finds where a TV show's intro (its theme song or title
// sequence) plays in each episode, so players can offer to skip it.
//
// Episodes of a season share their intro's audio, so the package decodes
// the start of each episode's soundtrack into an audio fingerprint, lines
// two episodes' fingerprints up at every possible offset, and takes the
// longest stretch they have in common as the intro in both. Chapters named
// Intro or Opening, when a file has them, come first (see probe).
package intro

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"math/bits"
	"math/cmplx"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// The fingerprint follows Haitsma and Kalker's robust audio hash: the audio
// is resampled to mono at sampleRate, cut into overlapping frames of
// frameSize samples every hop samples, and each frame's spectrum between
// lowHz and highHz is split into 33 bands on a log scale. Bit m of a frame's
// 32-bit value says whether the energy difference between bands m and m+1
// grew or shrank since the frame before. The same sound gives (nearly) the
// same bits whatever codec, bitrate or volume it went through.
const (
	sampleRate = 5512
	frameSize  = 2048 // about 0.37 s
	hop        = 256  // about 46 ms, so about 21.5 values a second
	lowHz      = 300
	highHz     = 2000
	bands      = 33
)

// FrameMs is how much audio each fingerprint value stands for.
const FrameMs = float64(hop) * 1000 / sampleRate

// Fingerprint decodes up to limit of a file's first audio stream with
// FFmpeg and fingerprints it. lowPriority, when set, is called with the
// FFmpeg process id as soon as it starts.
func Fingerprint(ctx context.Context, ffmpeg, file string, limit time.Duration, lowPriority func(pid int)) (Print, error) {
	args := []string{"-nostdin", "-v", "error", "-i", file, "-map", "0:a:0", "-t", strconv.FormatFloat(limit.Seconds(), 'f', 3, 64),
		"-vn", "-sn", "-dn", "-ac", "1", "-ar", strconv.Itoa(sampleRate), "-f", "s16le", "-acodec", "pcm_s16le", "-"}
	cmd := exec.CommandContext(ctx, ffmpeg, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		return Print{}, err
	}
	if err := cmd.Start(); err != nil {
		return Print{}, fmt.Errorf("ffmpeg %s: %w", file, err)
	}
	if lowPriority != nil {
		lowPriority(cmd.Process.Pid)
	}
	samples, readErr := readSamples(out, int(limit.Seconds()*sampleRate)+frameSize)
	if err := cmd.Wait(); err != nil || readErr != nil {
		if ctx.Err() != nil {
			return Print{}, fmt.Errorf("ffmpeg %s: %w", file, ctx.Err())
		}
		if err == nil {
			err = readErr
		}
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			msg, _, _ = strings.Cut(msg, "\n")
			return Print{}, fmt.Errorf("ffmpeg %s: %w: %s", file, err, msg)
		}
		return Print{}, fmt.Errorf("ffmpeg %s: %w", file, err)
	}
	return FingerprintSamples(samples), nil
}

func readSamples(r io.Reader, max int) ([]float64, error) {
	samples := make([]float64, 0, min(max, 1<<20))
	buf := make([]byte, 32*1024)
	var carry []byte
	for {
		n, err := r.Read(buf)
		data := append(carry, buf[:n]...)
		for len(data) >= 2 {
			if len(samples) < max {
				samples = append(samples, float64(int16(binary.LittleEndian.Uint16(data)))/32768)
			}
			data = data[2:]
		}
		carry = append(carry[:0], data...)
		if err == io.EOF {
			return samples, nil
		}
		if err != nil {
			return samples, err
		}
	}
}

// Print is a stretch of audio's fingerprint: one value per FrameMs, and
// whether each frame was too quiet to say anything. Silence looks the same
// in every episode, so quiet frames never count as shared.
type Print struct {
	Values []uint32
	Quiet  []bool
}

// Len is how many frames the print has.
func (p Print) Len() int { return len(p.Values) }

// quietEnergy is a frame's energy between lowHz and highHz below which it
// counts as silence, about 60 dB below a full-scale tone.
const quietEnergy = 0.25

// FingerprintSamples fingerprints mono audio at sampleRate, samples
// between -1 and 1.
func FingerprintSamples(samples []float64) Print {
	if len(samples) < frameSize+hop {
		return Print{}
	}
	n := (len(samples)-frameSize)/hop + 1
	window := make([]float64, frameSize)
	for i := range window {
		window[i] = 0.5 - 0.5*math.Cos(2*math.Pi*float64(i)/float64(frameSize-1)) // Hann
	}
	// Band edges as FFT bins, spaced evenly on a log scale.
	edges := make([]int, bands+1)
	for b := range edges {
		hz := lowHz * math.Pow(float64(highHz)/lowHz, float64(b)/bands)
		edges[b] = int(math.Round(hz * frameSize / sampleRate))
	}
	out := make([]uint32, n)
	quiet := make([]bool, n)
	buf := make([]complex128, frameSize)
	prev := make([]float64, bands)
	cur := make([]float64, bands)
	for f := 0; f < n; f++ {
		start := f * hop
		for i := range buf {
			buf[i] = complex(samples[start+i]*window[i], 0)
		}
		fft(buf)
		total := 0.0
		for b := 0; b < bands; b++ {
			e := 0.0
			for k := edges[b]; k < edges[b+1]; k++ {
				e += real(buf[k])*real(buf[k]) + imag(buf[k])*imag(buf[k])
			}
			cur[b] = e
			total += e
		}
		quiet[f] = total < quietEnergy
		if f > 0 {
			var v uint32
			for m := 0; m < bands-1; m++ {
				if (cur[m]-cur[m+1])-(prev[m]-prev[m+1]) > 0 {
					v |= 1 << m
				}
			}
			out[f] = v
		}
		prev, cur = cur, prev
	}
	return Print{Values: out[1:], Quiet: quiet[1:]}
}

// fft is an in-place radix-2 Cooley–Tukey FFT; len(a) must be a power of two.
func fft(a []complex128) {
	n := len(a)
	for i, j := 1, 0; i < n; i++ {
		bit := n >> 1
		for ; j&bit != 0; bit >>= 1 {
			j ^= bit
		}
		j ^= bit
		if i < j {
			a[i], a[j] = a[j], a[i]
		}
	}
	for size := 2; size <= n; size <<= 1 {
		w := cmplx.Exp(complex(0, -2*math.Pi/float64(size)))
		for start := 0; start < n; start += size {
			wk := complex(1, 0)
			for k := 0; k < size/2; k++ {
				u, v := a[start+k], a[start+k+size/2]*wk
				a[start+k], a[start+k+size/2] = u+v, u-v
				wk *= w
			}
		}
	}
}

// distance is how many of two fingerprint values' 32 bits differ.
func distance(a, b uint32) int { return bits.OnesCount32(a ^ b) }
