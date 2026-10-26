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
