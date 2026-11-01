package intro

import (
	"math"
	"math/rand/v2"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// theme is a made-up tune: a few voices stepping through notes, like a
// show's theme song.
func theme(seconds float64) []float64 {
	n := int(seconds * sampleRate)
	out := make([]float64, n)
	for i := range out {
		t := float64(i) / sampleRate
		note := func(base, rate float64, steps int) float64 {
			step := math.Mod(math.Floor(t*rate), float64(steps))
			return math.Sin(2 * math.Pi * t * base * math.Pow(2, step/12))
		}
		out[i] = 0.3*note(220, 3, 12) + 0.2*note(330, 5, 7) + 0.1*note(110, 1.5, 5)
	}
	return out
}

// talk stands in for an episode's own soundtrack: noise that swells and
// fades like speech, different for every seed.
func talk(seconds float64, seed uint64) []float64 {
	r := rand.New(rand.NewPCG(seed, seed))
	n := int(seconds * sampleRate)
	out := make([]float64, n)
	rate := 1 + r.Float64()*3
	tone := 200 + r.Float64()*600
	low := 0.0
	for i := range out {
		t := float64(i) / sampleRate
		low = 0.9*low + 0.1*(r.Float64()*2-1)
		out[i] = 0.5*low*math.Abs(math.Sin(t*rate)) + 0.05*math.Sin(2*math.Pi*tone*t)
	}
	return out
}

func concat(parts ...[]float64) []float64 {
	var out []float64
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// Another encoding of the same sound: quieter, with a little hiss.
func reencode(s []float64, gain float64, seed uint64) []float64 {
	r := rand.New(rand.NewPCG(seed, 1))
	out := make([]float64, len(s))
	for i, v := range s {
		out[i] = v*gain + (r.Float64()*2-1)*0.002
	}
	return out
}

func near(t *testing.T, what string, gotMs int64, want time.Duration) {
	t.Helper()
	if d := time.Duration(gotMs)*time.Millisecond - want; d < -time.Second || d > time.Second {
		t.Errorf("%s at %v, want about %v", what, time.Duration(gotMs)*time.Millisecond, want)
	}
}

func TestMatchFindsSharedIntro(t *testing.T) {
	th := theme(40)
	ep1 := FingerprintSamples(concat(talk(70, 1), th, talk(60, 2)))
	ep2 := FingerprintSamples(reencode(concat(talk(15, 3), th, talk(90, 4)), 0.6, 5))
	a, b, ok := Match(ep1, ep2)
	if !ok {
		t.Fatal("no intro found")
	}
	near(t, "intro in 1 starts", a.StartMs(), 70*time.Second)
	near(t, "intro in 1 ends", a.EndMs(), 110*time.Second)
	near(t, "intro in 2 starts", b.StartMs(), 15*time.Second)
	near(t, "intro in 2 ends", b.EndMs(), 55*time.Second)
}

func TestMatchIgnoresUnrelatedAndShortAndSilent(t *testing.T) {
	ep := FingerprintSamples(concat(talk(30, 1), theme(40), talk(60, 2)))
	if _, _, ok := Match(ep, FingerprintSamples(talk(150, 9))); ok {
		t.Error("matched an episode without the intro")
	}
	// A five-second sting is not an intro.
	if _, _, ok := Match(ep, FingerprintSamples(concat(talk(20, 7), theme(5), talk(100, 8)))); ok {
		t.Error("matched a short sting")
	}
	// Nor is silence both episodes start with.
	quiet := make([]float64, 30*sampleRate)
	if _, _, ok := Match(FingerprintSamples(concat(quiet, talk(60, 1))), FingerprintSamples(concat(quiet, talk(60, 2)))); ok {
		t.Error("matched silence")
	}
}

// Files encoded differently, decoded by FFmpeg.
func TestFingerprintFiles(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	dir := t.TempDir()
	tune := "aevalsrc='0.3*sin(2*PI*t*220*pow(2,mod(floor(t*3),12)/12))+0.2*sin(2*PI*t*330*pow(2,mod(floor(t*5),7)/12))':s=44100:d=30"
	speech := func(seed, d string) string {
		return "anoisesrc=color=pink:seed=" + seed + ":d=" + d + ":a=0.4,volume='0.3+0.7*abs(sin(t*2.3))':eval=frame"
	}
	make := func(name string, before, after string, codec ...string) string {
		t.Helper()
		out := filepath.Join(dir, name)
		args := []string{"-v", "error", "-y", "-f", "lavfi", "-i", speech("1"+name[:1], before), "-f", "lavfi", "-i", tune, "-f", "lavfi", "-i", speech("2"+name[:1], after),
			"-filter_complex", "[0]aresample=44100[a];[2]aresample=44100[c];[a][1][c]concat=n=3:v=0:a=1"}
		args = append(append(args, codec...), out)
		if b, err := exec.Command("ffmpeg", args...).CombinedOutput(); err != nil {
			t.Fatalf("ffmpeg: %v\n%s", err, b)
		}
		return out
	}
	f1 := make("1.mkv", "40", "30", "-c:a", "aac", "-b:a", "96k", "-ar", "48000", "-ac", "2")
	f2 := make("2.mp3", "5", "60", "-c:a", "libmp3lame", "-b:a", "128k")
	p1, err := Fingerprint(t.Context(), "ffmpeg", f1, time.Minute, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Only the first minute is read.
	if got := time.Duration(float64(p1.Len())*FrameMs) * time.Millisecond; got < 59*time.Second || got > 60*time.Second {
		t.Errorf("read %v", got)
	}
	p2, err := Fingerprint(t.Context(), "ffmpeg", f2, 2*time.Minute, nil)
	if err != nil {
		t.Fatal(err)
	}
	a, b, ok := Match(p1, p2)
	if !ok {
		t.Fatal("no intro found")
	}
	near(t, "intro in 1 starts", a.StartMs(), 40*time.Second)
	near(t, "intro in 2 starts", b.StartMs(), 5*time.Second)
	// The first file was only read up to a minute, 20 s into the intro.
	near(t, "intro in 2 ends", b.EndMs(), 25*time.Second)
	if _, err := Fingerprint(t.Context(), "ffmpeg", filepath.Join(dir, "missing.mkv"), time.Minute, nil); err == nil {
		t.Error("no error for a missing file")
	}
}
