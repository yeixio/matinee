package spectrum

import (
	"bytes"
	"context"
	"encoding/binary"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// tone is seconds of a sine at hz, as the samples FFmpeg would send.
func tone(hz float64, seconds float64, amp float64) []byte {
	n := int(seconds * rate)
	b := make([]byte, 2*n)
	for i := range n {
		s := amp * math.Sin(2*math.Pi*hz*float64(i)/rate)
		binary.LittleEndian.PutUint16(b[2*i:], uint16(int16(s*32767)))
	}
	return b
}

func band(hz float64) int {
	e := bandEdges()
	bin := int(math.Round(hz / (float64(rate) / window)))
	for b := range Bands {
		if bin >= e[b] && bin < e[b+1] {
			return b
		}
	}
	return -1
}

func TestATonePeaksInItsBand(t *testing.T) {
	levels, err := Analyze(bytes.NewReader(tone(440, 2, 0.5)))
	if err != nil {
		t.Fatal(err)
	}
	if got := len(levels) / Bands; got != 2*FPS {
		t.Fatalf("got %d moments for 2 seconds, want %d", got, 2*FPS)
	}
	want := band(440)
	mid := levels[FPS*Bands : FPS*Bands+Bands]
	for b, v := range mid {
		if b == want && v < 200 {
			t.Errorf("440 Hz band %d is %d, want loud", b, v)
		}
		if (b < want-2 || b > want+2) && v > 60 {
			t.Errorf("band %d is %d for a 440 Hz tone, want quiet", b, v)
		}
	}
}

func TestSilenceThenSound(t *testing.T) {
	in := append(make([]byte, 2*rate), tone(100, 1, 0.8)...) // a second of silence, then a second of tone
	levels, err := Analyze(bytes.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	b := band(100)
	if quiet, loud := levels[FPS/2*Bands+b], levels[(FPS+FPS/2)*Bands+b]; quiet > 10 || loud < 200 {
		t.Errorf("100 Hz band: %d in the silence, %d in the tone", quiet, loud)
	}
}

func TestBandEdgesRiseAndFit(t *testing.T) {
	e := bandEdges()
	for b := 1; b <= Bands; b++ {
		if e[b] <= e[b-1] {
			t.Fatalf("edges don't rise: %v", e)
		}
	}
	if e[Bands] > window/2 {
		t.Fatalf("last edge %d past the spectrum", e[Bands])
	}
}

func TestLevelsFromAFileAreKept(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("no ffmpeg")
	}
	dir := t.TempDir()
	song := filepath.Join(dir, "song.wav")
	if out, err := exec.Command(ffmpeg, "-v", "error", "-f", "lavfi", "-i", "sine=frequency=1000:duration=3", song).CombinedOutput(); err != nil {
		t.Fatalf("making a song: %v %s", err, out)
	}
	a := New(ffmpeg, filepath.Join(dir, "levels"))
	levels, err := a.Levels(context.Background(), song)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(levels) / Bands; n < 3*FPS-1 || n > 3*FPS+1 {
		t.Fatalf("got %d moments for 3 seconds", n)
	}
	if v := levels[FPS*Bands+band(1000)]; v < 200 {
		t.Errorf("1 kHz band is %d", v)
	}
	kept, _ := os.ReadDir(filepath.Join(dir, "levels"))
	if len(kept) != 1 {
		t.Fatalf("kept %d files, want 1", len(kept))
	}
	// The second time comes from what was kept, even without FFmpeg.
	a.FFmpeg = "/nonexistent"
	again, err := a.Levels(context.Background(), song)
	if err != nil || !bytes.Equal(again, levels) {
		t.Fatalf("second time: %v, same %v", err, bytes.Equal(again, levels))
	}
}
