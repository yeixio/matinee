package mediabridge

import "testing"

func TestOpusSamples(t *testing.T) {
	for _, c := range []struct {
		p    []byte
		want int
	}{
		{[]byte{0xfc}, 960},        // CELT 20 ms
		{[]byte{0xf0}, 480},        // CELT 10 ms
		{[]byte{0xf1}, 960},        // two 10 ms frames
		{[]byte{0xf3, 0x03}, 1440}, // three
		{[]byte{0x00}, 480},        // SILK 10 ms
		{nil, 480},
	} {
		if got := opusSamples(c.p); got != c.want {
			t.Errorf("%x: %d, want %d", c.p, got, c.want)
		}
	}
}
