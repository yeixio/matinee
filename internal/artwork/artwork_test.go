package artwork

import "testing"

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
