package mediabridge

import "bytes"

// keyframer makes every keyframe stand on its own: a console sends the
// H.264 SPS and PPS once, at the start, but a player who joins later, or
// whose decoder starts over, needs them with each keyframe.
type keyframer struct {
	sps, pps []byte
}

// nalus splits an Annex B access unit into its NAL units, without their
// start codes.
func nalus(au []byte) [][]byte {
	var out [][]byte
	i := 0
	start := -1
	for i+2 < len(au) {
		if au[i] == 0 && au[i+1] == 0 && au[i+2] == 1 {
			if start >= 0 {
				end := i
				if end > start && au[end-1] == 0 {
					end--
				}
				out = append(out, au[start:end])
			}
			i += 3
			start = i
			continue
		}
		i++
	}
	if start >= 0 && start < len(au) {
		out = append(out, au[start:])
	}
	return out
}

var startCode = []byte{0, 0, 0, 1}

// fix returns au with the SPS and PPS put before its keyframe when it has
// one but not them, and remembers them when it does.
func (k *keyframer) fix(au []byte) []byte {
	idr, hasSPS := false, false
	for _, n := range nalus(au) {
		if len(n) == 0 {
			continue
		}
		switch n[0] & 0x1f {
		case 5:
			idr = true
		case 7:
			hasSPS = true
			k.sps = bytes.Clone(n)
		case 8:
			k.pps = bytes.Clone(n)
		}
	}
	if !idr || hasSPS || k.sps == nil || k.pps == nil {
		return au
	}
	out := make([]byte, 0, len(au)+len(k.sps)+len(k.pps)+8)
	out = append(append(out, startCode...), k.sps...)
	out = append(append(out, startCode...), k.pps...)
	return append(out, au...)
}

// opusSamples is how many 48 kHz samples an Opus packet holds, from its
// first byte (RFC 6716, 3.1): a PlayStation sends 10 ms ones, Sunshine 5 ms.
func opusSamples(p []byte) int {
	if len(p) == 0 {
		return 480
	}
	toc := p[0]
	config := toc >> 3
	var frame int // in 48 kHz samples
	switch {
	case config < 12: // SILK: 10, 20, 40, 60 ms
		frame = []int{480, 960, 1920, 2880}[config%4]
	case config < 16: // hybrid: 10, 20 ms
		frame = []int{480, 960}[config%2]
	default: // CELT: 2.5, 5, 10, 20 ms
		frame = []int{120, 240, 480, 960}[config%4]
	}
	switch toc & 3 {
	case 0:
		return frame
	case 1, 2:
		return 2 * frame
	default:
		if len(p) < 2 {
			return frame
		}
		return int(p[1]&0x3f) * frame
	}
}
