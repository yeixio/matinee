package main

import (
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
)

// useWritableCacheDir points ffmpeg's libraries at a cache folder they can
// write to. The GPU drivers (Mesa's shader cache for VAAPI) and fontconfig
// (subtitle burn-in) keep caches under ~/.cache, but the container image runs
// with a read-only root filesystem, so ffmpeg logs "Failed to create
// /home/matinee/.cache for shader cache (Read-only file system)---disabling"
// and recompiles its GPU shaders on every transcode. When that folder can't be
// written, XDG_CACHE_HOME moves those caches into the data folder, which the
// server can always write. ffmpeg inherits it from the server's environment.
//
// Nothing changes when the user set XDG_CACHE_HOME themselves or the home
// folder is writable, and Windows and macOS don't use XDG_CACHE_HOME.
func useWritableCacheDir(dataDir string, log *slog.Logger) {
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" || os.Getenv("XDG_CACHE_HOME") != "" {
		return
	}
	if home, err := os.UserHomeDir(); err == nil && writable(filepath.Join(home, ".cache")) {
		return
	}
	dir, err := filepath.Abs(filepath.Join(dataDir, "cache"))
	if err != nil || !writable(dir) {
		log.Warn("no writable cache folder for ffmpeg; GPU shaders are compiled again for each transcode", "folder", dir)
		return
	}
	os.Setenv("XDG_CACHE_HOME", dir)
	log.Debug("home folder isn't writable; ffmpeg keeps its caches in the data folder", "folder", dir)
}

// writable reports whether dir exists or can be made, and files can be
// created in it.
func writable(dir string) bool {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false
	}
	f, err := os.CreateTemp(dir, ".write-test-*")
	if err != nil {
		return false
	}
	f.Close()
	os.Remove(f.Name())
	return true
}
