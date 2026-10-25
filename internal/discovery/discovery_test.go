package discovery

import (
	"slices"
	"testing"
)

func TestTXT(t *testing.T) {
	got := Announcement{Port: 7171, Version: "1.2.0"}.TXT()
	if want := []string{"product=matinee", "version=1.2.0"}; !slices.Equal(got, want) {
		t.Fatalf("without a URL: %q", got)
	}
	got = Announcement{Port: 443, URL: "https://matinee.example.com/", Version: "1.2.0"}.TXT()
	if !slices.Contains(got, "url=https://matinee.example.com") {
		t.Fatalf("with a URL: %q", got)
	}
	if slices.Contains(got, "home=1") {
		t.Fatalf("an announcer without a home port claimed one: %q", got)
	}
	got = Announcement{Port: 7172, URL: "https://matinee.example.com", Version: "1.2.0", Home: true}.TXT()
	if !slices.Contains(got, "home=1") || !slices.Contains(got, "url=https://matinee.example.com") {
		t.Fatalf("with a home port: %q", got)
	}
}
