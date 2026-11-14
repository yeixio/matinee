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
	uiSetRelBit  = 0x40045566
	uiSetAbsBit  = 0x40045567
)

// device is one virtual input device made with /dev/uinput.
type device struct {
	f *os.File
}

// newDevice makes a virtual input device with what spec says it has.
func newDevice(spec deviceSpec) (*device, error) {
	f, err := os.OpenFile("/dev/uinput", os.O_WRONLY|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, fmt.Errorf("can't make a virtual %s: %w", spec.Name, err)
	}
	fd := int(f.Fd())
	fail := func(err error) (*device, error) {
		f.Close()
		return nil, fmt.Errorf("uinput: %w", err)
	}
	set := func(req uint, v int) error { return unix.IoctlSetInt(fd, req, v) }
	bits := []struct {
		req  uint
		ev   int
		list []uint16
	}{{uiSetKeyBit, evKey, spec.Keys}, {uiSetRelBit, evRel, spec.Rels}}
	if err := set(uiSetEvBit, evSyn); err != nil {
		return fail(err)
	}
	for _, b := range bits {
		if len(b.list) == 0 {
			continue
		}
		if err := set(uiSetEvBit, b.ev); err != nil {
			return fail(err)
		}
		for _, c := range b.list {
			if err := set(b.req, int(c)); err != nil {
				return fail(err)
			}
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
	copy(dev.Name[:], spec.Name)
	dev.Bus, dev.Vendor, dev.Product, dev.Ver = spec.Bus, spec.Vendor, spec.Product, spec.Version
	if len(spec.Abs) > 0 {
		if err := set(uiSetEvBit, evAbs); err != nil {
			return fail(err)
		}
	}
	for _, a := range spec.Abs {
		if err := set(uiSetAbsBit, int(a.Code)); err != nil {
			return fail(err)
		}
		dev.AbsMax[a.Code], dev.AbsMin[a.Code], dev.Flat[a.Code] = a.Max, a.Min, a.Flat
	}
	var b bytes.Buffer
	binary.Write(&b, binary.NativeEndian, &dev)
	if _, err := f.Write(b.Bytes()); err != nil {
		return fail(err)
	}
	if err := unix.IoctlSetInt(fd, uiDevCreate, 0); err != nil {
		return fail(err)
	}
	return &device{f: f}, nil
}

func (d *device) send(evs []event) error {
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
	_, err := d.f.Write(b.Bytes())
	return err
}

func (d *device) close() {
	unix.IoctlSetInt(int(d.f.Fd()), uiDevDestroy, 0)
	d.f.Close()
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
