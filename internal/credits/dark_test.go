package credits

import (
	"bytes"
	"testing"
	"time"
)

// frames makes one frame every two seconds from 0 to end, dark where dark
// says so.
func frames(end time.Duration, dark func(at time.Duration) bool) []Frame {
	var out []Frame
	for at := time.Duration(0); at < end; at += 2 * time.Second {
		b := 20
		if dark(at) {
			b = 95
		}
		out = append(out, Frame{At: at, Black: b})
	}
	return out
}

func between(from, to time.Duration) func(time.Duration) bool {
	return func(at time.Duration) bool { return at >= from && at < to }
}

func TestFindDark(t *testing.T) {
	const m = time.Minute
	s := time.Second
	tests := []struct {
		name        string
		dark        func(time.Duration) bool
		start, stop time.Duration
		ok          bool
	}{
		{"credits to the end", between(10*m, 15*m), 10 * m, 0, true},
		{"nothing dark", func(time.Duration) bool { return false }, 0, 0, false},
		{"a short dark scene", between(13*m, 13*m+20*s), 0, 0, false},
		{"a dark scene long before the end", between(3*m, 4*m), 0, 0, false},
		{"a scene after the credits", between(9*m, 14*m), 9 * m, 14 * m, true},
		{"a logo halfway", func(at time.Duration) bool {
			return at >= 10*m && !(at >= 12*m && at < 12*m+4*s)
		}, 10 * m, 0, true},
		{"a scene halfway through", func(at time.Duration) bool {
			return between(8*m, 10*m)(at) || between(11*m, 15*m)(at)
		}, 8 * m, 0, true},
		{"a short dark scene just before", func(at time.Duration) bool {
			return between(9*m, 9*m+40*s)(at) || between(10*m, 15*m)(at)
		}, 10 * m, 0, true},
		{"black padding at the very end", between(14*m+10*s, 15*m), 14*m + 10*s, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, stop, ok := FindDark(frames(15*m, tt.dark), 15*m)
			if ok != tt.ok || start != tt.start || stop != tt.stop {
				t.Fatalf("got %v %v %v, want %v %v %v", start, stop, ok, tt.start, tt.stop, tt.ok)
			}
		})
	}
}

func TestParseBlackframe(t *testing.T) {
	out := bytes.NewBufferString(`Input #0, matroska,webm, from 'x.mkv':
[Parsed_blackframe_1 @ 0x55c241a38a00] frame:0 pblack:0 pts:0 t:0.000000 type:I last_keyframe:0
[Parsed_blackframe_1 @ 0x55c241a38a00] frame:1 pblack:97 pts:2000 t:2.500000 type:I last_keyframe:1
[Parsed_blackframe_1 @ 0x55c241a38a00] frame:2 pblack:97 pts:2000 t:2.500000 type:I last_keyframe:1
`)
	got := parseBlackframe(out, time.Minute)
	want := []Frame{{time.Minute, 0}, {time.Minute + 2500*time.Millisecond, 97}}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("got %+v", got)
	}
}
