package xboxremote

import (
	"encoding/binary"
	"math"
	"time"
)

// The player's controller comes as the game stream's input messages:
// libretro's button bits (B=0 … R3=15, see gamestream.Runner.SetButtons)
// and a signed byte per stick axis, down and right positive. pad turns
// them into the Xbox's input reports.

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

// The Xbox's buttons.
const (
	xNexus  = 1 << 1
	xMenu   = 1 << 2
	xView   = 1 << 3
	xA      = 1 << 4
	xB      = 1 << 5
	xX      = 1 << 6
	xY      = 1 << 7
	xUp     = 1 << 8
	xDown   = 1 << 9
	xLeft   = 1 << 10
	xRight  = 1 << 11
	xLB     = 1 << 12
	xRB     = 1 << 13
	xLStick = 1 << 14
	xRStick = 1 << 15
)

// buttonMap is where each libretro bit goes: its B is the bottom face
// button (A on an Xbox pad), A the right (B), Y the left (X), X the top
// (Y), wherever they're printed on the player's controller.
var buttonMap = map[int]uint16{
	rB: xA, rA: xB, rY: xX, rX: xY,
	rUp: xUp, rDown: xDown, rLeft: xLeft, rRight: xRight,
	rL: xLB, rR: xRB, rL3: xLStick, rR3: xRStick,
	rSelect: xView, rStart: xMenu,
}

// homeCombo, Select and Start held together, is the Xbox button, which
// most controllers don't send on to apps.
const homeCombo = 1<<rSelect | 1<<rStart

type pad struct {
	buttons uint16
	sticks  [4]int8
}

// apply takes one input message, and says whether it changed the first
// controller.
func (p *pad) apply(b []byte) bool {
	if len(b) < 4 || b[1] != 0 {
		return false
	}
	switch b[0] {
	case 1:
		m := uint16(b[2]) | uint16(b[3])<<8
		if m == p.buttons {
			return false
		}
		p.buttons = m
	case 3:
		axis := int(b[2])
		if axis < 0 || axis > 3 || p.sticks[axis] == int8(b[3]) {
			return false
		}
		p.sticks[axis] = int8(b[3])
	default:
		return false
	}
	return true
}

// axis is a stick axis as the Xbox takes it, full scale; up is positive
// for the Xbox, so flip says to turn it over.
func axis(v int8, flip bool) int16 {
	f := float64(v) / 127
	if flip {
		f = -f
	}
	return int16(math.Max(-32767, math.Min(32767, math.Round(f*32767))))
}

// frame is the controller as one gamepad frame: 23 bytes.
func (p *pad) frame() []byte {
	out := make([]byte, 23)
	m := p.buttons
	var buttons uint16
	if m&homeCombo == homeCombo {
		buttons |= xNexus
		m &^= homeCombo
	}
	for bit, x := range buttonMap {
		if m&(1<<bit) != 0 {
			buttons |= x
		}
	}
	out[0] = 0 // the first controller
	binary.LittleEndian.PutUint16(out[1:], buttons)
	binary.LittleEndian.PutUint16(out[3:], uint16(axis(p.sticks[0], false)))
	binary.LittleEndian.PutUint16(out[5:], uint16(axis(p.sticks[1], true)))
	binary.LittleEndian.PutUint16(out[7:], uint16(axis(p.sticks[2], false)))
	binary.LittleEndian.PutUint16(out[9:], uint16(axis(p.sticks[3], true)))
	if m&(1<<rL2) != 0 {
		binary.LittleEndian.PutUint16(out[11:], 0xffff)
	}
	if m&(1<<rR2) != 0 {
		binary.LittleEndian.PutUint16(out[13:], 0xffff)
	}
	binary.LittleEndian.PutUint32(out[15:], 1) // physicality, as xbox.com sends it
	binary.BigEndian.PutUint32(out[19:], 1)
	return out
}

// Input report kinds.
const (
	reportMetadata       = 1
	reportGamepad        = 2
	reportClientMetadata = 8
	reportServerMetadata = 16
)

// frameTimes is one picture's metadata frame: when it came and was shown,
// which the console uses to tune the stream.
type frameTimes struct {
	key     uint32 // its RTP timestamp
	arrived uint32 // ms
}

// report is one input report: a header (kinds, sequence number, time),
// then the metadata frames and the gamepad frame, if any.
func report(seq uint32, at time.Duration, frames []frameTimes, gamepad []byte) []byte {
	kind := uint16(0)
	size := 14
	if len(frames) > 0 {
		kind |= reportMetadata
		size += 1 + 28*len(frames)
	}
	if gamepad != nil {
		kind |= reportGamepad
		size += 1 + len(gamepad)
	}
	out := make([]byte, size)
	binary.LittleEndian.PutUint16(out, kind)
	binary.LittleEndian.PutUint32(out[2:], seq)
	ms := float64(at) / float64(time.Millisecond)
	binary.LittleEndian.PutUint64(out[6:], math.Float64bits(ms))
	o := 14
	if len(frames) > 0 {
		out[o] = byte(len(frames))
		o++
		now := uint32(ms)
		for _, f := range frames {
			for i, v := range []uint32{f.key, f.arrived, f.arrived, now, now, now, now} {
				binary.LittleEndian.PutUint32(out[o+4*i:], v)
			}
			o += 28
		}
	}
	if gamepad != nil {
		out[o] = 1
		copy(out[o+1:], gamepad)
	}
	return out
}

// clientMetadata is the first report: how many touch points the player
// has (one, as touch isn't sent on).
func clientMetadata(at time.Duration) []byte {
	out := make([]byte, 15)
	binary.LittleEndian.PutUint16(out, reportClientMetadata)
	binary.LittleEndian.PutUint64(out[6:], math.Float64bits(float64(at)/float64(time.Millisecond)))
	out[14] = 1
	return out
}
