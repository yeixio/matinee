package stream

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// A filled square drawn with ASS drawing commands, which need no font, on
// screen from 1 s to 2 s.
const squareASS = `[Script Info]
ScriptType: v4.00+
PlayResX: 160
PlayResY: 90

[V4+ Styles]
Format: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding
Style: Box,Arial,20,&H00FFFFFF,&H000000FF,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,0,0,7,0,0,0,1

[Events]
Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text
Dialogue: 0,0:00:01.00,0:00:02.00,Box,,0,0,0,,{\pos(0,0)\p1}m 0 0 l 160 0 160 90 0 90{\p0}
`

func TestStyledFilterDrawsAtTheRightTime(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	// A folder name with everything the filter graph treats as special.
	dir := filepath.Join(t.TempDir(), `Bob's [Show], part 1; a:b`)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	ass := filepath.Join(dir, "box.ass")
	os.WriteFile(ass, []byte(squareASS), 0o644)
	video := filepath.Join(dir, "Episode.mkv")
	if b, err := exec.Command(ffmpeg, "-v", "error", "-f", "lavfi", "-i", "color=c=black:s=160x90:r=10:d=3", "-i", ass,
		"-map", "0:v", "-map", "1:s", "-c:v", "libx264", "-c:s", "ass", video).CombinedOutput(); err != nil {
		t.Fatalf("make video: %v: %s", err, b)
	}
	frame := func(path string, stream int, start float64) []byte {
		t.Helper()
		out, err := exec.Command(ffmpeg, "-v", "error", "-ss", "1.2", "-i", video, "-vf", StyledFilter(path, stream, start)+",format=gray",
			"-frames:v", "1", "-f", "rawvideo", "-").Output()
		if err != nil {
			t.Fatalf("draw: %v", err)
		}
		return out
	}
	lit := func(px []byte) bool {
		return len(px) > 0 && bytes.Count(px, []byte{0x10}) < len(px)/2 && px[len(px)/2] > 128
	}

	// 1.2 s into the file the square shows, embedded or from the sidecar.
	if px := frame(video, 0, 1.2); !lit(px) {
		t.Error("embedded track: no square at 1.2 s")
	}
	if px := frame(ass, -1, 1.2); !lit(px) {
		t.Error("sidecar: no square at 1.2 s")
	}
	// Without moving the clock, libass would think it's 0 s and draw nothing.
	if px := frame(video, 0, 0); lit(px) {
		t.Error("the clock isn't what makes the difference")
	}
}
