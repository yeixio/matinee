package gamestream

import (
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/yeixio/matinee/internal/gamedat"
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

// playsArchive says the emulator takes this zip or 7z as it is.
func (s System) playsArchive(path string) bool {
	return slices.Contains(s.Extensions, strings.ToLower(filepath.Ext(path)))
}

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
	{Key: "atari2600", Name: "Atari 2600", Emulator: "Stella 2014", Core: "stella2014_libretro.so", Extensions: []string{".a26"}},
	{Key: "atari5200", Name: "Atari 5200", Emulator: "a5200", Core: "a5200_libretro.so", Extensions: []string{".a52"}},
	{Key: "atari7800", Name: "Atari 7800", Emulator: "ProSystem", Core: "prosystem_libretro.so", Extensions: []string{".a78"}},
	{Key: "lynx", Name: "Atari Lynx", Emulator: "Beetle Lynx", Core: "mednafen_lynx_libretro.so", Extensions: []string{".lnx"}},
	{Key: "jaguar", Name: "Atari Jaguar", Emulator: "Virtual Jaguar", Core: "virtualjaguar_libretro.so", Extensions: []string{".j64", ".jag"}},
	{Key: "sms", Name: "Sega Master System", Emulator: "Gearsystem", Core: "gearsystem_libretro.so", Extensions: []string{".sms"}},
	{Key: "gamegear", Name: "Game Gear", Emulator: "Gearsystem", Core: "gearsystem_libretro.so", Extensions: []string{".gg"}},
	{Key: "colecovision", Name: "ColecoVision", Emulator: "Gearcoleco", Core: "gearcoleco_libretro.so", Extensions: []string{".col"}},
	{Key: "intellivision", Name: "Intellivision", Emulator: "FreeIntv", Core: "freeintv_libretro.so", Extensions: []string{".int"}},
	{Key: "pce", Name: "PC Engine / TurboGrafx-16", Emulator: "Beetle PCE Fast", Core: "mednafen_pce_fast_libretro.so", Extensions: []string{".pce"}},
	{Key: "ngp", Name: "Neo Geo Pocket", Emulator: "Beetle NeoPop", Core: "mednafen_ngp_libretro.so", Extensions: []string{".ngp"}},
	{Key: "ngpc", Name: "Neo Geo Pocket Color", Emulator: "Beetle NeoPop", Core: "mednafen_ngp_libretro.so", Extensions: []string{".ngc"}},
	{Key: "wonderswan", Name: "WonderSwan", Emulator: "Beetle Cygne", Core: "mednafen_wswan_libretro.so", Extensions: []string{".ws"}},
	{Key: "wonderswancolor", Name: "WonderSwan Color", Emulator: "Beetle Cygne", Core: "mednafen_wswan_libretro.so", Extensions: []string{".wsc"}},
	{Key: "vb", Name: "Virtual Boy", Emulator: "Beetle VB", Core: "mednafen_vb_libretro.so", Extensions: []string{".vb"}},
	{Key: "c64", Name: "Commodore 64", Emulator: "VICE", Core: "vice_x64_libretro.so", Extensions: []string{".d64", ".t64", ".prg", ".crt", ".g64"}},
	{Key: "amiga", Name: "Amiga", Emulator: "PUAE", Core: "puae_libretro.so", Extensions: []string{".adf", ".ipf"}},
	{Key: "zxspectrum", Name: "ZX Spectrum", Emulator: "Fuse", Core: "fuse_libretro.so", Extensions: []string{".tzx", ".z80", ".sna"}},
	// Emulators that take a zip as it is come last, so a console's game in
	// a zip goes to its console. They play only games filed for their
	// system (SystemForGame). MAME is built on its own (games/mame.txt).
	{Key: "pc", Name: "PC (DOS)", Emulator: "DOSBox Pure", Core: "dosbox_pure_libretro.so", Extensions: []string{".dosz", ".exe"}},
	{Key: "mame", Name: "Arcade", Emulator: "MAME", Core: "mame_libretro.so", Extensions: []string{".zip", ".7z"}},
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
		if fileExists(s.Core) && !m.isBroken(s.Core) {
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
	if gamedat.IsArchive(path) {
		if s, ok := systemByExt(systems, zipGameExt(path)); ok {
			return s, true
		}
	}
	return systemByExt(systems, ext)
}

// SystemForGame says which system plays a game of a Games library, filed
// for the system key (gamedat's): an emulator an admin added for its
// files, else the built-in one for its system. Without a key it's
// SystemFor.
func (m *Manager) SystemForGame(path, key string) (System, bool) {
	if key == "" {
		return m.SystemFor(path)
	}
	systems := m.Available()
	ext := strings.ToLower(filepath.Ext(path))
	if gamedat.IsArchive(path) && key != "mame" && key != "pc" {
		if inner := zipGameExt(path); inner != "" {
			ext = inner
		}
	}
	for _, s := range systems {
		if s.Custom && slices.Contains(s.Extensions, ext) {
			return s, true
		}
	}
	for _, s := range systems {
		if !s.Custom && s.Key == key {
			return s, true
		}
	}
	return System{}, false
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
	if gamedat.IsArchive(name) {
		return true
	}
	_, ok := systemByExt(m.Available(), ext)
	return ok
}

// zipGameExt is the extension of the game in a zip or 7z (see zipGame).
func zipGameExt(path string) string {
	files, err := gamedat.ListArchive(path)
	if err != nil {
		return ""
	}
	best, _ := zipGame(files, nil)
	if best == nil {
		return ""
	}
	return strings.ToLower(filepath.Ext(best.Name))
}

// discLists are the files that list a disc's tracks, or a game's discs,
// which are played instead of the tracks themselves.
var discLists = []string{".m3u", ".cue", ".gdi", ".ccd"}

// zipGame is the game in a zip or 7z: the file listing its discs or tracks
// if there is one (whole says every file must be unpacked beside it), else
// a file of the kind the emulator plays (exts, when known), the best of
// them by its name, then the biggest, which is the game in a zip that also
// holds a readme. "Best" matters for GoodTools-style archives that hold
// every dump of a game: a verified good one ([!]) from the US or Europe,
// not a bad dump, an overdump, a hack or a beta.
func zipGame(files []gamedat.File, exts []string) (best *gamedat.File, whole bool) {
	rank := func(f *gamedat.File) int {
		return len(discLists) - max(-1, slices.Index(discLists, strings.ToLower(filepath.Ext(f.Name))))
	}
	plays := func(f *gamedat.File) bool {
		return len(exts) == 0 || slices.Contains(exts, strings.ToLower(filepath.Ext(f.Name)))
	}
	better := func(f, b *gamedat.File) bool {
		if rf, rb := rank(f), rank(b); rf != rb {
			return rf < rb
		}
		if pf, pb := plays(f), plays(b); pf != pb {
			return pf
		}
		if qf, qb := dumpRank(f.Base()), dumpRank(b.Base()); qf != qb {
			return qf < qb
		}
		if f.Size != b.Size {
			return f.Size > b.Size
		}
		return f.Name < b.Name
	}
	for i := range files {
		f := &files[i]
		if strings.HasPrefix(f.Base(), ".") {
			continue
		}
		if best == nil || better(f, best) {
			best = f
		}
	}
	return best, best != nil && slices.Contains(discLists, strings.ToLower(filepath.Ext(best.Name)))
}

// dumpFlag are GoodTools' and No-Intro's marks for a dump that isn't the
// game as sold: bad, overdumped, trained, hacked, fixed, pirate,
// translated, alternative, or not finished.
var dumpFlag = regexp.MustCompile(`(?i)\[(b|o|t|h|f|p|a|T[+-])[0-9a-z+ -]*\]|\((beta|proto|prototype|demo|sample|hack|pirate|unl)[^)]*\)`)

// dumpRank orders a game's dumps by name, lower being better.
func dumpRank(name string) int {
	r := 10 * len(dumpFlag.FindAllString(name, -1))
	if strings.Contains(name, "[!]") {
		r -= 5
	}
	switch {
	case usRegion.MatchString(name):
		r -= 3
	case euRegion.MatchString(name):
		r -= 2
	}
	return r
}

var (
	usRegion = regexp.MustCompile(`\((U|USA|World|UE|JU|JUE|USA, Europe)\)`)
	euRegion = regexp.MustCompile(`\((E|Europe)\)`)
)
