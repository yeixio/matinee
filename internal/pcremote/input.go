package pcremote

import (
	"encoding/binary"
)

// The player's input comes as the game stream's messages (see
// gamestream.Session.input, and gamehost's kbm.go for 4 to 8): libretro's
// button bits and a signed byte per stick axis for each controller, Linux
// key codes, and the mouse. input turns them into what matinee-pcremote
// takes, as Moonlight sends them: Xbox-style controllers, Windows
// virtual-key codes.

// libretro's joypad bits.
const (
	rB = iota
	rY
	rSelect
	rStart
	rUp
	rDown
	rLeft
	rRight
	rA
	rX
	rL
	rR
	rL2
	rR2
	rL3
	rR3
)

// Moonlight's controller buttons.
const (
	mlUp      = 0x0001
	mlDown    = 0x0002
	mlLeft    = 0x0004
	mlRight   = 0x0008
	mlPlay    = 0x0010
	mlBack    = 0x0020
	mlLSClick = 0x0040
	mlRSClick = 0x0080
	mlLB      = 0x0100
	mlRB      = 0x0200
	mlSpecial = 0x0400
	mlA       = 0x1000
	mlB       = 0x2000
	mlX       = 0x4000
	mlY       = 0x8000
)

// buttonMap is where each libretro bit goes: its B is the bottom face
// button (A on an Xbox pad), A the right (B), Y the left (X), X the top (Y).
var buttonMap = map[int]uint32{
	rB: mlA, rA: mlB, rY: mlX, rX: mlY,
	rUp: mlUp, rDown: mlDown, rLeft: mlLeft, rRight: mlRight,
	rL: mlLB, rR: mlRB, rL3: mlLSClick, rR3: mlRSClick,
	rSelect: mlBack, rStart: mlPlay,
}

// guideCombo, Select and Start held together, is the Xbox button, which
// opens Steam's menu: most controllers don't pass theirs on to apps. The
// game host does the same.
const guideCombo = 1<<rSelect | 1<<rStart

const maxPads = 4

type padState struct {
	buttons uint16
	sticks  [4]int8
}

// Moonlight's modifier keys.
const (
	modShift = 0x01
	modCtrl  = 0x02
	modAlt   = 0x04
	modMeta  = 0x08
)

type msg struct {
	typ byte
	b   []byte
}

type input struct {
	pads    [maxPads]padState
	present uint16
	// held is the keys (by virtual-key code) and mouse buttons held, to
	// let go of at the end.
	keys    map[uint16]bool
	buttons map[byte]bool
	// width and height are the picture's, which pointer spots are on.
	width, height int
}

func newInput(width, height int) input {
	return input{keys: map[uint16]bool{}, buttons: map[byte]bool{}, width: width, height: height}
}

func le16(b []byte) int     { return int(b[0]) | int(b[1])<<8 }
func s16(b []byte) int16    { return int16(uint16(b[0]) | uint16(b[1])<<8) }
func put16(b []byte, v int) { binary.LittleEndian.PutUint16(b, uint16(v)) }

// apply turns one message into the bridge's.
func (in *input) apply(b []byte) []msg {
	if len(b) == 0 {
		return nil
	}
	switch b[0] {
	case 1, 3:
		if len(b) < 4 || int(b[1]) >= maxPads {
			return nil
		}
		port := int(b[1])
		p := &in.pads[port]
		if b[0] == 1 {
			m := uint16(b[2]) | uint16(b[3])<<8
			if m == p.buttons && in.present&(1<<port) != 0 {
				return nil
			}
			p.buttons = m
		} else {
			axis := int(b[2])
			if axis > 3 || p.sticks[axis] == int8(b[3]) && in.present&(1<<port) != 0 {
				return nil
			}
			p.sticks[axis] = int8(b[3])
		}
		in.present |= 1 << port
		return []msg{{'C', in.padMsg(port)}}
	case 4:
		if len(b) < 4 {
			return nil
		}
		vk, ok := linuxToVK[uint16(le16(b[1:]))]
		if !ok {
			return nil
		}
		down := b[3] != 0
		if in.keys[vk] == down {
			return nil
		}
		in.keys[vk] = down
		return []msg{in.keyMsg(vk, down)}
	case 5:
		if len(b) < 5 || in.width <= 0 || in.height <= 0 {
			return nil
		}
		// 0 to 65535 across the picture, onto its pixels.
		x := le16(b[1:]) * in.width / 65536
		y := le16(b[3:]) * in.height / 65536
		out := make([]byte, 8)
		put16(out, x)
		put16(out[2:], y)
		put16(out[4:], in.width)
		put16(out[6:], in.height)
		return []msg{{'p', out}}
	case 6:
		if len(b) < 5 {
			return nil
		}
		dx, dy := s16(b[1:]), s16(b[3:])
		if dx == 0 && dy == 0 {
			return nil
		}
		out := make([]byte, 4)
		put16(out, int(dx))
		put16(out[2:], int(dy))
		return []msg{{'m', out}}
	case 7:
		if len(b) < 3 || b[1] > 4 {
			return nil
		}
		// The browser's order (left, middle, right, back, forward) is
		// Moonlight's, from 1.
		button, down := b[1]+1, b[2] != 0
		if in.buttons[button] == down {
			return nil
		}
		in.buttons[button] = down
		return []msg{{'b', []byte{button, b[2] & 1}}}
	case 8:
		if len(b) < 3 || b[1] == 0 && b[2] == 0 {
			return nil
		}
		return []msg{{'w', []byte{b[1], b[2]}}}
	}
	return nil
}

// modifiers is the modifier keys held.
func (in *input) modifiers() byte {
	var m byte
	for vk, down := range in.keys {
		if !down {
			continue
		}
		switch vk {
		case vkLShift, vkRShift:
			m |= modShift
		case vkLControl, vkRControl:
			m |= modCtrl
		case vkLMenu, vkRMenu:
			m |= modAlt
		case vkLWin, vkRWin:
			m |= modMeta
		}
	}
	return m
}

func (in *input) keyMsg(vk uint16, down bool) msg {
	out := make([]byte, 4)
	// The top bit marks a virtual-key code, as Moonlight sends it.
	put16(out, int(0x8000|vk))
	if down {
		out[2] = 1
	}
	out[3] = in.modifiers()
	return msg{'k', out}
}

// padMsg is controller port's state: number, controllers present,
// buttons, triggers, then the sticks lx ly rx ry.
func (in *input) padMsg(port int) []byte {
	p := in.pads[port]
	var buttons uint32
	m := p.buttons
	if m&guideCombo == guideCombo {
		buttons |= mlSpecial
		m &^= guideCombo
	}
	for bit, ml := range buttonMap {
		if m&(1<<bit) != 0 {
			buttons |= ml
		}
	}
	out := make([]byte, 17)
	out[0] = byte(port)
	binary.LittleEndian.PutUint16(out[1:], in.present)
	binary.LittleEndian.PutUint32(out[3:], buttons)
	if m&(1<<rL2) != 0 {
		out[7] = 0xff
	}
	if m&(1<<rR2) != 0 {
		out[8] = 0xff
	}
	for i, v := range p.sticks {
		val := int(v) * 256
		if v == 127 {
			val = 32767
		}
		// The game stream's sticks point down for positive y, Xbox
		// pads' up.
		if i%2 == 1 {
			val = -val
			if val > 32767 {
				val = 32767
			}
		}
		put16(out[9+2*i:], val)
	}
	return out
}

// release lets go of every key and button held, and centres every pad.
func (in *input) release() []msg {
	var out []msg
	for vk, down := range in.keys {
		if down {
			in.keys[vk] = false
			out = append(out, in.keyMsg(vk, false))
		}
	}
	for button, down := range in.buttons {
		if down {
			in.buttons[button] = false
			out = append(out, msg{'b', []byte{button, 0}})
		}
	}
	for port := range maxPads {
		if in.present&(1<<port) != 0 && in.pads[port] != (padState{}) {
			in.pads[port] = padState{}
			out = append(out, msg{'C', in.padMsg(port)})
		}
	}
	return out
}
