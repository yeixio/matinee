package config

import "testing"

func TestParseSize(t *testing.T) {
	for in, want := range map[string]int64{"": 0, "50Gi": 50 << 30, "45G": 45e9, "500Mi": 500 << 20, "1073741824": 1 << 30, "1.5Ti": 3 << 39} {
		if got, err := ParseSize(in); err != nil || got != want {
			t.Errorf("ParseSize(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, bad := range []string{"lots", "-1Gi", "Gi"} {
		if _, err := ParseSize(bad); err == nil {
			t.Errorf("ParseSize(%q) took it", bad)
		}
	}
}
