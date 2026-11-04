package stream

import (
	"bufio"
	"bytes"
	"crypto/sha1"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
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

// padHours is when the line added to the end of a subtitle file comes.
const padHours = 12

// paddedSubtitles is a copy of a picture subtitle file with one more, empty
// line hours in, kept in the transcode cache, or the file itself when it
// can't be made. FFmpeg 7 and later read a small subtitle file to its end
// before the video's first frame and then lose that end, waiting for more
// lines forever once the video passes the last one. A line past the end of
// any video means it never has to wait.
func (t *Transcoder) paddedSubtitles(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return path
	}
	sum := sha1.Sum([]byte(fmt.Sprintf("%s|%d|%d", path, info.Size(), info.ModTime().UnixNano())))
	dir := filepath.Join(t.Dir, "subtitle-files")
	out := filepath.Join(dir, hex.EncodeToString(sum[:10])+strings.ToLower(filepath.Ext(path)))
	if _, err := os.Stat(out); err == nil {
		return out
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return path
	}
	if err := padSubtitleFile(path, out); err != nil {
		t.Log.Warn("couldn't prepare a subtitle file; it may stop the video early", "file", path, "err", err)
		os.Remove(out)
		return path
	}
	return out
}

func padSubtitleFile(path, out string) error {
	src, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var add []byte
	switch strings.ToLower(filepath.Ext(path)) {
	case ".sup":
		add, err = pgsClear(src, padHours*3600*90000)
	case ".idx":
		// The last line again, hours in; the .sub beside the copy is the original.
		pos := regexp.MustCompile(`(?m)^timestamp:.*filepos:\s*([0-9a-fA-F]+)`).FindAllSubmatch(src, -1)
		if len(pos) == 0 {
			return errors.New("no lines")
		}
		add = []byte(fmt.Sprintf("\ntimestamp: %02d:00:00:000, filepos: %s\n", padHours, pos[len(pos)-1][1]))
		if err := linkOrCopy(strings.TrimSuffix(path, filepath.Ext(path))+".sub", strings.TrimSuffix(out, filepath.Ext(out))+".sub"); err != nil {
			return err
		}
	default:
		return errors.New("not a picture subtitle file")
	}
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(out), "pad-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	_, err = tmp.Write(append(src, add...))
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	return os.Rename(tmp.Name(), out)
}

// pgsClear is a display set that clears the screen at pts, in the picture
// size of the file's first one.
func pgsClear(sup []byte, pts uint32) ([]byte, error) {
	var size []byte
	for i := 0; i+13 <= len(sup) && sup[i] == 'P' && sup[i+1] == 'G'; {
		n := int(binary.BigEndian.Uint16(sup[i+11:]))
		if sup[i+10] == 0x16 && n >= 4 && i+13+4 <= len(sup) {
			size = sup[i+13 : i+17]
			break
		}
		i += 13 + n
	}
	if size == nil {
		return nil, errors.New("not a PGS file")
	}
	var b bytes.Buffer
	seg := func(typ byte, data []byte) {
		b.WriteString("PG")
		binary.Write(&b, binary.BigEndian, pts)
		binary.Write(&b, binary.BigEndian, uint32(0))
		b.WriteByte(typ)
		binary.Write(&b, binary.BigEndian, uint16(len(data)))
		b.Write(data)
	}
	// Composition: the size, 0x10 for its frame rate, number 0xffff, a
	// normal case, no palette update, and no objects.
	seg(0x16, append(append([]byte{}, size...), 0x10, 0xff, 0xff, 0, 0, 0, 0))
	seg(0x80, nil)
	return b.Bytes(), nil
}

func linkOrCopy(from, to string) error {
	os.Remove(to)
	if err := os.Link(from, to); err == nil {
		return nil
	}
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	f, err := os.Create(to)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, in); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
