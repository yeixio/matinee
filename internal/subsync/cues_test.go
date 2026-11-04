package subsync

import (
	"strings"
	"testing"
)

func TestParseVTT(t *testing.T) {
	cues := ParseVTT([]byte("WEBVTT\n\n1\n00:00:01.500 --> 00:00:03.000 line:90%\n<i>Hello</i> there\n\n00:04.000 --> 00:05.250\n[door slams]\n\n00:00:06,000 --> 00:00:07,000\n♪ La la la ♪\n\n00:00:08.000 --> 00:00:09.000\n- [laughs] No way.\n"))
	want := []Cue{{1500, 3000, false}, {4000, 5250, true}, {6000, 7000, true}, {8000, 9000, false}}
	if len(cues) != len(want) {
		t.Fatalf("got %+v", cues)
	}
	for i := range want {
		if cues[i] != want[i] {
			t.Errorf("cue %d: got %+v, want %+v", i, cues[i], want[i])
		}
	}
}

func TestShiftVTT(t *testing.T) {
	in := "WEBVTT\r\n\r\n00:00:01.000 --> 00:00:02.500 align:start\r\nHi\r\n\r\n01:00:00.000 --> 01:00:01.000\r\nLater\r\n"
	out := string(ShiftVTT([]byte(in), Shift{OffsetMs: -1500}))
	for _, w := range []string{"00:00:00.000 --> 00:00:01.000 align:start\r\n", "00:59:58.500 --> 00:59:59.500\r\n"} {
		if !strings.Contains(out, w) {
			t.Errorf("missing %q in %q", w, out)
		}
	}
	out = string(ShiftVTT([]byte(in), Shift{Rate: 25 / 23.976}))
	if !strings.Contains(out, "01:02:33.754 --> ") {
		t.Errorf("rate: %q", out)
	}
	if string(ShiftVTT([]byte(in), Shift{})) != in {
		t.Error("a zero shift changed the file")
	}
}

func TestShiftASS(t *testing.T) {
	in := "[Events]\nFormat: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text\nDialogue: 0,0:00:01.00,0:00:02.50,Default,,0,0,0,,Hello, world\nComment: 0,0:00:01.00,0:00:02.50,Default,,0,0,0,,note\n"
	out := string(ShiftASS([]byte(in), Shift{OffsetMs: 2340}))
	if !strings.Contains(out, "Dialogue: 0,0:00:03.34,0:00:04.84,Default,,0,0,0,,Hello, world\n") {
		t.Errorf("got %q", out)
	}
	if !strings.Contains(out, "Comment: 0,0:00:01.00") {
		t.Error("comments should stay")
	}
}
