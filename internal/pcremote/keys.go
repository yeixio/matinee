package pcremote

// Windows virtual-key codes, which Moonlight sends keys as.
const (
	vkLShift   = 0xA0
	vkRShift   = 0xA1
	vkLControl = 0xA2
	vkRControl = 0xA3
	vkLMenu    = 0xA4
	vkRMenu    = 0xA5
	vkLWin     = 0x5B
	vkRWin     = 0x5C
)

// linuxToVK is the virtual-key code of each Linux key code the player
// sends (linux/input-event-codes.h).
var linuxToVK = func() map[uint16]uint16 {
	m := map[uint16]uint16{
		1:  0x1B,                               // Esc
		12: 0xBD, 13: 0xBB, 14: 0x08, 15: 0x09, // - = Backspace Tab
		26: 0xDB, 27: 0xDD, 28: 0x0D, 29: vkLControl, // [ ] Enter
		39: 0xBA, 40: 0xDE, 41: 0xC0, 42: vkLShift, 43: 0xDC, // ; ' ` \
		51: 0xBC, 52: 0xBE, 53: 0xBF, 54: vkRShift, 55: 0x6A, // , . / keypad *
		56: vkLMenu, 57: 0x20, 58: 0x14, // Space CapsLock
		69: 0x90, 70: 0x91, // NumLock ScrollLock
		71: 0x67, 72: 0x68, 73: 0x69, 74: 0x6D, // keypad 7 8 9 -
		75: 0x64, 76: 0x65, 77: 0x66, 78: 0x6B, // keypad 4 5 6 +
		79: 0x61, 80: 0x62, 81: 0x63, 82: 0x60, 83: 0x6E, // keypad 1 2 3 0 .
		86: 0xE2, 87: 0x7A, 88: 0x7B, // 102nd F11 F12
		96: 0x0D, 97: vkRControl, 98: 0x6F, 99: 0x2C, 100: vkRMenu, // keypad Enter, keypad /, PrintScreen
		102: 0x24, 103: 0x26, 104: 0x21, 105: 0x25, 106: 0x27, // Home Up PageUp Left Right
		107: 0x23, 108: 0x28, 109: 0x22, 110: 0x2D, 111: 0x2E, // End Down PageDown Insert Delete
		113: 0xAD, 114: 0xAE, 115: 0xAF, 119: 0x13, // Mute VolumeDown VolumeUp Pause
		125: vkLWin, 126: vkRWin, 127: 0x5D, // Menu
	}
	// 1 to 9, then 0.
	for i, code := range []uint16{2, 3, 4, 5, 6, 7, 8, 9, 10} {
		m[code] = uint16('1' + i)
	}
	m[11] = '0'
	// The letters, row by row.
	for _, row := range []struct {
		first   uint16
		letters string
	}{{16, "QWERTYUIOP"}, {30, "ASDFGHJKL"}, {44, "ZXCVBNM"}} {
		for i, c := range row.letters {
			m[row.first+uint16(i)] = uint16(c)
		}
	}
	// F1 to F10.
	for i := uint16(0); i < 10; i++ {
		m[59+i] = 0x70 + i
	}
	return m
}()
