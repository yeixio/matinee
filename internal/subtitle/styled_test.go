package subtitle

import (
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const sampleASS = `[Script Info]
ScriptType: v4.00+
PlayResX: 640
PlayResY: 360

[V4+ Styles]
Format: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding
Style: Sign,Gandhi Sans,40,&H0000FFFF,&H000000FF,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,2,0,8,10,10,10,1

[Events]
Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text
Dialogue: 0,0:00:00.00,0:00:01.50,Sign,,0,0,0,,{\pos(320,40)}The station
`

// styledVideo makes an MKV with an ASS track and an attached font.
func styledVideo(t *testing.T) string {
	t.Helper()
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe not installed")
	}
	dir := t.TempDir()
	ass := filepath.Join(dir, "signs.ass")
	font := filepath.Join(dir, "Gandhi Sans.ttf")
	os.WriteFile(ass, []byte(sampleASS), 0o644)
	os.WriteFile(font, []byte("not really a font, but a file named like one"), 0o644)
	out := filepath.Join(dir, "Show.mkv")
	cmd := exec.Command(ffmpeg, "-v", "error", "-f", "lavfi", "-i", "color=c=black:s=320x180:d=2", "-i", ass,
		"-attach", font, "-metadata:s:t", "mimetype=application/x-truetype-font",
		"-map", "0:v", "-map", "1:s", "-c:v", "libx264", "-c:s", "ass", out)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("make video: %v: %s", err, b)
	}
	return out
}

func TestStyledTrackAndFonts(t *testing.T) {
	video := styledVideo(t)
	c := NewConverter("ffmpeg", t.TempDir()).WithFFprobe("ffprobe")

	tr := Track{ID: "e0", Stream: 0, Codec: "ass", Styled: true}

	rec := httptest.NewRecorder()
	c.ServeASS(rec, httptest.NewRequest("GET", "/", nil), video, tr)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `{\pos(320,40)}The station`) || !strings.Contains(rec.Body.String(), "Gandhi Sans") {
		t.Fatalf("ass: %d %q", rec.Code, rec.Body.String())
	}

	fonts, err := c.Fonts(t.Context(), video)
	if err != nil || len(fonts) != 1 || fonts[0].Name != "Gandhi Sans.ttf" || fonts[0].Index != 0 {
		t.Fatalf("fonts: %v %+v", err, fonts)
	}
	rec = httptest.NewRecorder()
	c.ServeFont(rec, httptest.NewRequest("GET", "/", nil), video, fonts[0])
	if rec.Code != 200 || rec.Body.String() != "not really a font, but a file named like one" {
		t.Fatalf("font: %d %q", rec.Code, rec.Body.String())
	}
	// A track that isn't ASS has nothing to give.
	rec = httptest.NewRecorder()
	c.ServeASS(rec, httptest.NewRequest("GET", "/", nil), video, Track{ID: "e1", Codec: "subrip"})
	if rec.Code != 422 {
		t.Errorf("subrip as ass: %d", rec.Code)
	}
}

func TestStyledSidecar(t *testing.T) {
	dir := t.TempDir()
	video := filepath.Join(dir, "Movie.mkv")
	os.WriteFile(video, nil, 0o644)
	os.WriteFile(filepath.Join(dir, "Movie.en.ass"), []byte("\xef\xbb\xbf"+sampleASS), 0o644)
	os.WriteFile(filepath.Join(dir, "Movie.en.srt"), []byte("1\n00:00:00,000 --> 00:00:01,000\nHi\n"), 0o644)
	var ass Track
	for _, tr := range Sidecars(video) {
		if tr.Codec == "ass" {
			ass = tr
		} else if tr.Styled {
			t.Errorf("%s is styled", tr.Codec)
		}
	}
	if !ass.Styled {
		t.Fatalf("sidecar ass not styled: %+v", ass)
	}
	c := NewConverter("ffmpeg", t.TempDir())
	rec := httptest.NewRecorder()
	c.ServeASS(rec, httptest.NewRequest("GET", "/", nil), video, ass)
	if rec.Code != 200 || !strings.HasPrefix(rec.Body.String(), "[Script Info]") {
		t.Fatalf("sidecar: %d %q", rec.Code, rec.Body.String()[:min(40, rec.Body.Len())])
	}
}

func TestFontNamesAreOnlyNames(t *testing.T) {
	fonts, err := parseFonts([]byte(`{"streams":[
		{"index":0,"codec_type":"video"},
		{"index":3,"codec_type":"attachment","tags":{"filename":"../../etc/evil.ttf","mimetype":"font/ttf"}},
		{"index":4,"codec_type":"attachment","tags":{"filename":"cover.jpg","mimetype":"image/jpeg"}},
		{"index":5,"codec_type":"attachment","tags":{"FILENAME":"C:\\fonts\\Arial.OTF"}}]}`))
	if err != nil || len(fonts) != 2 || fonts[0].Name != "evil.ttf" || fonts[0].stream != 3 || fonts[1].Name != "Arial.OTF" || fonts[1].Index != 1 {
		t.Fatalf("%v %+v", err, fonts)
	}
}
