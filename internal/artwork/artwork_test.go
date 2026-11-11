package artwork

import (
	"errors"
	"testing"
)

func TestLarge(t *testing.T) {
	for _, c := range []struct{ src, kind, want string }{
		{"https://image.tmdb.org/t/p/w1280/abc.jpg", "backdrop", "https://image.tmdb.org/t/p/original/abc.jpg"},
		{"https://image.tmdb.org/t/p/w500/abc.jpg", "poster", "https://image.tmdb.org/t/p/w780/abc.jpg"},
		{"/media/Movies/Film/poster.jpg", "poster", "/media/Movies/Film/poster.jpg"},
		{"embedded:/music/a.flac", "poster", "embedded:/music/a.flac"},
	} {
		if got := Large(c.src, c.kind); got != c.want {
			t.Errorf("Large(%q, %q) = %q, want %q", c.src, c.kind, got, c.want)
		}
	}
}

// An image that couldn't be had isn't fetched again for a while, so a page
// of missing covers doesn't wait on each one every time it's shown.
func TestFailedImagesWait(t *testing.T) {
	c := NewCache(t.TempDir(), "ffmpeg")
	calls := 0
	fill := func(dst string) error { calls++; return errors.New("HTTP 404") }
	for range 3 {
		if _, err := c.cached("https://example.com/missing.jpg", ".jpg", fill); err == nil {
			t.Fatal("no error for a missing image")
		}
	}
	if calls != 1 {
		t.Fatalf("fetched %d times", calls)
	}
}
