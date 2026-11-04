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

func TestSidecarsInSubsFoldersAndPictures(t *testing.T) {
	root := t.TempDir()
	touch := func(rel string) {
		p := filepath.Join(root, rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, nil, 0o644)
	}
	for _, f := range []string{
		"Movie/Movie.mkv", "Movie/Movie.en.sup", "Movie/Movie.fr.idx", "Movie/Movie.fr.sub", "Movie/Movie.de.idx",
		"Movie/Subs/2_English.srt", "Movie/Subs/3_Spanish.srt", "Movie/Subtitles/Movie.it.srt",
		"Show/E01.mkv", "Show/E02.mkv", "Show/Subs/E01/2_English.srt", "Show/Subs/E02/2_English.srt", "Show/Subs/everyone.srt",
	} {
		touch(f)
	}
	got := func(video string) map[string]Track {
		m := map[string]Track{}
		for _, tr := range Sidecars(filepath.Join(root, video)) {
			rel, _ := filepath.Rel(root, tr.Path)
			m[filepath.ToSlash(rel)] = tr
		}
		return m
	}
	movie := got("Movie/Movie.mkv")
	want := map[string]struct {
		lang   string
		burnIn bool
	}{
		"Movie/Movie.en.sup": {"en", true}, "Movie/Movie.fr.idx": {"fr", true},
		"Movie/Subs/2_English.srt": {"en", false}, "Movie/Subs/3_Spanish.srt": {"es", false}, "Movie/Subtitles/Movie.it.srt": {"it", false},
	}
	if len(movie) != len(want) {
		t.Errorf("movie: got %d tracks, want %d: %v", len(movie), len(want), movie)
	}
	for p, w := range want {
		tr, ok := movie[p]
		if !ok || tr.Language != w.lang || tr.BurnIn != w.burnIn {
			t.Errorf("%s: %+v, want language %s burnIn %v", p, tr, w.lang, w.burnIn)
		}
	}
	if tr := movie["Movie/Subs/2_English.srt"]; tr.Label != "English" {
		t.Errorf("numbered file label %q", tr.Label)
	}
	if movie["Movie/Subs/2_English.srt"].ID == got("Show/E01.mkv")["Show/Subs/E01/2_English.srt"].ID {
		t.Error("ids should differ by folder")
	}
	// Each episode gets its own folder; a flat file can't say whose it is.
	e1 := got("Show/E01.mkv")
	if _, ok := e1["Show/Subs/E01/2_English.srt"]; !ok || len(e1) != 1 {
		t.Errorf("episode: %v", e1)
	}
}
