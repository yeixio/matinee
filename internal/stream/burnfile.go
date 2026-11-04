package stream

import (
	"bufio"
	"encoding/binary"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var idxTimestamp = regexp.MustCompile(`^timestamp:\s*(\d+):(\d+):(\d+):(\d+)`)

// subtitleFileStart is when a picture subtitle file's first line is, in
// seconds. FFmpeg counts each input from its first moment, so a file whose
// first line comes three seconds in would otherwise show it at once.
func subtitleFileStart(path string) float64 {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()
	switch strings.ToLower(filepath.Ext(path)) {
	case ".sup":
		// Every PGS segment starts with "PG" and its time at 90 kHz.
		var h [6]byte
		if _, err := f.Read(h[:]); err != nil || h[0] != 'P' || h[1] != 'G' {
			return 0
		}
		return float64(binary.BigEndian.Uint32(h[2:])) / 90000
	case ".idx":
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			if m := idxTimestamp.FindStringSubmatch(strings.TrimSpace(sc.Text())); m != nil {
				var n [4]float64
				for i := range n {
					n[i], _ = strconv.ParseFloat(m[i+1], 64)
				}
				return n[0]*3600 + n[1]*60 + n[2] + n[3]/1000
			}
		}
	}
	return 0
}
