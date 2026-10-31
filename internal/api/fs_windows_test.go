package api

import (
	"slices"
	"testing"
)

func TestWindowsFolderPicker(t *testing.T) {
	if !isDriveList("/") || !isDriveList(`\`) || isDriveList(`C:\`) {
		t.Fatal("only / and \\ list the drives")
	}
	if d := listDrives(); !slices.Contains(d, `C:\`) {
		t.Fatalf("drives %q have no C:", d)
	}
	for dir, want := range map[string]string{
		`C:\`:             "/",
		`C:\Users`:        `C:\`,
		`D:\Media\Movies`: `D:\Media`,
		`\\nas\media`:     `\\nas\media`,
		`\\nas\media\TV`:  `\\nas\media\`,
	} {
		if got := parentDir(dir); got != want {
			t.Errorf("parentDir(%q) = %q, want %q", dir, got, want)
		}
	}
}
