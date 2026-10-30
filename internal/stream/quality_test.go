package stream

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yeixio/matinee/internal/store"
)

func ids(steps []Step) string {
	var out []string
	for _, s := range steps {
		out = append(out, s.ID)
	}
	return strings.Join(out, ",")
}

func intp(n int) *int { return &n }

func TestLadder(t *testing.T) {
	tests := []struct {
		name   string
		width  *int
		onCPU  bool
		offers []Step
		want   string
	}{
		{"4K on a GPU", intp(3840), false, Steps, "1080p,720p,480p,360p"},
		{"wide film counts as 1080p", intp(1920), false, Steps, "1080p,720p,480p,360p"},
		{"720p file", intp(1280), false, Steps, "720p,480p,360p"},
		{"between steps keeps the one above", intp(1000), false, Steps, "720p,480p,360p"},
		{"tiny file gets the smallest", intp(320), false, Steps, "360p"},
		{"unknown size gets everything", nil, false, Steps, "1080p,720p,480p,360p"},
		{"CPU keeps the top and the middle", intp(3840), true, Steps, "1080p,480p"},
		{"CPU with three", intp(1280), true, Steps, "720p,480p"},
		{"CPU with two", intp(1280), true, Steps[1:3], "720p,480p"},
		{"only what's offered", intp(3840), false, []Step{Steps[0], Steps[2]}, "1080p,480p"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ids(Ladder(store.Item{Width: tt.width}, tt.offers, tt.onCPU))
			if got != tt.want {
				t.Errorf("got %s, want %s", got, tt.want)
			}
		})
	}
}

func TestMasterPlaylist(t *testing.T) {
	it := store.Item{Width: intp(1920), Height: intp(800)}
	got := string(masterPlaylist(it, Steps[:2], true))
	want := "#EXTM3U\n#EXT-X-VERSION:3\n" +
		"#EXT-X-STREAM-INF:BANDWIDTH=8192000,CODECS=\"avc1.640028,mp4a.40.2\",RESOLUTION=1920x800,NAME=\"1080p\"\nindex_0.m3u8\n" +
		"#EXT-X-STREAM-INF:BANDWIDTH=4128000,CODECS=\"avc1.640028,mp4a.40.2\",RESOLUTION=1280x534,NAME=\"720p\"\nindex_1.m3u8\n"
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
	if got := string(masterPlaylist(store.Item{}, Steps[3:], false)); !strings.Contains(got, "BANDWIDTH=700000,CODECS=\"avc1.640028\",NAME") {
		t.Errorf("without audio or size: %s", got)
	}
}

func TestFits(t *testing.T) {
	ms := int64(60_000)
	// 30 MB a minute is 4 Mbps.
	it := store.Item{VideoCodec: "h264", Width: intp(1280), DurationMs: &ms, Size: 30_000_000}
	if !fits(it, Steps[1]) {
		t.Error("4 Mbps 720p should fit 720p")
	}
	if fits(it, Steps[2]) {
		t.Error("720p shouldn't fit 480p")
	}
	it.VideoCodec = "hevc"
	if fits(it, Steps[1]) {
		t.Error("HEVC can't be copied")
	}
}

// An adaptive transcode makes a master playlist and a playlist with
// segments for each step, all served to the viewer who started it.
func TestStartAdaptive(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "clip.mkv")
	gen := exec.Command(ffmpeg, "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "testsrc=size=1280x720:rate=24:duration=10",
		"-f", "lavfi", "-i", "sine=duration=10", "-c:v", "mpeg4", "-c:a", "mp2", src)
	if out, err := gen.CombinedOutput(); err != nil {
		t.Fatalf("make clip: %v\n%s", err, out)
	}
	var logs strings.Builder
	tr := NewTranscoder(ffmpeg, filepath.Join(dir, "cache"), nil, slog.New(slog.NewTextHandler(&logs, nil)))
	defer tr.Close()
	it := store.Item{ID: 1, Path: src, VideoCodec: "mpeg4", AudioCodec: "mp2", Width: intp(1280), Height: intp(720)}

	id, err := tr.Start(context.Background(), 7, it, 0, -1, 0, Steps[1:]...)
	if err != nil {
		t.Fatalf("start: %v\n%s", err, logs.String())
	}
	if Playlist(Steps[1:]) != "master.m3u8" {
		t.Fatal("an adaptive stream starts from master.m3u8")
	}
	// On the CPU: 720p and 480p.
	get := func(name string) (int, string) {
		w := httptest.NewRecorder()
		tr.ServeFile(w, httptest.NewRequest("GET", "/hls/"+id+"/"+name+"?token=abc", nil), 7, id, name)
		return w.Code, w.Body.String()
	}
	code, master := get("master.m3u8")
	if code != 200 || strings.Count(master, "#EXT-X-STREAM-INF") != 2 || !strings.Contains(master, "index_1.m3u8?token=abc") {
		t.Fatalf("master: %d\n%s", code, master)
	}
	for _, v := range []string{"index_0.m3u8", "index_1.m3u8"} {
		code, pl := get(v)
		if code != 200 || !strings.Contains(pl, "seg_") || !strings.Contains(pl, "?token=abc") {
			t.Fatalf("%s: %d\n%s", v, code, pl)
		}
	}
	if code, _ := get("seg_1_00000.ts"); code != 200 {
		t.Fatalf("segment: %d", code)
	}
	// Each step is its own size, with its own audio.
	probe := func(seg string) string {
		out, err := exec.Command(strings.TrimSuffix(ffmpeg, "ffmpeg")+"ffprobe", "-v", "error", "-show_entries", "stream=codec_type,width,height", "-of", "csv=p=0",
			filepath.Join(tr.Dir, id, seg)).Output()
		if err != nil {
			t.Skipf("ffprobe: %v", err)
		}
		return strings.TrimSpace(string(out))
	}
	if got := probe("seg_0_00000.ts"); !strings.Contains(got, "1280,720") || !strings.Contains(got, "audio") {
		t.Errorf("720p segment: %q", got)
	}
	if got := probe("seg_1_00000.ts"); !strings.Contains(got, "854,480") || !strings.Contains(got, "audio") {
		t.Errorf("480p segment: %q", got)
	}
	if _, err := os.Stat(filepath.Join(tr.Dir, id, "index.m3u8")); err == nil {
		t.Error("an adaptive stream has no single playlist")
	}
}

// One quality step is a single stream at that size.
func TestStartOneStep(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "clip.mp4")
	gen := exec.Command(ffmpeg, "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "testsrc=size=1280x720:rate=24:duration=6",
		"-c:v", "libx264", "-preset", "ultrafast", src)
	if out, err := gen.CombinedOutput(); err != nil {
		t.Fatalf("make clip: %v\n%s", err, out)
	}
	tr := NewTranscoder(ffmpeg, filepath.Join(dir, "cache"), nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer tr.Close()
	// H.264 at 720p is re-encoded for 360p rather than copied.
	it := store.Item{ID: 1, Path: src, VideoCodec: "h264", Width: intp(1280), Height: intp(720)}
	id, err := tr.Start(context.Background(), 7, it, 0, -1, 0, Steps[3])
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	out, err := exec.Command(strings.TrimSuffix(ffmpeg, "ffmpeg")+"ffprobe", "-v", "error", "-select_streams", "v", "-show_entries", "stream=width,height", "-of", "csv=p=0",
		filepath.Join(tr.Dir, id, "seg00000.ts")).Output()
	if err != nil {
		t.Skipf("ffprobe: %v", err)
	}
	if got := strings.Fields(string(out)); len(got) == 0 || got[0] != "640,360" {
		t.Errorf("size %q, want 640,360", got)
	}
}

func TestNextSegment(t *testing.T) {
	for in, want := range map[string]string{
		"/x/seg00004.ts":    "/x/seg00005.ts",
		"/x/seg_2_00009.ts": "/x/seg_2_00010.ts",
		"/x/index.m3u8":     "",
	} {
		if got := nextSegment(in); got != want {
			t.Errorf("%s: got %q, want %q", in, got, want)
		}
	}
}
