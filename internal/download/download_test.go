package download

import (
	"slices"
	"testing"

	"github.com/yeixio/matinee/internal/store"
)

func TestConvertArgs(t *testing.T) {
	ptr := func(n int) *int { return &n }
	dur := int64(60_000) // a minute
	q, _ := QualityByName("720p")

	small := store.Item{Path: "/m/a.mkv", VideoCodec: "h264", Height: ptr(720), DurationMs: &dur, Size: 20_000_000} // ~2.7 Mbps
	if args := ConvertArgs(small, q, -1, "out"); !slices.Contains(args, "copy") {
		t.Errorf("small H.264 should keep its picture: %v", args)
	}
	big := small
	big.Size = 100_000_000 // ~13 Mbps
	if args := ConvertArgs(big, q, -1, "out"); slices.Contains(args, "copy") || !slices.Contains(args, "libx264") {
		t.Errorf("a high bitrate file should be re-encoded: %v", args)
	}
	tall := small
	tall.Height = ptr(1080)
	if args := ConvertArgs(tall, q, -1, "out"); slices.Contains(args, "copy") {
		t.Errorf("1080p doesn't fit 720p: %v", args)
	}
	if args := ConvertArgs(small, q, 2, "out"); slices.Contains(args, "copy") || !slices.Contains(args, "-filter_complex") {
		t.Errorf("burning in subtitles needs a re-encode: %v", args)
	}
	song := store.Item{Path: "/m/a.flac", AudioCodec: "flac"}
	if args := ConvertArgs(song, q, -1, "out"); !slices.Contains(args, "ipod") || !slices.Contains(args, "-vn") {
		t.Errorf("music converts to M4A: %v", args)
	}
}
