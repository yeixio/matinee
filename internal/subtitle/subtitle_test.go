package subtitle

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/yeixio/matinee/internal/probe"
)

func TestSidecars(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{
		"Movie (2021).mkv",
		"Movie (2021).srt",
		"Movie (2021).en.srt",
		"Movie (2021).en.forced.srt",
		"Movie (2021).eng.hi.srt",
		"Movie (2021).English.SDH.vtt",
		"Movie (2021).pt-BR.ass",
		"Movie (2021).hi.srt",
		"Movie (2021).Commentary.en.srt",
		"movie (2021).FR.srt", // case doesn't matter
		"Movie (2021) Extended.srt",
		"Movie (2021).en.sub", // VobSub pairs aren't supported yet
		"Other.en.srt",
	} {
		os.WriteFile(filepath.Join(dir, name), nil, 0o644)
	}
	got := map[string]Track{}
	for _, tr := range Sidecars(filepath.Join(dir, "Movie (2021).mkv")) {
		got[filepath.Base(tr.Path)] = tr
		if !tr.External || len(tr.ID) != 13 || tr.ID[0] != 'x' {
			t.Errorf("%s: bad track %+v", tr.Path, tr)
		}
	}
	want := map[string]struct{ label, lang string }{
		"Movie (2021).srt":               {"SRT file", ""},
		"Movie (2021).en.srt":            {"English", "en"},
		"Movie (2021).en.forced.srt":     {"English (Forced)", "en"},
		"Movie (2021).eng.hi.srt":        {"English (SDH)", "en"},
		"Movie (2021).English.SDH.vtt":   {"English (SDH)", "en"},
		"Movie (2021).pt-BR.ass":         {"Portuguese (Brazil)", "pt"},
		"Movie (2021).hi.srt":            {"Hindi", "hi"},
		"Movie (2021).Commentary.en.srt": {"English (Commentary)", "en"},
		"movie (2021).FR.srt":            {"French", "fr"},
	}
	if len(got) != len(want) {
		t.Errorf("found %d sidecars, want %d: %v", len(got), len(want), got)
	}
	for name, w := range want {
		tr, ok := got[name]
		if !ok {
			t.Errorf("%s not found", name)
			continue
		}
		if tr.Label != w.label || tr.Language != w.lang {
			t.Errorf("%s: got %q (%q), want %q (%q)", name, tr.Label, tr.Language, w.label, w.lang)
		}
	}
	if !got["Movie (2021).en.forced.srt"].Forced || !got["Movie (2021).eng.hi.srt"].SDH {
		t.Error("flags not read from file names")
	}
}

func TestListEmbedded(t *testing.T) {
	tracks := List(filepath.Join(t.TempDir(), "x.mkv"), []probe.Subtitle{
		{Index: 0, Codec: "subrip", Language: "eng", Default: true},
		{Index: 1, Codec: "hdmv_pgs_subtitle", Language: "ger", Forced: true, Title: "Forced"},
		{Index: 2, Codec: "eia_608"}, // not supported: left out
		{Index: 3, Codec: "ass", Language: "und", Title: "Signs & Songs"},
		{Index: 4, Codec: "mov_text"},
	})
	want := []struct {
		id, label string
		burn      bool
	}{
		{"e0", "English", false},
		{"e1", "German (Forced)", true},
		{"e3", "Signs & Songs", false},
		{"e4", "Track 5", false},
	}
	if len(tracks) != len(want) {
		t.Fatalf("got %+v", tracks)
	}
	for i, w := range want {
		if tracks[i].ID != w.id || tracks[i].Label != w.label || tracks[i].BurnIn != w.burn {
			t.Errorf("track %d: got %+v, want %+v", i, tracks[i], w)
		}
	}
}
