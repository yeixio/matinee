package intro

import "time"

// Tuning, from trying the matcher on episodes that share an intro and on
// ones that don't (see match_test.go). Single values are too noisy to
// judge: the same sound after two codecs differs in about 7 of 32 bits,
// unrelated sounds in about 16 but now and then in only 8. So the matcher
// averages the difference over a couple of seconds.
const (
	// window is how much audio each judgement averages over.
	window = 2 * time.Second
	// sameBits: a stretch whose values differ in at most this many bits on
	// average is the same sound in both.
	sameBits = 12
	// quietBits stands in for a frame that is silent in either: silence
	// sounds the same everywhere, so it never counts as shared.
	quietBits = 16
)

// MinIntro and MaxIntro bound what counts as an intro. A shorter shared
// passage is a channel ident or a sound effect; a longer one is a recap or
// the same file twice.
var (
	MinIntro = 15 * time.Second
	MaxIntro = 200 * time.Second
)

// Segment is a stretch of a print, in frames: [Start, End).
type Segment struct{ Start, End int }

func (s Segment) Len() int { return s.End - s.Start }

// StartMs and EndMs turn a segment's frames into time.
func (s Segment) StartMs() int64 { return int64(float64(s.Start) * FrameMs) }
func (s Segment) EndMs() int64   { return int64(float64(s.End) * FrameMs) }

func frames(d time.Duration) int { return int(float64(d.Milliseconds()) / FrameMs) }

// Match finds the longest passage two prints share, wherever it is in each,
// and where it is in a and in b. ok is false when they share nothing
// between MinIntro and MaxIntro long.
//
// It tries every way of lining the two up. That is a couple of hundred
// million cheap comparisons for ten minutes of each, under a second.
func Match(a, b Print) (inA, inB Segment, ok bool) {
	na, nb := a.Len(), b.Len()
	minLen, maxLen, w := frames(MinIntro), frames(MaxIntro), frames(window)
	limit := sameBits * w // a window's total at most this is a hit
	best, bestD := Segment{}, 0
	dist := make([]int, 0, min(na, nb))
	for d := -(na - 1); d < nb; d++ { // b index = a index + d
		i0, i1 := max(0, -d), min(na, nb-d)
		if i1-i0 < minLen {
			continue
		}
		dist = dist[:0]
		for i := i0; i < i1; i++ {
			j := i + d
			if a.Quiet[i] || b.Quiet[j] {
				dist = append(dist, quietBits)
			} else {
				dist = append(dist, distance(a.Values[i], b.Values[j]))
			}
		}
		// Slide the window along; a run is the windows in a row whose
		// centres are a hit, and covers from the first centre to the last.
		sum := 0
		for k := 0; k < w && k < len(dist); k++ {
			sum += dist[k]
		}
		run := -1
		closeRun := func(end int) { // end: one past the run's last centre
			if run < 0 {
				return
			}
			if n := end - run; n > best.Len() && n <= maxLen {
				best, bestD = Segment{i0 + run, i0 + end}, d
			}
			run = -1
		}
		for k := 0; k+w <= len(dist); k++ {
			if k > 0 {
				sum += dist[k+w-1] - dist[k-1]
			}
			centre := k + w/2
			if sum <= limit {
				if run < 0 {
					run = centre
					if k == 0 {
						run = 0 // shared from the very start
					}
				}
			} else {
				closeRun(centre)
			}
		}
		closeRun(len(dist)) // still shared at the very end
	}
	if best.Len() < minLen {
		return Segment{}, Segment{}, false
	}
	return best, Segment{best.Start + bestD, best.End + bestD}, true
}
