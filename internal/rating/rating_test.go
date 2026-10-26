package rating

import "testing"

func TestLevel(t *testing.T) {
	for _, c := range []struct {
		country, cert string
		want          int
	}{
		{"US", "G", LittleKids},
		{"US", "TV-Y7", Kids},
		{"US", "pg-13", Teens},
		{"US", "TV-MA", Adults},
		{"US", "NR", Unknown},
		{"US", "", Unknown},
		{"GB", "PG", Kids},
		{"GB", "12A", Teens},
		{"GB", "15", Adults},
		{"AU", "MA15+", Adults},
		{"DE", "0", LittleKids},
		{"DE", "6", Kids},
		{"DE", "12", Teens},
		{"DE", "16", Adults},
		{"FR", "10", Kids},
		{"SE", "Btl", Unknown},
		{"SE", "7", Kids},
		{"NL", "AL", LittleKids},
	} {
		if got := Level(c.country, c.cert); got != c.want {
			t.Errorf("Level(%q, %q) = %d, want %d", c.country, c.cert, got, c.want)
		}
	}
}
