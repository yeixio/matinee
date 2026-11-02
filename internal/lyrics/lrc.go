// Package lyrics finds a song's words, and when they're sung: from a .lrc
// file next to the song, from the song's own tags, or (only when an admin
// turns it on) from LRCLIB, a free and open lyrics database.
package lyrics

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Line is one line of a song's words. Ms is when it's sung, in
// milliseconds from the start; it's nil when the words aren't timed.
type Line struct {
	Ms   *int64 `json:"ms,omitempty"`
	Text string `json:"text"`
}

// Lyrics are a song's words. Synced means every line has its time.
type Lyrics struct {
	Synced bool   `json:"synced"`
	Lines  []Line `json:"lines"`
	// Source says where they came from: file (a .lrc or .txt next to the
	// song), embedded (the song's tags) or online (LRCLIB). Empty when
	// there are none.
	Source string `json:"source,omitempty"`
	// Instrumental means the song has no words at all.
	Instrumental bool `json:"instrumental,omitempty"`
}

// Empty is true when there's nothing to show.
func (l Lyrics) Empty() bool { return len(l.Lines) == 0 }

var (
	// [mm:ss], [mm:ss.xx], [mm:ss:xx] and [h:mm:ss.xx]
	timeTag = regexp.MustCompile(`^\[(\d+):(\d{1,2})(?::(\d{1,2}))?(?:[.:](\d{1,3}))?\]`)
	// [ar:Artist], [offset:+250] and the like.
	metaTag = regexp.MustCompile(`^\[([a-zA-Z#]+):([^\]]*)\]\s*$`)
	// Enhanced LRC times each word: <mm:ss.xx>.
	wordTag = regexp.MustCompile(`<\d+:\d{1,2}(?:[.:]\d{1,3})?>`)
)

// Parse reads LRC text, or plain text when no line is timed. Times are
// moved by the file's [offset:], so they're when the words are sung.
func Parse(text string) Lyrics {
	text = strings.TrimPrefix(text, "\ufeff")
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	var offset int64
	var timed, plain []Line
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if m := metaTag.FindStringSubmatch(line); m != nil {
			if strings.EqualFold(m[1], "offset") {
				if n, err := strconv.ParseInt(strings.TrimSpace(m[2]), 10, 64); err == nil {
					offset = n
				}
			}
			continue
		}
		var times []int64
		for {
			m := timeTag.FindStringSubmatch(line)
			if m == nil {
				break
			}
			times = append(times, tagMs(m))
			line = strings.TrimSpace(line[len(m[0]):])
		}
		words := strings.Join(strings.Fields(wordTag.ReplaceAllString(line, "")), " ")
		if len(times) == 0 {
			plain = append(plain, Line{Text: words})
			continue
		}
		for _, t := range times {
			timed = append(timed, Line{Ms: &t, Text: words})
		}
	}
	if len(timed) > 0 {
		// A positive offset shows the words sooner.
		for i := range timed {
			ms := max(0, *timed[i].Ms-offset)
			timed[i].Ms = &ms
		}
		slices.SortStableFunc(timed, func(a, b Line) int { return int(*a.Ms - *b.Ms) })
		return Lyrics{Synced: true, Lines: trimBlank(timed)}
	}
	return Lyrics{Lines: trimBlank(collapseBlank(plain))}
}

func tagMs(m []string) int64 {
	a, _ := strconv.ParseInt(m[1], 10, 64)
	b, _ := strconv.ParseInt(m[2], 10, 64)
	ms := (a*60 + b) * 1000
	frac := m[4]
	if m[3] != "" {
		if frac == "" {
			// [mm:ss:xx]: the third part is hundredths.
			frac = m[3]
		} else {
			// [h:mm:ss.xx]
			c, _ := strconv.ParseInt(m[3], 10, 64)
			ms = ((a*60+b)*60 + c) * 1000
		}
	}
	if frac != "" {
		n, _ := strconv.ParseInt(frac, 10, 64)
		switch len(frac) {
		case 1:
			n *= 100
		case 2:
			n *= 10
		}
		ms += n
	}
	return ms
}

// trimBlank drops blank lines at the start and end. Blank lines in the
// middle of timed words are pauses, which players show.
func trimBlank(lines []Line) []Line {
	for len(lines) > 0 && lines[0].Text == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && lines[len(lines)-1].Text == "" {
		lines = lines[:len(lines)-1]
	}
	if lines == nil {
		return []Line{}
	}
	return lines
}

// collapseBlank keeps one blank line between verses.
func collapseBlank(lines []Line) []Line {
	var out []Line
	for i, l := range lines {
		if l.Text == "" && i > 0 && lines[i-1].Text == "" {
			continue
		}
		out = append(out, l)
	}
	return out
}
