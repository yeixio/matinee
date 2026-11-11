package probe

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// A healthy file jumps fine; one whose jumps don't finish in time is damaged.
func TestDamaged(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	file := filepath.Join(t.TempDir(), "a.mkv")
	if out, err := exec.Command(ffmpeg, "-v", "error", "-f", "lavfi", "-i", "testsrc2=s=320x180:r=10:d=20",
		"-c:v", "libx264", "-preset", "ultrafast", file).CombinedOutput(); err != nil {
		t.Fatalf("make file: %v\n%s", err, out)
	}
	ctx := context.Background()
	if Damaged(ctx, ffmpeg, file, 20_000, nil) {
		t.Fatal("a healthy file taken for damaged")
	}
	old := jumpWait
	jumpWait = time.Nanosecond
	t.Cleanup(func() { jumpWait = old })
	if !Damaged(ctx, ffmpeg, file, 20_000, nil) {
		t.Fatal("jumps that time out not taken for damage")
	}
}
