//go:build !windows

package api

import "path/filepath"

// isDriveList is whether path asks for Windows' list of drives.
func isDriveList(string) bool { return false }

func listDrives() []string { return nil }

func parentDir(dir string) string { return filepath.Dir(dir) }
