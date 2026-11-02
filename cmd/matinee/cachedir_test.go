//go:build linux

package main

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func TestUseWritableCacheDir(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	t.Run("read-only home", func(t *testing.T) {
		// A file where the home folder should be can't hold .cache, even
		// for root, much like a read-only root filesystem.
		home := filepath.Join(t.TempDir(), "home")
		if err := os.WriteFile(home, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Setenv("HOME", home)
		t.Setenv("XDG_CACHE_HOME", "")
		data := t.TempDir()
		useWritableCacheDir(data, log)
		if got, want := os.Getenv("XDG_CACHE_HOME"), filepath.Join(data, "cache"); got != want {
			t.Fatalf("XDG_CACHE_HOME = %q, want %q", got, want)
		}
	})

	t.Run("writable home", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		t.Setenv("XDG_CACHE_HOME", "")
		useWritableCacheDir(t.TempDir(), log)
		if got := os.Getenv("XDG_CACHE_HOME"); got != "" {
			t.Fatalf("XDG_CACHE_HOME = %q, want it left unset", got)
		}
	})

	t.Run("user's own setting", func(t *testing.T) {
		t.Setenv("HOME", "/nonexistent")
		t.Setenv("XDG_CACHE_HOME", "/mine")
		useWritableCacheDir(t.TempDir(), log)
		if got := os.Getenv("XDG_CACHE_HOME"); got != "/mine" {
			t.Fatalf("XDG_CACHE_HOME = %q, want /mine", got)
		}
	})
}
