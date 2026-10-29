// Package spectrum works out how loud each part of a song's sound is, moment
// by moment, for the apps' music visualizer. The apps can't hear what they
// play (an Apple TV has no way to), so the server listens once, with FFmpeg,
// and they draw from the levels in time with the music.
package spectrum

import (
	"bufio"
	"context"
	"crypto/sha1"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"math/cmplx"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/yeixio/matinee/internal/probe"
)

const (
	// FPS is how many moments a second the levels describe.
	FPS = 20
	// Bands is how many parts of the sound each moment has, from the bass
	// (40 Hz) up to the treble (5 kHz), spaced the way ears hear them.
	Bands = 24

	rate   = 10240 // samples a second, enough for 5 kHz
	hop    = rate / FPS
	window = 1024 // a tenth of a second
	lowHz  = 40.0
	highHz = rate / 2.0
	// Each band is scaled to its own loud moments, so the treble moves as
	// much as the bass; anything this many decibels below them is silence.
	floorDB = 42.0
	// How much quieter than the loudest band a band can be and still be
	// scaled to its own loud moments.
	quieterDB = 24.0
	// The longest sound worth listening to: a two-hour mix.
	maxFrames = 2 * 60 * 60 * FPS
	// Changing how levels are worked out makes the cached ones stale.
	version = 1
)

// Analyzer works out songs' levels and keeps them, so each file is listened
// to once per version of it.
type Analyzer struct {
	FFmpeg string
	Dir    string
	// Timeout is the longest one file may take; 0 means five minutes.
	Timeout time.Duration

	mu       sync.Mutex
	inflight map[string]*sync.Mutex
	// A couple at a time: playing a queue asks for the next song early.
	slots chan struct{}
}

func New(ffmpeg, dir string) *Analyzer {
	os.MkdirAll(dir, 0o755)
	return &Analyzer{FFmpeg: ffmpeg, Dir: dir, inflight: map[string]*sync.Mutex{}, slots: make(chan struct{}, 2)}
}

// Levels returns the file's levels: Bands bytes for each moment, 0 for
// silence to 255 for as loud as that band gets in the song.
func (a *Analyzer) Levels(ctx context.Context, path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	sum := sha1.Sum([]byte(fmt.Sprintf("%d|%s|%d|%d", version, path, info.Size(), info.ModTime().UnixNano())))
	key := filepath.Join(a.Dir, hex.EncodeToString(sum[:]))
	if b, err := os.ReadFile(key); err == nil {
		return b, nil
	}
	a.mu.Lock()
	m, ok := a.inflight[key]
	if !ok {
		m = &sync.Mutex{}
		a.inflight[key] = m
	}
	a.mu.Unlock()
	m.Lock()
	defer func() {
		m.Unlock()
		a.mu.Lock()
		delete(a.inflight, key)
		a.mu.Unlock()
	}()
	if b, err := os.ReadFile(key); err == nil {
		return b, nil
	}
	select {
	case a.slots <- struct{}{}:
		defer func() { <-a.slots }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	b, err := a.listen(ctx, path)
	if err != nil {
		return nil, err
	}
	tmp := key + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err == nil {
		os.Rename(tmp, key)
	}
	return b, nil
}

// listen decodes the file's sound to mono with FFmpeg and works out its levels.
func (a *Analyzer) listen(ctx context.Context, path string) ([]byte, error) {
	timeout := a.Timeout
	if timeout == 0 {
		timeout = 5 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, a.FFmpeg, "-v", "error", "-nostdin", "-i", path,
		"-map", "0:a:0", "-vn", "-ac", "1", "-ar", strconv.Itoa(rate), "-f", "s16le", "-")
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("ffmpeg %s: %w", path, err)
	}
	// Nobody waits on this but the visualizer, which can start without it.
	probe.LowerPriority(cmd.Process.Pid)
	frames, readErr := Analyze(bufio.NewReaderSize(out, 64<<10))
	if readErr != nil {
		cmd.Process.Kill()
	}
	waitErr := cmd.Wait()
	if ctx.Err() != nil {
		return nil, fmt.Errorf("ffmpeg %s: %w", path, ctx.Err())
	}
	if readErr != nil {
		return nil, readErr
	}
	if waitErr != nil && len(frames) == 0 {
		return nil, fmt.Errorf("ffmpeg %s: %w", path, waitErr)
	}
	return frames, nil
}

// Analyze reads 16-bit little-endian mono samples at 10240 a second and
// returns their levels.
func Analyze(r io.Reader) ([]byte, error) {
	edges := bandEdges()
	hann := make([]float64, window)
	for i := range hann {
		hann[i] = 0.5 - 0.5*math.Cos(2*math.Pi*float64(i)/float64(window-1))
	}
	buf := make([]float64, window)
	in := make([]byte, hop*2)
	spec := make([]complex128, window)
	var db [][Bands]float32
	filled := window - hop // the first moment starts half a window early, in silence
	for len(db) < maxFrames {
		n, err := io.ReadFull(r, in)
		if n == 0 {
			if err == io.EOF || errors.Is(err, io.ErrUnexpectedEOF) {
				break
			}
			if err != nil {
				return nil, err
			}
		}
		copy(buf, buf[hop:])
		for i := range hop {
			var s float64
			if 2*i+1 < n {
				s = float64(int16(binary.LittleEndian.Uint16(in[2*i:]))) / 32768
			}
			buf[filled+i] = s
		}
		filled = window - hop
		for i, s := range buf {
			spec[i] = complex(s*hann[i], 0)
		}
		fft(spec)
		var f [Bands]float32
		for b := range Bands {
			lo, hi := edges[b], edges[b+1]
			var p float64
			for k := lo; k < hi; k++ {
				p += real(spec[k])*real(spec[k]) + imag(spec[k])*imag(spec[k])
			}
			f[b] = float32(10 * math.Log10(p/float64(hi-lo)+1e-12))
		}
		db = append(db, f)
		if err != nil {
			break
		}
	}
	return scale(db), nil
}

// scale maps each band's decibels to 0–255, its loud moments (the 98th
// percentile, so one bang doesn't flatten the rest) near the top. A band far
// quieter than the loudest one is scaled to that one instead, so a hiss
// nobody hears doesn't dance as much as the beat.
func scale(db [][Bands]float32) []byte {
	out := make([]byte, len(db)*Bands)
	if len(db) == 0 {
		return out
	}
	var tops [Bands]float64
	loudest := math.Inf(-1)
	col := make([]float32, len(db))
	for b := range Bands {
		for i := range db {
			col[i] = db[i][b]
		}
		slices.Sort(col)
		tops[b] = float64(col[min(len(col)-1, len(col)*98/100)])
		loudest = math.Max(loudest, tops[b])
	}
	for b := range Bands {
		top := math.Max(tops[b], loudest-quieterDB)
		// A band that's silent throughout stays still.
		if tops[b] < -100 {
			continue
		}
		for i := range db {
			v := (float64(db[i][b]) - (top - floorDB)) / floorDB
			out[i*Bands+b] = byte(math.Round(math.Max(0, math.Min(1, v)) * 255))
		}
	}
	return out
}

// bandEdges returns the FFT bins each band starts at, the last one's end
// after them: log-spaced, at least one bin wide.
func bandEdges() [Bands + 1]int {
	var e [Bands + 1]int
	binHz := float64(rate) / window
	for b := range Bands + 1 {
		hz := lowHz * math.Pow(highHz/lowHz, float64(b)/Bands)
		e[b] = int(math.Round(hz / binHz))
		if b > 0 && e[b] <= e[b-1] {
			e[b] = e[b-1] + 1
		}
	}
	e[Bands] = min(e[Bands], window/2)
	return e
}

// fft transforms x in place (its length a power of two).
func fft(x []complex128) {
	n := len(x)
	for i, j := 1, 0; i < n; i++ {
		bit := n >> 1
		for ; j&bit != 0; bit >>= 1 {
			j ^= bit
		}
		j ^= bit
		if i < j {
			x[i], x[j] = x[j], x[i]
		}
	}
	for size := 2; size <= n; size <<= 1 {
		w := cmplx.Exp(complex(0, -2*math.Pi/float64(size)))
		for start := 0; start < n; start += size {
			wk := complex(1, 0)
			for k := range size / 2 {
				a, b := x[start+k], x[start+k+size/2]*wk
				x[start+k], x[start+k+size/2] = a+b, a-b
				wk *= w
			}
		}
	}
}
