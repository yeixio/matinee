package gamestream

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// An admin can add an emulator of their own: any libretro core built for
// this server's kind of computer (a Linux .so), put in Config.UserCoreDir
// by uploading it in Settings or by hand. Matinee asks it which files it
// plays (matinee-game-runner --info), and it plays those, ahead of the
// built-in emulators. It runs as its own process like the built-in ones, so
// one that crashes only stops its own game.

// Emulator is one emulator on this server, for listing.
type Emulator struct {
	File    string `json:"file"`
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
	// Systems are the built-in systems it plays, by name.
	Systems    []string `json:"systems,omitempty"`
	Extensions []string `json:"extensions"`
	// BuiltIn says it came with Matinee and can't be removed.
	BuiltIn bool `json:"builtIn"`
	// Error is why an added emulator can't be used.
	Error string `json:"error,omitempty"`
}

type userCore struct {
	mod  time.Time
	size int64
	info CoreInfo
	err  error
}

// ErrNotEmulator means an added file isn't an emulator this server can run.
var ErrNotEmulator = errors.New("not an emulator this server can run")

// MaxEmulatorSize is the biggest emulator file that can be added.
const MaxEmulatorSize = 512 << 20

// scanUserCores checks every emulator in UserCoreDir, asking the runner
// about those new or changed since last time, and returns them by name.
func (m *Manager) scanUserCores() []string {
	if m.cfg.UserCoreDir == "" {
		return nil
	}
	entries, _ := os.ReadDir(m.cfg.UserCoreDir)
	m.coresMu.Lock()
	defer m.coresMu.Unlock()
	if m.userCores == nil {
		m.userCores = map[string]userCore{}
	}
	var names []string
	seen := map[string]bool{}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || strings.HasPrefix(name, ".") || !strings.HasSuffix(name, ".so") {
			continue
		}
		st, err := e.Info()
		if err != nil {
			continue
		}
		seen[name] = true
		names = append(names, name)
		if c, ok := m.userCores[name]; ok && c.mod.Equal(st.ModTime()) && c.size == st.Size() {
			continue
		}
		c := userCore{mod: st.ModTime(), size: st.Size()}
		c.info, c.err = ReadCoreInfo(context.Background(), m.cfg.Runner, filepath.Join(m.cfg.UserCoreDir, name))
		if c.err == nil && len(c.info.Extensions) == 0 {
			c.err = errors.New("it doesn't say which games it plays")
		}
		m.userCores[name] = c
	}
	for name := range m.userCores {
		if !seen[name] {
			delete(m.userCores, name)
		}
	}
	slices.Sort(names)
	return names
}

// userSystems are the systems of the emulators an admin added.
func (m *Manager) userSystems() []System {
	var out []System
	for _, name := range m.scanUserCores() {
		m.coresMu.Lock()
		c := m.userCores[name]
		m.coresMu.Unlock()
		if c.err != nil {
			continue
		}
		out = append(out, System{
			Key: "custom-" + strings.TrimSuffix(strings.TrimSuffix(name, ".so"), "_libretro"), Name: c.info.Name,
			Emulator: c.info.Name, Extensions: c.info.Extensions, Custom: true,
			Core: filepath.Join(m.cfg.UserCoreDir, name),
		})
	}
	return out
}

// Emulators lists the emulators an admin added, then the built-in ones.
func (m *Manager) Emulators() []Emulator {
	out := []Emulator{}
	for _, name := range m.scanUserCores() {
		m.coresMu.Lock()
		c := m.userCores[name]
		m.coresMu.Unlock()
		out = append(out, userEmulator(name, c))
	}
	var builtins []Emulator
	for _, s := range builtIn {
		if !fileExists(filepath.Join(m.cfg.CoreDir, s.Core)) {
			continue
		}
		i := slices.IndexFunc(builtins, func(e Emulator) bool { return e.File == s.Core })
		if i < 0 {
			builtins = append(builtins, Emulator{File: s.Core, Name: s.Emulator, BuiltIn: true, Extensions: []string{}})
			i = len(builtins) - 1
		}
		builtins[i].Systems = append(builtins[i].Systems, s.Name)
		builtins[i].Extensions = append(builtins[i].Extensions, s.Extensions...)
	}
	return append(out, builtins...)
}

func userEmulator(name string, c userCore) Emulator {
	e := Emulator{File: name, Name: c.info.Name, Version: c.info.Version, Extensions: c.info.Extensions}
	if e.Extensions == nil {
		e.Extensions = []string{}
	}
	if c.err != nil {
		e.Error = c.err.Error()
	}
	if e.Name == "" {
		e.Name = name
	}
	return e
}

// AddEmulator saves an emulator an admin uploaded as file, once the runner
// has loaded it and it has said which games it plays. One with the same
// name is replaced. A file that isn't an emulator this server can run is
// refused with ErrNotEmulator, and not kept.
func (m *Manager) AddEmulator(ctx context.Context, file string, r io.Reader) (Emulator, error) {
	name := safeName(filepath.Base(file))
	if !strings.HasSuffix(name, ".so") || strings.HasPrefix(name, ".") {
		return Emulator{}, fmt.Errorf("%w: an emulator is a libretro core, a file ending in .so", ErrNotEmulator)
	}
	if err := os.MkdirAll(m.cfg.UserCoreDir, 0o755); err != nil {
		return Emulator{}, err
	}
	tmp, err := os.CreateTemp(m.cfg.UserCoreDir, ".upload-*.so")
	if err != nil {
		return Emulator{}, err
	}
	defer os.Remove(tmp.Name())
	n, err := io.Copy(tmp, io.LimitReader(r, MaxEmulatorSize+1))
	if err == nil {
		err = tmp.Chmod(0o755)
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return Emulator{}, err
	}
	if n > MaxEmulatorSize {
		return Emulator{}, fmt.Errorf("%w: the file is too big", ErrNotEmulator)
	}
	info, err := ReadCoreInfo(ctx, m.cfg.Runner, tmp.Name())
	if err == nil && len(info.Extensions) == 0 {
		err = errors.New("it doesn't say which games it plays")
	}
	if err != nil {
		return Emulator{}, fmt.Errorf("%w: %s", ErrNotEmulator, strings.ReplaceAll(err.Error(), tmp.Name()+": ", ""))
	}
	if err := os.Rename(tmp.Name(), filepath.Join(m.cfg.UserCoreDir, name)); err != nil {
		return Emulator{}, err
	}
	m.scanUserCores()
	m.coresMu.Lock()
	c := m.userCores[name]
	m.coresMu.Unlock()
	return userEmulator(name, c), nil
}

// RemoveEmulator deletes an emulator an admin added. Games being played
// with it carry on until they end.
func (m *Manager) RemoveEmulator(file string) error {
	name := filepath.Base(file)
	if m.cfg.UserCoreDir == "" || name != file || strings.HasPrefix(name, ".") || !strings.HasSuffix(name, ".so") {
		return os.ErrNotExist
	}
	if err := os.Remove(filepath.Join(m.cfg.UserCoreDir, name)); err != nil {
		return err
	}
	m.scanUserCores()
	return nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
