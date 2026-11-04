package scan

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestIsTrailerFile(t *testing.T) {
	for name, want := range map[string]bool{
		"Heat (1995)-trailer.mkv": true,
		"Heat.1995.trailer.mp4":   true,
		"heat_trailer2.mov":       true,
		"trailer.mp4":             true,
		"Trailer.MKV":             true,
		"Heat (1995).mkv":         false,
		"Trailer Park Boys.mkv":   false,
		"Heat-trailer.srt":        false,
		"The Trailers (2004).mkv": false,
	} {
		if got := IsTrailerFile(name); got != want {
			t.Errorf("IsTrailerFile(%q) = %v, want %v", name, got, want)
		}
	}
}

func touch(t *testing.T, root string, rels ...string) {
	t.Helper()
	for _, rel := range rels {
		p := filepath.Join(root, rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func rel(root string, paths []string) []string {
	out := []string{}
	for _, p := range paths {
		r, _ := filepath.Rel(root, p)
		out = append(out, filepath.ToSlash(r))
	}
	return out
}

func TestMovieTrailers(t *testing.T) {
	root := t.TempDir()
	touch(t, root,
		"Heat (1995)/Heat (1995).mkv",
		"Heat (1995)/Heat (1995)-trailer.mp4",
		"Heat (1995)/Trailers/Teaser.mkv",
		"Heat (1995)/Trailers/notes.txt",
		"Alien (1979).mkv",
		"Alien (1979)-trailer.mkv",
		"Aliens (1986)-trailer.mkv",
		"trailer.mp4",
	)
	got := rel(root, MovieTrailers(root, filepath.Join(root, "Heat (1995)/Heat (1995).mkv")))
	if want := []string{"Heat (1995)/Heat (1995)-trailer.mp4", "Heat (1995)/Trailers/Teaser.mkv"}; !slices.Equal(got, want) {
		t.Errorf("own folder: %v, want %v", got, want)
	}
	// A shared folder only gives a movie the trailers named after it.
	got = rel(root, MovieTrailers(root, filepath.Join(root, "Alien (1979).mkv")))
	if want := []string{"Alien (1979)-trailer.mkv"}; !slices.Equal(got, want) {
		t.Errorf("shared folder: %v, want %v", got, want)
	}
}

func TestShowFolders(t *testing.T) {
	root := t.TempDir()
	touch(t, root, "Severance/trailer.mkv", "Severance/Season 2/Trailers/Season 2 Teaser.mp4")
	show, season := ShowFolders(root, filepath.Join(root, "Severance/Season 2/Severance S02E01.mkv"))
	if show != filepath.Join(root, "Severance") || season != filepath.Join(root, "Severance/Season 2") {
		t.Fatalf("folders: %q %q", show, season)
	}
	if got := rel(root, FolderTrailers(show)); !slices.Equal(got, []string{"Severance/trailer.mkv"}) {
		t.Errorf("show trailers: %v", got)
	}
	got := FolderTrailers(season)
	if len(got) != 1 || TrailerName(got[0]) != "Season 2 Teaser" {
		t.Errorf("season trailers: %v", got)
	}
	if show, season := ShowFolders(root, filepath.Join(root, "Flat S01E01.mkv")); show != "" || season != "" {
		t.Errorf("episode in the library folder: %q %q", show, season)
	}
	if _, season := ShowFolders(root, filepath.Join(root, "Show/Show S01E01.mkv")); season != "" {
		t.Errorf("no season folder: %q", season)
	}
}

func TestExtraFileKind(t *testing.T) {
	for name, want := range map[string]string{
		"Heat (1995)-trailer.mkv":         ExtraTrailer,
		"Heat (1995)-featurette.mkv":      ExtraFeaturette,
		"Bank job-deleted.mp4":            ExtraDeleted,
		"Bank job-deletedscene2.mp4":      ExtraDeleted,
		"Making Heat-behindthescenes.mkv": ExtraBehindTheScenes,
		"Michael Mann-interview.mkv":      ExtraInterview,
		"Heat-scene.mkv":                  ExtraScene,
		"Heat-short.mkv":                  ExtraShort,
		"Heat-other.mkv":                  ExtraOther,
		"Heat-extra.mkv":                  ExtraOther,
		"The.Big.Short.2015.mkv":          "",
		"The Big Short (2015).mkv":        "",
		"Heat-featurette.srt":             "",
	} {
		got, ok := ExtraFileKind(name)
		if got != want || ok != (want != "") {
			t.Errorf("ExtraFileKind(%q) = %q %v, want %q", name, got, ok, want)
		}
	}
}

func TestIsExtraDir(t *testing.T) {
	for _, c := range []struct {
		name   string
		nested bool
		want   bool
	}{
		{"Trailers", false, true},
		{"Behind The Scenes", true, true},
		{"Behind.the.Scenes", true, true},
		{"deleted_scenes", true, true},
		{"Featurettes", true, true},
		{"Extras", true, true},
		{"Shorts", true, true},
		// Right in the library's folder, these are groups of titles.
		{"Shorts", false, false},
		{"Other", false, false},
		{"Season 1", true, false},
		{"Specials", true, false},
	} {
		if got := IsExtraDir(c.name, c.nested); got != c.want {
			t.Errorf("IsExtraDir(%q, %v) = %v", c.name, c.nested, got)
		}
	}
}

func TestMovieExtras(t *testing.T) {
	root := t.TempDir()
	touch(t, root,
		"Heat (1995)/Heat (1995).mkv",
		"Heat (1995)/Heat (1995)-featurette.mkv",
		"Heat (1995)/Heat (1995)-trailer.mp4",
		"Heat (1995)/Deleted Scenes/Bank job.mkv",
		"Heat (1995)/Extras/Interviews/Michael Mann.mkv",
		"Heat (1995)/Extras/Storyboards.mkv",
		"Heat (1995)/Extras/notes.txt",
		"Alien (1979).mkv",
		"Alien (1979)-behindthescenes.mkv",
		"Aliens (1986)-featurette.mkv",
	)
	got := MovieExtras(root, filepath.Join(root, "Heat (1995)/Heat (1995).mkv"))
	want := []Extra{
		{filepath.Join(root, "Heat (1995)/Heat (1995)-trailer.mp4"), ExtraTrailer},
		{filepath.Join(root, "Heat (1995)/Deleted Scenes/Bank job.mkv"), ExtraDeleted},
		{filepath.Join(root, "Heat (1995)/Heat (1995)-featurette.mkv"), ExtraFeaturette},
		{filepath.Join(root, "Heat (1995)/Extras/Interviews/Michael Mann.mkv"), ExtraInterview},
		{filepath.Join(root, "Heat (1995)/Extras/Storyboards.mkv"), ExtraOther},
	}
	if !slices.Equal(got, want) {
		t.Errorf("own folder:\n%v\nwant\n%v", got, want)
	}
	got = MovieExtras(root, filepath.Join(root, "Alien (1979).mkv"))
	if want := []Extra{{filepath.Join(root, "Alien (1979)-behindthescenes.mkv"), ExtraBehindTheScenes}}; !slices.Equal(got, want) {
		t.Errorf("shared folder: %v", got)
	}
}

func TestExtraName(t *testing.T) {
	for _, c := range []struct{ path, want string }{
		{"/m/Heat (1995)/Heat (1995)-featurette.mkv", ""},
		{"/m/Heat (1995)/Making Heat-behindthescenes.mkv", "Making Heat"},
		{"/m/Heat (1995)/Deleted Scenes/Bank.job.mkv", "Bank job"},
		{"/m/Heat (1995)/trailer.mp4", ""},
	} {
		if got := ExtraName(c.path, "Heat", "Heat (1995)"); got != c.want {
			t.Errorf("ExtraName(%q) = %q, want %q", c.path, got, c.want)
		}
	}
}
