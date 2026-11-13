package gamedat

import (
	"path"
	"slices"
	"strings"
	"unicode"
)

// System is a kind of machine games are for.
type System struct {
	// Key names it in the API and the database.
	Key string `json:"key"`
	// Name is what people see, and Short a name that fits a badge.
	Name  string `json:"name"`
	Short string `json:"short"`
	// Maker is who made it.
	Maker string `json:"maker"`
	// Arcade is MAME: games are sets named by short name, checked against
	// MAME's own list. The others are consoles, matched by hash against
	// No-Intro's list.
	Arcade bool `json:"arcade"`
	// Libretro is the system's name in libretro's database and thumbnails.
	Libretro string `json:"-"`
	// Exts are its game files' extensions.
	Exts []string `json:"-"`
	// Folders are names its folder often has, besides its own names.
	Folders []string `json:"-"`
	Header  Header   `json:"-"`
	// Disc says its games are discs, which are told by their names rather
	// than read and hashed.
	Disc bool `json:"-"`
}

// MAMEVersion is the version of MAME arcade sets are checked against and
// played with: games/mame.txt's, the server's MAME. ROM sets are made for
// one version of MAME.
const MAMEVersion = "0.289"

// Systems are the systems Matinee knows, MAME first. They're Box Office's
// (internal/gamedat there), plus the PlayStation.
var Systems = []System{
	{Key: "mame", Name: "Arcade (MAME)", Short: "MAME", Maker: "Arcade", Arcade: true, Libretro: "MAME",
		Folders: []string{"mame", "arcade", "arcades", "mamecurrent", "mameroms", "roms"}},
	{Key: "nes", Name: "Nintendo Entertainment System", Short: "NES", Maker: "Nintendo", Libretro: "Nintendo - Nintendo Entertainment System",
		Exts: []string{".nes"}, Folders: []string{"nes", "famicom", "fc", "nintendo"}, Header: INES},
	{Key: "snes", Name: "Super Nintendo", Short: "SNES", Maker: "Nintendo", Libretro: "Nintendo - Super Nintendo Entertainment System",
		Exts: []string{".sfc", ".smc", ".swc", ".fig"}, Folders: []string{"snes", "sfc", "superfamicom", "supernes", "supernintendo"}, Header: Copier},
	{Key: "n64", Name: "Nintendo 64", Short: "N64", Maker: "Nintendo", Libretro: "Nintendo - Nintendo 64",
		Exts: []string{".z64", ".n64", ".v64"}, Folders: []string{"n64", "nintendo64"}, Header: N64Order},
	{Key: "gb", Name: "Game Boy", Short: "GB", Maker: "Nintendo", Libretro: "Nintendo - Game Boy",
		Exts: []string{".gb"}, Folders: []string{"gb", "gameboy"}},
	{Key: "gbc", Name: "Game Boy Color", Short: "GBC", Maker: "Nintendo", Libretro: "Nintendo - Game Boy Color",
		Exts: []string{".gbc"}, Folders: []string{"gbc", "gameboycolor"}},
	{Key: "gba", Name: "Game Boy Advance", Short: "GBA", Maker: "Nintendo", Libretro: "Nintendo - Game Boy Advance",
		Exts: []string{".gba"}, Folders: []string{"gba", "gameboyadvance"}},
	{Key: "vb", Name: "Virtual Boy", Short: "VB", Maker: "Nintendo", Libretro: "Nintendo - Virtual Boy",
		Exts: []string{".vb"}, Folders: []string{"vb", "virtualboy"}},
	{Key: "genesis", Name: "Sega Genesis / Mega Drive", Short: "Genesis", Maker: "Sega", Libretro: "Sega - Mega Drive - Genesis",
		Exts: []string{".md", ".gen"}, Folders: []string{"genesis", "megadrive", "md", "segagenesis", "segamegadrive"}},
	{Key: "sms", Name: "Sega Master System", Short: "SMS", Maker: "Sega", Libretro: "Sega - Master System - Mark III",
		Exts: []string{".sms"}, Folders: []string{"sms", "mastersystem", "segamastersystem", "markiii"}},
	{Key: "gamegear", Name: "Game Gear", Short: "GG", Maker: "Sega", Libretro: "Sega - Game Gear",
		Exts: []string{".gg"}, Folders: []string{"gamegear", "gg", "segagamegear"}},
	{Key: "pce", Name: "PC Engine / TurboGrafx-16", Short: "PCE", Maker: "NEC", Libretro: "NEC - PC Engine - TurboGrafx 16",
		Exts: []string{".pce"}, Folders: []string{"pce", "pcengine", "tg16", "turbografx", "turbografx16"}},
	{Key: "atari2600", Name: "Atari 2600", Short: "2600", Maker: "Atari", Libretro: "Atari - 2600",
		Exts: []string{".a26"}, Folders: []string{"atari2600", "a2600", "2600", "vcs"}},
	{Key: "atari7800", Name: "Atari 7800", Short: "7800", Maker: "Atari", Libretro: "Atari - 7800",
		Exts: []string{".a78"}, Folders: []string{"atari7800", "a7800", "7800"}, Header: A7800},
	{Key: "lynx", Name: "Atari Lynx", Short: "Lynx", Maker: "Atari", Libretro: "Atari - Lynx",
		Exts: []string{".lnx"}, Folders: []string{"lynx", "atarilynx"}, Header: Lynx},
	{Key: "ngp", Name: "Neo Geo Pocket", Short: "NGP", Maker: "SNK", Libretro: "SNK - Neo Geo Pocket",
		Exts: []string{".ngp"}, Folders: []string{"ngp", "neogeopocket"}},
	{Key: "ngpc", Name: "Neo Geo Pocket Color", Short: "NGPC", Maker: "SNK", Libretro: "SNK - Neo Geo Pocket Color",
		Exts: []string{".ngc"}, Folders: []string{"ngpc", "neogeopocketcolor"}},
	{Key: "wonderswan", Name: "WonderSwan", Short: "WS", Maker: "Bandai", Libretro: "Bandai - WonderSwan",
		Exts: []string{".ws"}, Folders: []string{"ws", "wonderswan"}},
	{Key: "wonderswancolor", Name: "WonderSwan Color", Short: "WSC", Maker: "Bandai", Libretro: "Bandai - WonderSwan Color",
		Exts: []string{".wsc"}, Folders: []string{"wsc", "wonderswancolor"}},
	{Key: "32x", Name: "Sega 32X", Short: "32X", Maker: "Sega", Libretro: "Sega - 32X",
		Exts: []string{".32x"}, Folders: []string{"32x", "sega32x"}},
	{Key: "atari5200", Name: "Atari 5200", Short: "5200", Maker: "Atari", Libretro: "Atari - 5200",
		Exts: []string{".a52"}, Folders: []string{"atari5200", "a5200", "5200"}},
	{Key: "jaguar", Name: "Atari Jaguar", Short: "Jaguar", Maker: "Atari", Libretro: "Atari - Jaguar",
		Exts: []string{".j64", ".jag"}, Folders: []string{"jaguar", "atarijaguar"}},
	{Key: "colecovision", Name: "ColecoVision", Short: "Coleco", Maker: "Coleco", Libretro: "Coleco - ColecoVision",
		Exts: []string{".col"}, Folders: []string{"colecovision", "coleco"}},
	{Key: "intellivision", Name: "Intellivision", Short: "Intv", Maker: "Mattel", Libretro: "Mattel - Intellivision",
		Exts: []string{".int"}, Folders: []string{"intellivision", "intv"}},
	{Key: "c64", Name: "Commodore 64", Short: "C64", Maker: "Commodore", Libretro: "Commodore - 64",
		Exts: []string{".d64", ".t64", ".prg", ".crt", ".g64"}, Folders: []string{"c64", "commodore64"}},
	{Key: "amiga", Name: "Amiga", Short: "Amiga", Maker: "Commodore", Libretro: "Commodore - Amiga",
		Exts: []string{".adf", ".ipf"}, Folders: []string{"amiga", "commodoreamiga"}},
	{Key: "zxspectrum", Name: "ZX Spectrum", Short: "ZX", Maker: "Sinclair", Libretro: "Sinclair - ZX Spectrum +3",
		Exts: []string{".tzx", ".z80", ".sna"}, Folders: []string{"zxspectrum", "spectrum", "zx"}},
	{Key: "msx", Name: "MSX", Short: "MSX", Maker: "Microsoft", Libretro: "Microsoft - MSX",
		Exts: []string{".mx1", ".mx2"}, Folders: []string{"msx", "msx1", "msx2"}},
	{Key: "nds", Name: "Nintendo DS", Short: "DS", Maker: "Nintendo", Libretro: "Nintendo - Nintendo DS",
		Exts: []string{".nds"}, Folders: []string{"nds", "ds", "nintendods"}},
	{Key: "3ds", Name: "Nintendo 3DS", Short: "3DS", Maker: "Nintendo", Libretro: "Nintendo - Nintendo 3DS",
		Exts: []string{".3ds", ".cia"}, Folders: []string{"3ds", "n3ds", "nintendo3ds"}},
	// Discs, which No-Intro doesn't list: told by name. A .bin is a track
	// listed in a .cue, not a game of its own.
	{Key: "psx", Name: "PlayStation", Short: "PS1", Maker: "Sony", Libretro: "Sony - PlayStation", Disc: true,
		Exts: []string{".cue", ".chd", ".pbp", ".m3u"}, Folders: []string{"psx", "ps1", "playstation", "psone", "sonyplaystation"}},
	{Key: "segacd", Name: "Sega CD / Mega-CD", Short: "Sega CD", Maker: "Sega", Libretro: "Sega - Mega-CD - Sega CD", Disc: true,
		Folders: []string{"segacd", "megacd", "segamegacd"}},
	{Key: "saturn", Name: "Sega Saturn", Short: "Saturn", Maker: "Sega", Libretro: "Sega - Saturn", Disc: true,
		Folders: []string{"saturn", "segasaturn"}},
	{Key: "dreamcast", Name: "Dreamcast", Short: "DC", Maker: "Sega", Libretro: "Sega - Dreamcast", Disc: true,
		Exts: []string{".gdi", ".cdi"}, Folders: []string{"dreamcast", "dc", "segadreamcast"}},
	{Key: "ps2", Name: "PlayStation 2", Short: "PS2", Maker: "Sony", Libretro: "Sony - PlayStation 2", Disc: true,
		Folders: []string{"ps2", "playstation2", "sonyplaystation2"}},
	{Key: "psp", Name: "PlayStation Portable", Short: "PSP", Maker: "Sony", Libretro: "Sony - PlayStation Portable", Disc: true,
		Exts: []string{".cso"}, Folders: []string{"psp", "playstationportable"}},
	{Key: "gamecube", Name: "GameCube", Short: "GC", Maker: "Nintendo", Libretro: "Nintendo - GameCube", Disc: true,
		Exts: []string{".gcm", ".rvz"}, Folders: []string{"gamecube", "gc", "ngc", "nintendogamecube"}},
	{Key: "wii", Name: "Wii", Short: "Wii", Maker: "Nintendo", Libretro: "Nintendo - Wii", Disc: true,
		Exts: []string{".wbfs"}, Folders: []string{"wii", "nintendowii"}},
	{Key: "switch", Name: "Nintendo Switch", Short: "Switch", Maker: "Nintendo", Disc: true,
		Exts: []string{".nsp", ".xci"}, Folders: []string{"switch", "nintendoswitch", "ns"}},
	{Key: "pc", Name: "PC", Short: "PC", Maker: "PC", Libretro: "DOS", Disc: true,
		Folders: []string{"pc", "dos", "msdos", "windows"}},
}

// LookupSystem finds a system by key.
func LookupSystem(key string) (System, bool) {
	i := slices.IndexFunc(Systems, func(s System) bool { return s.Key == key })
	if i < 0 {
		return System{}, false
	}
	return Systems[i], true
}

// fold makes a folder name comparable: lower case letters and digits only,
// so "Super Nintendo", "super-nintendo" and "SuperNintendo" are one.
func fold(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// SystemByFolder finds the system a folder's name says it's for: its key,
// its names, libretro's name for it, or a usual folder name.
func SystemByFolder(name string) (System, bool) {
	f := fold(name)
	if f == "" {
		return System{}, false
	}
	for _, s := range Systems {
		if f == s.Key || f == fold(s.Name) || f == fold(s.Short) || f == fold(s.Libretro) || slices.Contains(s.Folders, f) {
			// "roms" alone says nothing but that it holds games.
			if f == "roms" {
				continue
			}
			return s, true
		}
	}
	return System{}, false
}

// SystemByExt finds the console a game file's extension is for.
func SystemByExt(name string) (System, bool) {
	ext := strings.ToLower(path.Ext(name))
	for _, s := range Systems {
		if slices.Contains(s.Exts, ext) {
			return s, true
		}
	}
	return System{}, false
}

// IsGameFile says whether a file looks like a game: a known console's
// file, or an archive.
func IsGameFile(name string) bool {
	if IsArchive(name) {
		return true
	}
	_, ok := SystemByExt(name)
	return ok
}
