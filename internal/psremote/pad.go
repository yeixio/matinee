package psremote

import "encoding/binary"

// The player's controller comes as the game stream's input messages:
// libretro's button bits (B=0 … R3=15, see gamestream.Runner.SetButtons)
// and a signed byte per stick axis. pad turns them into a PlayStation
// controller's state for the bridge.

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

// chiaki-ng's controller buttons.
const (
	psCross    = 1 << 0
	psCircle   = 1 << 1
	psSquare   = 1 << 2
	psTriangle = 1 << 3
	psLeft     = 1 << 4
	psRight    = 1 << 5
	psUp       = 1 << 6
	psDown     = 1 << 7
	psL1       = 1 << 8
	psR1       = 1 << 9
	psL3       = 1 << 10
	psR3       = 1 << 11
	psOptions  = 1 << 12
	psCreate   = 1 << 13
	psTouchpad = 1 << 14
	psHome     = 1 << 15
)

// buttonMap is where each libretro bit goes: its B is the bottom face
// button (Cross), A the right (Circle), Y the left (Square), X the top
// (Triangle), wherever they're printed on the player's controller.
// Select presses the touchpad, which games use far more than Create.
var buttonMap = map[int]uint32{
	rB: psCross, rA: psCircle, rY: psSquare, rX: psTriangle,
	rUp: psUp, rDown: psDown, rLeft: psLeft, rRight: psRight,
	rL: psL1, rR: psR1, rL3: psL3, rR3: psR3,
	rSelect: psTouchpad, rStart: psOptions,
}

// homeCombo, Select and Start held together, is the PS button, which most
// controllers don't send on to apps.
const homeCombo = 1<<rSelect | 1<<rStart

type pad struct {
	buttons uint16
	sticks  [4]int8
}

// apply takes one input message, and returns the controller state to send
// when it changed anything on the first controller.
func (p *pad) apply(b []byte) ([]byte, bool) {
	if len(b) < 4 || b[1] != 0 {
		return nil, false
	}
	switch b[0] {
	case 1:
		m := uint16(b[2]) | uint16(b[3])<<8
		if m == p.buttons {
			return nil, false
		}
		p.buttons = m
	case 3:
		axis := int(b[2])
		if axis < 0 || axis > 3 || p.sticks[axis] == int8(b[3]) {
			return nil, false
		}
		p.sticks[axis] = int8(b[3])
	default:
		return nil, false
	}
	return p.state(), true
}

// state is the controller as the bridge takes it: buttons, L2, R2, then
// the sticks lx ly rx ry, little-endian.
func (p *pad) state() []byte {
	var buttons uint32
	m := p.buttons
	if m&homeCombo == homeCombo {
		buttons |= psHome
		m &^= homeCombo
	}
	for bit, ps := range buttonMap {
		if m&(1<<bit) != 0 {
			buttons |= ps
		}
	}
	out := make([]byte, 14)
	binary.LittleEndian.PutUint32(out, buttons)
	if m&(1<<rL2) != 0 {
		out[4] = 0xff
	}
	if m&(1<<rR2) != 0 {
		out[5] = 0xff
	}
	for i, v := range p.sticks {
		val := int16(v) * 256
		if v == 127 {
			val = 32767
		}
		binary.LittleEndian.PutUint16(out[6+2*i:], uint16(val))
	}
	return out
}
