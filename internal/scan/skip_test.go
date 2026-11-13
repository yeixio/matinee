package scan

import (
	"errors"
	"testing"
)

func TestAddSkip(t *testing.T) {
	var st Stats
	st.addSkip(Skip("arcade", "mame"), "/games/mame/pacman.zip")
	st.addSkip(Skip("arcade", "mame"), "/games/mame/galaga.zip")
	st.addSkip(Skip("unknown", "c64"), "/games/c64/esprade.zip")
	st.addSkip(errors.New("other"), "/games/x.bin") // no reason: not counted
	want := []SkipCount{
		{Reason: "arcade", Group: "mame", Count: 2, Example: "pacman.zip"},
		{Reason: "unknown", Group: "c64", Count: 1, Example: "esprade.zip"},
	}
	if len(st.Skipped) != len(want) {
		t.Fatalf("got %+v", st.Skipped)
	}
	for i := range want {
		if st.Skipped[i] != want[i] {
			t.Fatalf("got %+v, want %+v", st.Skipped[i], want[i])
		}
	}
	if !errors.Is(Skip("arcade", ""), ErrSkip) {
		t.Fatal("Skip should be ErrSkip")
	}
}
