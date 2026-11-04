package subsync

import (
	"bytes"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// Cue is one subtitle line's time on screen, in milliseconds.
type Cue struct {
	StartMs, EndMs int64
	// Sound marks lines that describe sounds or music, such as "[door
	// slams]" or "♪ ♪", which nobody says.
	Sound bool
}

// Shift moves subtitle times: each time t becomes t×Rate + OffsetMs. A
// Rate of 0 means 1.
type Shift struct {
	OffsetMs int64   `json:"offsetMs"`
	Rate     float64 `json:"rate,omitempty"`
}

// IsZero says whether the shift leaves times as they are.
func (s Shift) IsZero() bool { return s.OffsetMs == 0 && (s.Rate == 0 || s.Rate == 1) }

// Apply shifts one time, never before the start.
func (s Shift) Apply(ms int64) int64 {
	r := s.Rate
	if r == 0 {
		r = 1
	}
	return max(0, int64(math.Round(float64(ms)*r))+s.OffsetMs)
}

var vttTiming = regexp.MustCompile(`^\s*((?:\d+:)?\d{1,2}:\d{2}[.,]\d{1,3})\s+-->\s+((?:\d+:)?\d{1,2}:\d{2}[.,]\d{1,3})(.*)$`)

// ParseVTT reads the cues of a WebVTT (or SubRip) file.
func ParseVTT(data []byte) []Cue {
	var out []Cue
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	for i := 0; i < len(lines); i++ {
		m := vttTiming.FindStringSubmatch(lines[i])
		if m == nil {
			continue
		}
		start, ok1 := parseClock(m[1])
		end, ok2 := parseClock(m[2])
		if !ok1 || !ok2 || end <= start {
			continue
		}
		var text []string
		for i+1 < len(lines) && strings.TrimSpace(lines[i+1]) != "" {
			i++
			text = append(text, lines[i])
		}
		out = append(out, Cue{StartMs: start, EndMs: end, Sound: isSound(strings.Join(text, " "))})
	}
	return out
}

// parseClock reads "01:02:03.456", "02:03.456" or SubRip's "01:02:03,456".
func parseClock(s string) (int64, bool) {
	s = strings.Replace(s, ",", ".", 1)
	main, frac, _ := strings.Cut(s, ".")
	var ms int64
	for _, p := range strings.Split(main, ":") {
		n, err := strconv.ParseInt(p, 10, 64)
		if err != nil {
			return 0, false
		}
		ms = ms*60 + n
	}
	ms *= 1000
	for len(frac) < 3 {
		frac += "0"
	}
	f, err := strconv.ParseInt(frac[:3], 10, 64)
	if err != nil {
		return 0, false
	}
	return ms + f, true
}

var tags = regexp.MustCompile(`<[^>]*>|\{[^}]*\}`)

// isSound says whether a line only describes sounds: everything in it is
// in brackets or between music notes.
func isSound(text string) bool {
	t := tags.ReplaceAllString(text, "")
	if strings.ContainsAny(t, "♪♫") {
		return true
	}
	depth := 0
	for _, r := range t {
		switch r {
		case '[', '(':
			depth++
		case ']', ')':
			depth = max(0, depth-1)
		default:
			if depth == 0 && (unicode.IsLetter(r) || unicode.IsDigit(r)) {
				return false
			}
		}
	}
	return true
}

// ShiftVTT rewrites a WebVTT file's cue times.
func ShiftVTT(data []byte, s Shift) []byte {
	if s.IsZero() {
		return data
	}
	lines := bytes.Split(data, []byte("\n"))
	for i, line := range lines {
		m := vttTiming.FindSubmatch(bytes.TrimRight(line, "\r"))
		if m == nil {
			continue
		}
		start, ok1 := parseClock(string(m[1]))
		end, ok2 := parseClock(string(m[2]))
		if !ok1 || !ok2 {
			continue
		}
		cr := ""
		if bytes.HasSuffix(line, []byte("\r")) {
			cr = "\r"
		}
		lines[i] = fmt.Appendf(nil, "%s --> %s%s%s", vttClock(s.Apply(start)), vttClock(s.Apply(end)), m[3], cr)
	}
	return bytes.Join(lines, []byte("\n"))
}

func vttClock(ms int64) string {
	return fmt.Sprintf("%02d:%02d:%02d.%03d", ms/3600000, ms/60000%60, ms/1000%60, ms%1000)
}

// ShiftASS rewrites the times of an ASS/SSA file's dialogue lines, which
// look like "Dialogue: 0,0:01:02.34,0:01:04.56,Default,…".
func ShiftASS(data []byte, s Shift) []byte {
	if s.IsZero() {
		return data
	}
	lines := bytes.Split(data, []byte("\n"))
	for i, line := range lines {
		head, rest, ok := bytes.Cut(line, []byte(":"))
		if !ok || !bytes.EqualFold(bytes.TrimSpace(head), []byte("Dialogue")) {
			continue
		}
		f := bytes.SplitN(rest, []byte(","), 4)
		if len(f) < 4 {
			continue
		}
		start, ok1 := parseClock(string(bytes.TrimSpace(f[1])))
		end, ok2 := parseClock(string(bytes.TrimSpace(f[2])))
		if !ok1 || !ok2 {
			continue
		}
		f[1], f[2] = []byte(assClock(s.Apply(start))), []byte(assClock(s.Apply(end)))
		lines[i] = append(append(head, ':'), bytes.Join(f, []byte(","))...)
	}
	return bytes.Join(lines, []byte("\n"))
}

func assClock(ms int64) string {
	cs := (ms + 5) / 10
	return fmt.Sprintf("%d:%02d:%02d.%02d", cs/360000, cs/6000%60, cs/100%60, cs%100)
}
