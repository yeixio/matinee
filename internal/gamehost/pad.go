package gamehost

// The player's buttons and sticks become a virtual Xbox-style gamepad,
// which Steam and almost every PC game know. Buttons come as libretro's
// bits (B=0 … R3=15; see gamestream.Runner.SetButtons) and sticks as a
// signed byte per axis; this turns them into Linux input events.

// Linux input event types and codes (linux/input-event-codes.h).
const (
	evSyn = 0x00
	evKey = 0x01
	evAbs = 0x03

	btnSouth  = 0x130
	btnEast   = 0x131
	btnNorth  = 0x133
	btnWest   = 0x134
	btnTL     = 0x136
	btnTR     = 0x137
	btnSelect = 0x13a
	btnStart  = 0x13b
	btnMode   = 0x13c
	btnThumbL = 0x13d
	btnThumbR = 0x13e

	absX     = 0x00
	absY     = 0x01
	absZ     = 0x02 // left trigger
	absRX    = 0x03
	absRY    = 0x04
	absRZ    = 0x05 // right trigger
	absHat0X = 0x10
	absHat0Y = 0x11
)

// event is one input event: type, code, value.
type event struct {
	Type, Code uint16
	Value      int32
}

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

// buttonCodes are the gamepad buttons libretro's bits press: its B is the
// bottom face button, A the right one, Y the left and X the top, as on an
// Xbox pad.
var buttonCodes = map[int]uint16{
	rB: btnSouth, rA: btnEast, rY: btnWest, rX: btnNorth,
	rL: btnTL, rR: btnTR, rSelect: btnSelect, rStart: btnStart,
	rL3: btnThumbL, rR3: btnThumbR,
}

// padKeys and padAxes are what the virtual pad has.
var padKeys = []uint16{btnSouth, btnEast, btnNorth, btnWest, btnTL, btnTR, btnSelect, btnStart, btnMode, btnThumbL, btnThumbR}

type axisRange struct {
	Code     uint16
	Min, Max int32
	Flat     int32
}

var padAxes = []axisRange{
	{absX, -32768, 32767, 128}, {absY, -32768, 32767, 128},
	{absRX, -32768, 32767, 128}, {absRY, -32768, 32767, 128},
	{absZ, 0, 255, 0}, {absRZ, 0, 255, 0},
	{absHat0X, -1, 1, 0}, {absHat0Y, -1, 1, 0},
}

// padState is what's held on one pad, to send only what changed.
type padState struct {
	buttons uint16
	sticks  [4]int8
}

// Guide is pressed by holding Select and Start together, as PC pads have a
// button for it that console pads don't.
const guideCombo = 1<<rSelect | 1<<rStart

// setButtons is the events that take the pad from what it held to mask.
func (p *padState) setButtons(mask uint16) []event {
	old := p.buttons
	p.buttons = mask
	if old == mask {
		return nil
	}
	var out []event
	for bit := 0; bit < 16; bit++ {
		was, is := old&(1<<bit) != 0, mask&(1<<bit) != 0
		if was == is {
			continue
		}
		v := int32(0)
		if is {
			v = 1
		}
		if code, ok := buttonCodes[bit]; ok {
			out = append(out, event{evKey, code, v})
		}
		switch bit {
		case rL2:
			out = append(out, event{evAbs, absZ, v * 255})
		case rR2:
			out = append(out, event{evAbs, absRZ, v * 255})
		}
	}
	hat := func(neg, pos int, m uint16) int32 {
		switch {
		case m&(1<<neg) != 0 && m&(1<<pos) == 0:
			return -1
		case m&(1<<pos) != 0 && m&(1<<neg) == 0:
			return 1
		}
		return 0
	}
	if hx := hat(rLeft, rRight, mask); hx != hat(rLeft, rRight, old) {
		out = append(out, event{evAbs, absHat0X, hx})
	}
	if hy := hat(rUp, rDown, mask); hy != hat(rUp, rDown, old) {
		out = append(out, event{evAbs, absHat0Y, hy})
	}
	if wasG, isG := old&guideCombo == guideCombo, mask&guideCombo == guideCombo; wasG != isG {
		v := int32(0)
		if isG {
			v = 1
		}
		out = append(out, event{evKey, btnMode, v})
	}
	if len(out) > 0 {
		out = append(out, event{evSyn, 0, 0})
	}
	return out
}

var stickCodes = [4]uint16{absX, absY, absRX, absRY}

// setStick is the events that move a stick: axis 0 and 1 are the left
// stick's x and y, 2 and 3 the right's, from -128 (left, up) to 127.
func (p *padState) setStick(axis int, v int8) []event {
	if axis < 0 || axis > 3 || p.sticks[axis] == v {
		return nil
	}
	p.sticks[axis] = v
	val := int32(v) * 256
	if v == 127 {
		val = 32767
	}
	return []event{{evAbs, stickCodes[axis], val}, {evSyn, 0, 0}}
}
