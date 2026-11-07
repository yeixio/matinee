package store

import (
	"slices"
	"strings"
	"testing"
)

// A movie's files whose names don't say which cut they are are told apart
// by how long they run.
func TestGroupEditionsByLength(t *testing.T) {
	const minute = 60 * 1000
	named := func(path string) string {
		if strings.Contains(path, "Director") {
			return "Director's Cut"
		}
		return ""
	}
	cases := []struct {
		name  string
		files []LibraryFile
		want  []string
	}{
		{"two cuts", []LibraryFile{
			{Path: "a.mkv", Kind: "movie", durationMS: 178 * minute},
			{Path: "b.mkv", Kind: "movie", durationMS: 201 * minute},
		}, []string{"Theatrical", "Extended Edition"}},
		{"same cut in two qualities", []LibraryFile{
			{Path: "a.mkv", Kind: "movie", durationMS: 120 * minute},
			{Path: "b.mkv", Kind: "movie", durationMS: 120*minute + 30_000},
		}, []string{"", ""}},
		{"a PAL copy", []LibraryFile{
			{Path: "a.mkv", Kind: "movie", durationMS: 120 * minute},
			{Path: "b.mkv", Kind: "movie", durationMS: 115 * minute},
		}, []string{"", ""}},
		{"the name wins", []LibraryFile{
			{Path: "a.mkv", Kind: "movie", durationMS: 120 * minute},
			{Path: "Director.mkv", Kind: "movie", durationMS: 140 * minute},
		}, []string{"Theatrical", "Director's Cut"}},
		{"length unknown", []LibraryFile{
			{Path: "a.mkv", Kind: "movie"},
			{Path: "b.mkv", Kind: "movie", durationMS: 140 * minute},
		}, []string{"", ""}},
		{"episodes have no cuts", []LibraryFile{
			{Path: "a.mkv", Kind: "episode", durationMS: 20 * minute},
			{Path: "b.mkv", Kind: "episode", durationMS: 45 * minute},
		}, []string{"", ""}},
	}
	for _, c := range cases {
		if got := groupEditions(c.files, named); !slices.Equal(got, c.want) {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}
