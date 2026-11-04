package subsync

import (
	"errors"
	"math"
	"math/bits"
	"math/cmplx"
	"slices"
)

// Rates are the changes of speed worth trying: subtitles timed for a film
// at 24 or 23.976 frames a second drift when the video runs at another of
// 23.976, 24 or 25 (European TV and discs speed films up to 25).
var Rates = []float64{1, 23.976 / 24, 24 / 23.976, 23.976 / 25, 25 / 23.976, 24 / 25, 25 / 24}

// MaxOffsetMs is the furthest Align looks: subtitles for a release with a
// longer studio logo or a recap are a few seconds to minutes off.
const MaxOffsetMs = 15 * 60 * 1000

// Result is the best fit Align found.
type Result struct {
	Shift
	// Score is how clearly the fit stands out from all the others it
	// tried: how many spreads above the typical one it is.
	Score float64 `json:"score"`
	// Sure says the fit stands out enough to trust.
	Sure bool `json:"sure"`
}

// MinScore is the Score a fit needs to be trusted. Fits for the wrong
// video or a badly broken file stay well below it.
const MinScore = 9

// rateMargin is how much better a change of speed has to fit than none:
// a nearly matching speed fits almost as well, and none is far likelier.
const rateMargin = 1.04

var ErrNoCues = errors.New("no spoken lines to line up")

// Reference is what the subtitles are lined up with, one value per FrameMs
// step: a speech map, or another subtitle track's lines (see CueTrack).
type Reference []float64

// SpeechReference is a reference from a speech map. What wasn't listened
// to is NaN, which counts for nothing either way.
func SpeechReference(s Speech) Reference {
	out := make(Reference, len(s))
	for i, v := range s {
		out[i] = float64(v) / 254
		if v == Unheard {
			out[i] = math.NaN()
		}
	}
	return out
}

// CueReference is a reference from a track whose times are right, such as
// subtitles inside the video.
func CueReference(cues []Cue) Reference {
	return Reference(cueSignal(cues, 1))
}

// Align finds the shift that lines cues up with ref best.
func Align(ref Reference, cues []Cue) (Result, error) {
	spoken := 0
	for _, c := range cues {
		if !c.Sound {
			spoken++
		}
	}
	if spoken < 10 || len(ref) == 0 {
		return Result{}, ErrNoCues
	}
	maxOff := MaxOffsetMs / FrameMs
	var mean float64
	heard := 0
	for _, v := range ref {
		if !math.IsNaN(v) {
			mean += v
			heard++
		}
	}
	if heard == 0 {
		return Result{}, ErrNoCues
	}
	mean /= float64(heard)
	longest := len(ref)
	signals := make([][]float64, len(Rates))
	for i, r := range Rates {
		signals[i] = cueSignal(cues, r)
		longest = max(longest, len(signals[i]))
	}
	n := 1 << bits.Len(uint(longest+maxOff))
	refF := make([]complex128, n)
	for i, v := range ref {
		if !math.IsNaN(v) {
			refF[i] = complex(v-mean, 0)
		}
	}
	fft(refF, false)

	type fit struct {
		rate    float64
		offset  int
		peak, z float64
	}
	var fits []fit
	buf := make([]complex128, n)
	for i, sig := range signals {
		clear(buf)
		count := 0.0
		for t, v := range sig {
			buf[t] = complex(v, 0)
			count += v
		}
		if count == 0 {
			continue
		}
		fft(buf, false)
		for k := range buf {
			buf[k] = refF[k] * cmplx.Conj(buf[k])
		}
		fft(buf, true)
		// corr(k) = Σ ref[t+k]·sig[t]: how much more speech than usual
		// there is under the lines when they're moved k steps later.
		corr := make([]float64, 2*maxOff+1)
		best := 0
		for k := -maxOff; k <= maxOff; k++ {
			v := real(buf[(k+n)%n]) / count
			corr[k+maxOff] = v
			if v > corr[best] {
				best = k + maxOff
			}
		}
		fits = append(fits, fit{rate: Rates[i], offset: best - maxOff, peak: corr[best], z: standout(corr, best, 2000/FrameMs)})
	}
	if len(fits) == 0 {
		return Result{}, ErrNoCues
	}
	pick := fits[0] // Rates[0] is 1
	for _, f := range fits[1:] {
		if f.peak > pick.peak*rateMargin && f.peak > fits[0].peak*rateMargin {
			pick = f
		}
	}
	res := Result{Shift: Shift{OffsetMs: int64(pick.offset) * FrameMs, Rate: pick.rate}, Score: math.Round(pick.z*10) / 10}
	if pick.rate == 1 {
		res.Rate = 0
	}
	res.Sure = res.Score >= MinScore
	return res, nil
}

// standout is how many robust spreads corr[at] is above the typical value,
// leaving out the peak's own neighborhood.
func standout(corr []float64, at, near int) float64 {
	rest := make([]float64, 0, len(corr))
	for i, v := range corr {
		if i < at-near || i > at+near {
			rest = append(rest, v)
		}
	}
	if len(rest) < 10 {
		return 0
	}
	slices.Sort(rest)
	med := rest[len(rest)/2]
	dev := make([]float64, len(rest))
	for i, v := range rest {
		dev[i] = math.Abs(v - med)
	}
	slices.Sort(dev)
	spread := 1.4826 * dev[len(dev)/2] // the median deviation, scaled like a standard deviation
	if spread == 0 {
		return 0
	}
	return (corr[at] - med) / spread
}

// cueSignal is 1 for each step where a spoken line shows once its times are
// multiplied by rate, else 0.
func cueSignal(cues []Cue, rate float64) []float64 {
	var end int64
	for _, c := range cues {
		end = max(end, c.EndMs)
	}
	out := make([]float64, int(float64(end)*rate)/FrameMs+1)
	for _, c := range cues {
		if c.Sound {
			continue
		}
		a := int(float64(c.StartMs) * rate / FrameMs)
		b := min(len(out), int(float64(c.EndMs)*rate/FrameMs))
		for i := max(0, a); i < b; i++ {
			out[i] = 1
		}
	}
	return out
}

// fft transforms a in place; its length must be a power of two. inverse
// runs it backward, scaled so a round trip gives back a.
func fft(a []complex128, inverse bool) {
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
	sign := -1.0
	if inverse {
		sign = 1
	}
	tw := make([]complex128, n/2)
	for k := range tw {
		tw[k] = cmplx.Rect(1, sign*2*math.Pi*float64(k)/float64(n))
	}
	for size := 2; size <= n; size <<= 1 {
		half, stride := size/2, n/size
		for start := 0; start < n; start += size {
			for k := 0; k < half; k++ {
				u, v := a[start+k], a[start+k+half]*tw[k*stride]
				a[start+k], a[start+k+half] = u+v, u-v
			}
		}
	}
	if inverse {
		for i := range a {
			a[i] /= complex(float64(n), 0)
		}
	}
}
