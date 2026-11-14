package gamehost

// The player's keyboard and mouse, for PC games and the Steam desktop:
// keys come as Linux key codes, the pointer as a spot on the picture or as
// a move (when the player has captured the mouse), and become events on a
// virtual keyboard and mouse.
//
// Messages, after the kind byte:
//
//	4 code(lo hi) down       a key, by its Linux code (1 to 248)
//	5 x(lo hi) y(lo hi)      the pointer to a spot, 0 to 65535 across the picture
//	6 dx(lo hi) dy(lo hi)    the pointer moved by signed pixels
//	7 button down            a mouse button: 0 left, 1 middle, 2 right, 3 back, 4 forward
//	8 dy dx                  the wheel turned, in signed notches (up and right are positive)

const (
	evRel = 0x02

	relX      = 0x00
	relY      = 0x01
	relHWheel = 0x06
	relWheel  = 0x08

	btnLeft    = 0x110
	btnRight   = 0x111
	btnMiddle  = 0x112
	btnSide    = 0x113
	btnExtra   = 0x114
	maxKeyCode = 248

	// The pointer's spot runs from 0 to this across the picture.
	pointerMax = 65535
)

// mouseButtons are the buttons by the message's numbers, in the browser's
// order.
var mouseButtons = []uint16{btnLeft, btnMiddle, btnRight, btnSide, btnExtra}

// kbmKind says which device an input message is for.
type kbmKind int

const (
	kbmNone    kbmKind = iota
	kbmKeys            // the keyboard and relative mouse
	kbmPointer         // the absolute pointer
)

// kbmEvents turns one message into events, and the device they're for.
// Keys held are tracked in held, so a key isn't pressed twice and all can
// be let go at the end.
func kbmEvents(b []byte, held map[uint16]bool) (kbmKind, []event) {
	u16 := func(i int) int { return int(b[i]) | int(b[i+1])<<8 }
	s16 := func(i int) int32 { return int32(int16(uint16(b[i]) | uint16(b[i+1])<<8)) }
	press := func(code uint16, down bool) []event {
		if held[code] == down {
			return nil
		}
		held[code] = down
		v := int32(0)
		if down {
			v = 1
		}
		return []event{{evKey, code, v}, {evSyn, 0, 0}}
	}
	switch b[0] {
	case 4:
		if len(b) < 4 {
			return kbmNone, nil
		}
		code := u16(1)
		if code < 1 || code > maxKeyCode {
			return kbmNone, nil
		}
		return kbmKeys, press(uint16(code), b[3] != 0)
	case 5:
		if len(b) < 5 {
			return kbmNone, nil
		}
		return kbmPointer, []event{{evAbs, absX, int32(u16(1))}, {evAbs, absY, int32(u16(3))}, {evSyn, 0, 0}}
	case 6:
		if len(b) < 5 {
			return kbmNone, nil
		}
		var out []event
		if dx := s16(1); dx != 0 {
			out = append(out, event{evRel, relX, dx})
		}
		if dy := s16(3); dy != 0 {
			out = append(out, event{evRel, relY, dy})
		}
		if len(out) == 0 {
			return kbmNone, nil
		}
		return kbmKeys, append(out, event{evSyn, 0, 0})
	case 7:
		if len(b) < 3 || int(b[1]) >= len(mouseButtons) {
			return kbmNone, nil
		}
		return kbmKeys, press(mouseButtons[b[1]], b[2] != 0)
	case 8:
		if len(b) < 3 {
			return kbmNone, nil
		}
		var out []event
		if dy := int32(int8(b[1])); dy != 0 {
			out = append(out, event{evRel, relWheel, dy})
		}
		if dx := int32(int8(b[2])); dx != 0 {
			out = append(out, event{evRel, relHWheel, dx})
		}
		if len(out) == 0 {
			return kbmNone, nil
		}
		return kbmKeys, append(out, event{evSyn, 0, 0})
	}
	return kbmNone, nil
}

// releaseAll is the events that let go of every key and button held.
func releaseAll(held map[uint16]bool) []event {
	var out []event
	for code, down := range held {
		if down {
			out = append(out, event{evKey, code, 0})
			held[code] = false
		}
	}
	if len(out) > 0 {
		out = append(out, event{evSyn, 0, 0})
	}
	return out
}

// keyboardSpec is the virtual keyboard and mouse: every keyboard key,
// mouse buttons, moves and the wheel.
func keyboardSpec() deviceSpec {
	keys := make([]uint16, 0, maxKeyCode+len(mouseButtons))
	for k := uint16(1); k <= maxKeyCode; k++ {
		keys = append(keys, k)
	}
	keys = append(keys, mouseButtons...)
	return deviceSpec{
		Name: "Matinee Keyboard and Mouse",
		// USB, a made-up vendor of our own.
		Bus: 0x03, Vendor: 0x1209, Product: 0x4d4b, Version: 1,
		Keys: keys,
		Rels: []uint16{relX, relY, relWheel, relHWheel},
	}
}

// pointerSpec is the virtual absolute pointer, as a virtual machine's
// tablet is: it puts the pointer where the player points on the picture.
// Its left button is never pressed (clicks come from the mouse, at the
// same pointer), but makes the desktop treat it as a pointer.
func pointerSpec() deviceSpec {
	return deviceSpec{
		Name: "Matinee Pointer",
		Bus:  0x03, Vendor: 0x1209, Product: 0x4d50, Version: 1,
		Keys: []uint16{btnLeft},
		Abs:  []axisRange{{absX, 0, pointerMax, 0}, {absY, 0, pointerMax, 0}},
	}
}
