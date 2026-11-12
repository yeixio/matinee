//go:build linux

package gamehost

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// uinput's ioctls (linux/uinput.h).
const (
	uiDevCreate  = 0x5501
	uiDevDestroy = 0x5502
	uiSetEvBit   = 0x40045564
	uiSetKeyBit  = 0x40045565
	uiSetAbsBit  = 0x40045567
)

// pad is one virtual gamepad made with /dev/uinput.
type pad struct {
	f     *os.File
	state padState
}

// newPad makes a virtual Xbox 360 pad, which Steam Input knows.
func newPad(n int) (*pad, error) {
	f, err := os.OpenFile("/dev/uinput", os.O_WRONLY|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, fmt.Errorf("can't make a virtual gamepad: %w", err)
	}
	fd := int(f.Fd())
	set := func(req uint, v int) error { return unix.IoctlSetInt(fd, req, v) }
	for _, err := range []error{set(uiSetEvBit, evKey), set(uiSetEvBit, evAbs), set(uiSetEvBit, evSyn)} {
		if err != nil {
			f.Close()
			return nil, fmt.Errorf("uinput: %w", err)
		}
	}
	for _, k := range padKeys {
		if err := set(uiSetKeyBit, int(k)); err != nil {
			f.Close()
			return nil, fmt.Errorf("uinput: %w", err)
		}
	}
	// struct uinput_user_dev: name[80], input_id (bustype, vendor,
	// product, version), ff_effects_max, then absmax, absmin, absfuzz and
	// absflat, 64 each.
	var dev struct {
		Name                       [80]byte
		Bus, Vendor, Product, Ver  uint16
		FFMax                      uint32
		AbsMax, AbsMin, Fuzz, Flat [64]int32
	}
	copy(dev.Name[:], fmt.Sprintf("Matinee Controller %d", n+1))
	dev.Bus, dev.Vendor, dev.Product, dev.Ver = 0x03, 0x045e, 0x028e, 0x0110 // USB, Microsoft, Xbox 360 pad
	for _, a := range padAxes {
		if err := set(uiSetAbsBit, int(a.Code)); err != nil {
			f.Close()
			return nil, fmt.Errorf("uinput: %w", err)
		}
		dev.AbsMax[a.Code], dev.AbsMin[a.Code], dev.Flat[a.Code] = a.Max, a.Min, a.Flat
	}
	var b bytes.Buffer
	binary.Write(&b, binary.NativeEndian, &dev)
	if _, err := f.Write(b.Bytes()); err != nil {
		f.Close()
		return nil, fmt.Errorf("uinput: %w", err)
	}
	if err := unix.IoctlSetInt(fd, uiDevCreate, 0); err != nil {
		f.Close()
		return nil, fmt.Errorf("uinput: %w", err)
	}
	return &pad{f: f}, nil
}

func (p *pad) send(evs []event) error {
	if len(evs) == 0 {
		return nil
	}
	var b bytes.Buffer
	now := time.Now()
	for _, e := range evs {
		// struct input_event: a timeval, then type, code, value.
		binary.Write(&b, binary.NativeEndian, struct {
			Sec, Usec  int64
			Type, Code uint16
			Value      int32
		}{now.Unix(), int64(now.Nanosecond() / 1000), e.Type, e.Code, e.Value})
	}
	_, err := p.f.Write(b.Bytes())
	return err
}

func (p *pad) close() {
	unix.IoctlSetInt(int(p.f.Fd()), uiDevDestroy, 0)
	p.f.Close()
}

// gamePIDs are the processes Steam started for a game: they carry its app
// id in SteamAppId or SteamGameId.
func gamePIDs(appID int) []int {
	if appID == BigPicture {
		return anyGamePIDs()
	}
	want := [][]byte{[]byte("SteamAppId=" + strconv.Itoa(appID) + "\x00"), []byte("SteamGameId=" + strconv.Itoa(appID) + "\x00")}
	entries, _ := os.ReadDir("/proc")
	var out []int
	self := os.Getpid()
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid == self {
			continue
		}
		env, err := os.ReadFile(filepath.Join("/proc", e.Name(), "environ"))
		if err != nil {
			continue
		}
		env = append(env, 0)
		for _, w := range want {
			if bytes.Contains(env, w) {
				if cmd, _ := os.ReadFile(filepath.Join("/proc", e.Name(), "comm")); strings.TrimSpace(string(cmd)) != "steam" {
					out = append(out, pid)
				}
				break
			}
		}
	}
	return out
}

// anyGamePIDs are the processes of whatever games Steam is running: those
// with a Steam app id, other than Steam's own.
func anyGamePIDs() []int {
	entries, _ := os.ReadDir("/proc")
	var out []int
	self := os.Getpid()
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid == self {
			continue
		}
		env, err := os.ReadFile(filepath.Join("/proc", e.Name(), "environ"))
		if err != nil {
			continue
		}
		game := false
		for _, kv := range bytes.Split(env, []byte{0}) {
			if v, ok := bytes.CutPrefix(kv, []byte("SteamAppId=")); ok {
				id, _ := strconv.Atoi(string(v))
				game = id > 0
				break
			}
		}
		if !game {
			continue
		}
		switch cmd, _ := os.ReadFile(filepath.Join("/proc", e.Name(), "comm")); strings.TrimSpace(string(cmd)) {
		case "steam", "steamwebhelper", "steam-runtime-l", "srt-logger":
		default:
			out = append(out, pid)
		}
	}
	return out
}

// stopGame asks a game's processes to end, then ends them.
func stopGame(appID int) {
	pids := gamePIDs(appID)
	for _, p := range pids {
		syscall.Kill(p, syscall.SIGTERM)
	}
	for range 20 {
		if len(gamePIDs(appID)) == 0 {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	for _, p := range gamePIDs(appID) {
		syscall.Kill(p, syscall.SIGKILL)
	}
}
