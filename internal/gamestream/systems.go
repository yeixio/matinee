package gamestream

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
)

// System is a game console Matinee can play, and the emulator it uses.
type System struct {
	Key        string   `json:"key"`
	Name       string   `json:"name"`
	Core       string   `json:"-"`
	Extensions []string `json:"extensions"`
}

// Systems are the consoles the emulators in games/cores.txt play. The keys
// are Box Office's.
var Systems = []System{
	{Key: "gb", Name: "Game Boy", Core: "gambatte_libretro.so", Extensions: []string{".gb"}},
	{Key: "gbc", Name: "Game Boy Color", Core: "gambatte_libretro.so", Extensions: []string{".gbc"}},
	{Key: "gba", Name: "Game Boy Advance", Core: "mgba_libretro.so", Extensions: []string{".gba"}},
	{Key: "nes", Name: "Nintendo Entertainment System", Core: "fceumm_libretro.so", Extensions: []string{".nes", ".fds", ".unf", ".unif"}},
}

// LagTest is the built-in lag test, which needs no game file.
const LagTest = "lagtest.so"

// Available is the systems whose emulator is on this server.
func (m *Manager) Available() []System {
	var out []System
	for _, s := range Systems {
		if _, err := os.Stat(filepath.Join(m.cfg.CoreDir, s.Core)); err == nil {
			out = append(out, s)
		}
	}
	return out
}

// HasLagTest says whether the lag test is on this server.
func (m *Manager) HasLagTest() bool {
	_, err := os.Stat(filepath.Join(m.cfg.CoreDir, LagTest))
	return err == nil
}

// SystemFor says which system plays the game at path, by its extension, or
// for a zip by the extension of the game inside. ok is false for a file no
// emulator here plays.
func (m *Manager) SystemFor(path string) (System, bool) {
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".zip" {
		ext = zipGameExt(path)
	}
	for _, s := range m.Available() {
		for _, e := range s.Extensions {
			if e == ext {
				return s, true
			}
		}
	}
	return System{}, false
}

// IsGameFile says whether the file at path might be a game, by its name
// alone, for listing folders without opening every zip.
func (m *Manager) IsGameFile(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	if ext == ".zip" {
		return true
	}
	for _, s := range m.Available() {
		for _, e := range s.Extensions {
			if e == ext {
				return true
			}
		}
	}
	return false
}

// zipGameExt is the extension of the biggest file in a zip, which is the
// game (see unzipGame).
func zipGameExt(path string) string {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return ""
	}
	defer zr.Close()
	var best *zip.File
	for _, f := range zr.File {
		if f.FileInfo().IsDir() || strings.HasPrefix(filepath.Base(f.Name), ".") {
			continue
		}
		if best == nil || f.UncompressedSize64 > best.UncompressedSize64 {
			best = f
		}
	}
	if best == nil {
		return ""
	}
	return strings.ToLower(filepath.Ext(best.Name))
}
