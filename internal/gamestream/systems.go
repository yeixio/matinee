package gamestream

import (
	"archive/zip"
	"path/filepath"
	"slices"
	"strings"
)

// System is a game console Matinee can play, and the emulator it uses.
type System struct {
	Key  string `json:"key"`
	Name string `json:"name"`
	// Emulator is the emulator's own name for itself.
	Emulator   string   `json:"emulator"`
	Extensions []string `json:"extensions"`
	// Analog says the console's controller has analog sticks, which the
	// player sends as sticks rather than as the d-pad.
	Analog bool `json:"analog,omitempty"`
	// Custom says an admin added this emulator (see emulators.go).
	Custom bool `json:"custom,omitempty"`

	// Core is the emulator's file: a name in Config.CoreDir in builtIn,
	// the full path everywhere else.
	Core string `json:"-"`
	// Options are the emulator's settings that mustn't keep their defaults.
	Options map[string]string `json:"-"`
}

// playsZips says the emulator takes a zip as it is, as arcade emulators
// do, instead of the game inside it.
func (s System) playsZips() bool { return slices.Contains(s.Extensions, ".zip") }

// builtIn are the consoles the emulators in games/cores.txt play. The keys
// are Box Office's. Where two emulators take the same kind of file, the one
// listed first plays it.
var builtIn = []System{
	{Key: "gb", Name: "Game Boy", Emulator: "Gambatte", Core: "gambatte_libretro.so", Extensions: []string{".gb"}},
	{Key: "gbc", Name: "Game Boy Color", Emulator: "Gambatte", Core: "gambatte_libretro.so", Extensions: []string{".gbc"}},
	{Key: "gba", Name: "Game Boy Advance", Emulator: "mGBA", Core: "mgba_libretro.so", Extensions: []string{".gba"}},
	{Key: "nes", Name: "Nintendo Entertainment System", Emulator: "FCEUmm", Core: "fceumm_libretro.so", Extensions: []string{".nes", ".fds", ".unf", ".unif"}},
	{Key: "snes", Name: "Super Nintendo", Emulator: "bsnes", Core: "bsnes_libretro.so", Extensions: []string{".sfc", ".smc"}},
	{Key: "genesis", Name: "Sega Genesis / Mega Drive", Emulator: "clownmdemu", Core: "clownmdemu_libretro.so", Extensions: []string{".md", ".gen", ".bin"}},
	// Without a graphics card in the server, N64 pictures are drawn by
	// angrylion, in software.
	{Key: "n64", Name: "Nintendo 64", Emulator: "ParaLLEl N64", Core: "parallel_n64_libretro.so", Extensions: []string{".n64", ".v64", ".z64"},
		Analog: true, Options: map[string]string{"parallel-n64-gfxplugin": "angrylion"}},
	// Discs come as a .cue with its .bin tracks beside it, or one .chd or
	// .pbp. A .bin alone is a Genesis cartridge.
	{Key: "psx", Name: "PlayStation", Emulator: "PCSX ReARMed", Core: "pcsx_rearmed_libretro.so", Extensions: []string{".cue", ".chd", ".pbp", ".m3u", ".iso"}},
}

// LagTest is the built-in lag test, which needs no game file.
const LagTest = "lagtest.so"

// Available is the systems this server can play: those of the emulators
// an admin added, which come first so they're picked over the built-in
// ones for the same games, then the built-in ones that are here.
func (m *Manager) Available() []System {
	out := m.userSystems()
	for _, s := range builtIn {
		s.Core = filepath.Join(m.cfg.CoreDir, s.Core)
		if fileExists(s.Core) {
			out = append(out, s)
		}
	}
	return out
}

// HasLagTest says whether the lag test is on this server.
func (m *Manager) HasLagTest() bool {
	return fileExists(filepath.Join(m.cfg.CoreDir, LagTest))
}

// SystemFor says which system plays the game at path, by its extension. A
// zip is played by the system for the game inside, or failing that by an
// emulator that takes zips themselves (arcade games). ok is false for a
// file no emulator here plays.
func (m *Manager) SystemFor(path string) (System, bool) {
	systems := m.Available()
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".zip" {
		if s, ok := systemByExt(systems, zipGameExt(path)); ok {
			return s, true
		}
	}
	return systemByExt(systems, ext)
}

func systemByExt(systems []System, ext string) (System, bool) {
	if ext == "" {
		return System{}, false
	}
	for _, s := range systems {
		if slices.Contains(s.Extensions, ext) {
			return s, true
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
	_, ok := systemByExt(m.Available(), ext)
	return ok
}

// zipGameExt is the extension of the game in a zip (see zipGame).
func zipGameExt(path string) string {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return ""
	}
	defer zr.Close()
	best, _ := zipGame(zr.File)
	if best == nil {
		return ""
	}
	return strings.ToLower(filepath.Ext(best.Name))
}

// discLists are the files that list a disc's tracks, or a game's discs,
// which are played instead of the tracks themselves.
var discLists = []string{".m3u", ".cue", ".gdi", ".ccd"}

// zipGame is the game in a zip: the file listing its discs or tracks if
// there is one (whole says every file must be unpacked beside it), or else
// the biggest file, which is the game in a zip that also holds a readme.
func zipGame(files []*zip.File) (best *zip.File, whole bool) {
	rank := func(f *zip.File) int {
		return len(discLists) - max(-1, slices.Index(discLists, strings.ToLower(filepath.Ext(f.Name))))
	}
	for _, f := range files {
		if f.FileInfo().IsDir() || strings.HasPrefix(filepath.Base(f.Name), ".") {
			continue
		}
		switch {
		case best == nil:
			best = f
		case rank(f) < rank(best):
			best = f
		case rank(f) == rank(best) && f.UncompressedSize64 > best.UncompressedSize64:
			best = f
		}
	}
	return best, best != nil && slices.Contains(discLists, strings.ToLower(filepath.Ext(best.Name)))
}
