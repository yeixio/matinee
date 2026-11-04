package subsync

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var lines = []string{
	"Where were you last night?", "I told you, I was at the station until late.",
	"That is not what your brother said.", "My brother says a lot of things.",
	"We need to leave before the storm comes in.", "Get the car. I will meet you outside.",
	"Do you remember the house by the lake?", "Of course I do. We spent every summer there.",
	"Nobody has lived there for years.", "Then who keeps turning the lights on?",
	"Hand me the map, quickly.", "This road does not go anywhere.",
	"Listen to me. You have to trust me now.", "I trusted you once, and look where that got us.",
	"The train leaves at nine.", "Then we have an hour. Sit down and eat something.",
}

// speechClip has FFmpeg's built-in voice say a line, as 8 kHz samples.
func speechClip(t *testing.T, ffmpeg, text string) []float64 {
	t.Helper()
	out, err := exec.Command(ffmpeg, "-v", "error", "-f", "lavfi", "-i", "flite=text='"+strings.ReplaceAll(text, "'", "")+"'",
		"-ac", "1", "-ar", "8000", "-f", "s16le", "-").Output()
	if err != nil {
		t.Skipf("no flite voice in this FFmpeg: %v", err)
	}
	s := make([]float64, len(out)/2)
	for i := range s {
		s[i] = float64(int16(binary.LittleEndian.Uint16(out[2*i:]))) / 32768
	}
	return s
}

// movie makes a soundtrack of the given length with music and noise under
// it and lines spoken at random moments, and returns the file and where
// each line is spoken.
func movie(t *testing.T, ffmpeg string, seconds int, seed uint64) (string, []Cue) {
	t.Helper()
	rng := rand.New(rand.NewPCG(seed, 1))
	clips := map[string][]float64{}
	samples := make([]float64, seconds*sampleRate)
	// Music: slow chords that change every few seconds, louder in places.
	freqs := []float64{110, 138.6, 164.8, 220, 261.6, 329.6}
	for i := range samples {
		sec := float64(i) / sampleRate
		chord := int(sec/3.7) % len(freqs)
		loud := 0.03 + 0.05*(0.5+0.5*math.Sin(sec/23))
		samples[i] = loud*(math.Sin(2*math.Pi*freqs[chord]*sec)+0.6*math.Sin(2*math.Pi*freqs[(chord+2)%len(freqs)]*sec)) + 0.01*rng.NormFloat64()
	}
	var cues []Cue
	at := 2.0
	for at < float64(seconds-8) {
		text := lines[rng.IntN(len(lines))]
		c, ok := clips[text]
		if !ok {
			c = speechClip(t, ffmpeg, text)
			clips[text] = c
		}
		start := int(at * sampleRate)
		for i, v := range c {
			if start+i < len(samples) {
				samples[start+i] += 0.5 * v
			}
		}
		cues = append(cues, Cue{StartMs: int64(at * 1000), EndMs: int64(at*1000) + int64(len(c))*1000/sampleRate})
		at += float64(len(c))/sampleRate + 0.5 + rng.Float64()*8
		if rng.IntN(6) == 0 {
			at += 20 + rng.Float64()*30 // a stretch with only music
		}
	}
	var b bytes.Buffer
	b.WriteString("RIFF")
	binary.Write(&b, binary.LittleEndian, uint32(36+2*len(samples)))
	b.WriteString("WAVEfmt ")
	for _, v := range []any{uint32(16), uint16(1), uint16(1), uint32(sampleRate), uint32(2 * sampleRate), uint16(2), uint16(16)} {
		binary.Write(&b, binary.LittleEndian, v)
	}
	b.WriteString("data")
	binary.Write(&b, binary.LittleEndian, uint32(2*len(samples)))
	for _, v := range samples {
		binary.Write(&b, binary.LittleEndian, int16(max(-32768, min(32767, v*32767))))
	}
	path := filepath.Join(t.TempDir(), fmt.Sprintf("movie%d.wav", seed))
	if err := os.WriteFile(path, b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path, cues
}

// moved is how a subtitle file for another release has the lines: the
// opposite of the shift that fixes it.
func moved(cues []Cue, rate float64, offsetMs int64) []Cue {
	out := make([]Cue, len(cues))
	for i, c := range cues {
		out[i] = Cue{StartMs: max(0, int64(float64(c.StartMs-offsetMs)/rate)), EndMs: max(0, int64(float64(c.EndMs-offsetMs)/rate))}
	}
	return out
}

func TestAlignToSpeech(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	path, cues := movie(t, ffmpeg, 20*60, 7)
	sp, err := Listen(context.Background(), ffmpeg, path, Audio{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	ref := SpeechReference(sp)
	for _, tc := range []struct {
		name     string
		rate     float64
		offsetMs int64
	}{
		{"in sync", 1, 0},
		{"late", 1, 2400},
		{"early", 1, -4700},
		{"a minute off", 1, 61000},
		{"25 fps subtitles", 25 / 23.976, 0},
		{"23.976 fps subtitles on 25", 23.976 / 25, 1500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := Align(ref, moved(cues, tc.rate, tc.offsetMs))
			if err != nil {
				t.Fatal(err)
			}
			rate := res.Rate
			if rate == 0 {
				rate = 1
			}
			t.Logf("got %+v", res)
			if !res.Sure || math.Abs(rate-tc.rate) > 1e-6 || abs(res.OffsetMs-tc.offsetMs) > 200 {
				t.Errorf("got %+v, want rate %v offset %d", res, tc.rate, tc.offsetMs)
			}
		})
	}
	// Listening to parts of the film is enough.
	part, err := Listen(context.Background(), ffmpeg, path, Audio{}, []Window{{60000, 240000}, {500000, 240000}, {900000, 240000}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := Align(SpeechReference(part), moved(cues, 25/23.976, -3000))
	t.Logf("parts: %+v", res)
	if err != nil || !res.Sure || math.Abs(res.Rate-25/23.976) > 1e-6 || abs(res.OffsetMs+3000) > 200 {
		t.Errorf("from parts: got %+v, %v", res, err)
	}
	// Subtitles for another film fit nowhere.
	_, other := movie(t, ffmpeg, 20*60, 99)
	res, err = Align(ref, other)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("other film: %+v", res)
	if res.Sure {
		t.Errorf("another film's subtitles fit: %+v", res)
	}
}

func abs(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}
