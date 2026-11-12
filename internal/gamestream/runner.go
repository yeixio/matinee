// Package gamestream plays games on the server and streams them to players
// and apps: an emulator runs in its own process (games/runner), FFmpeg
// encodes its pictures and sound, and WebRTC carries them to the player with
// the controller's buttons coming back the other way.
package gamestream

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Info is what the emulator tells us before the first frame: every frame is
// Width×Height pixels of 4 bytes (bgr0), FPS of them a second, and sound
// comes as 16-bit stereo at SampleRate.
type Info struct {
	Width      int     `json:"width"`
	Height     int     `json:"height"`
	FPS        float64 `json:"fps"`
	SampleRate float64 `json:"sampleRate"`
	Aspect     float64 `json:"aspect"`
}

// Runner is one game being played by matinee-game-runner.
type Runner struct {
	cmd    *exec.Cmd
	readMu sync.Mutex // one reader of out at a time
	out    *bufio.Reader
	mu     sync.Mutex
	in     io.WriteCloser
	stderr *bytes.Buffer
	Info   Info
}

// StartRunner starts the runner for game (empty for a core that needs no
// game) and waits for its Info. options set the emulator's settings that
// shouldn't keep their defaults.
func StartRunner(runnerPath, core, game, saveDir, systemDir string, options map[string]string) (*Runner, error) {
	if game == "" {
		game = "-"
	}
	cmd := exec.Command(runnerPath, core, game, saveDir, systemDir)
	if len(options) > 0 {
		keys := make([]string, 0, len(options))
		for k := range options {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var b strings.Builder
		for _, k := range keys {
			fmt.Fprintf(&b, "%s=%s\n", k, options[k])
		}
		cmd.Env = append(os.Environ(), "MATINEE_GAME_OPTIONS="+b.String())
	}
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	r := &Runner{cmd: cmd, in: in, out: bufio.NewReaderSize(out, 1<<20), stderr: &bytes.Buffer{}}
	cmd.Stderr = &limitedWriter{w: r.stderr, n: 64 << 10}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	typ, msg, err := r.next()
	if err == nil && typ != 'I' {
		err = fmt.Errorf("emulator: %s", msg)
		if typ != 'E' {
			err = fmt.Errorf("emulator sent %q before its picture size", typ)
		}
	}
	if err == nil {
		err = json.Unmarshal(msg, &r.Info)
	}
	if err == nil && (r.Info.Width <= 0 || r.Info.Height <= 0 || r.Info.FPS <= 0) {
		err = fmt.Errorf("emulator gave no picture size")
	}
	if err != nil {
		r.Close()
		if errors.Is(err, io.EOF) {
			err = fmt.Errorf("emulator stopped: %s", strings.TrimSpace(r.stderr.String()))
		}
		return nil, err
	}
	return r, nil
}

const maxMessage = 64 << 20

func (r *Runner) next() (byte, []byte, error) {
	r.readMu.Lock()
	defer r.readMu.Unlock()
	var h [8]byte
	if _, err := io.ReadFull(r.out, h[:]); err != nil {
		return 0, nil, err
	}
	n := binary.LittleEndian.Uint32(h[4:])
	if n > maxMessage {
		return 0, nil, fmt.Errorf("emulator sent a %d-byte message", n)
	}
	b := make([]byte, n)
	if _, err := io.ReadFull(r.out, b); err != nil {
		return 0, nil, err
	}
	return h[0], b, nil
}

// Run hands every frame to frame and every bit of sound to sound until the
// game stops, then returns why.
func (r *Runner) Run(frame, sound func([]byte) error) error {
	for {
		typ, msg, err := r.next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		switch typ {
		case 'V':
			err = frame(msg)
		case 'A':
			err = sound(msg)
		case 'E':
			err = fmt.Errorf("emulator: %s", msg)
		}
		if err != nil {
			return err
		}
	}
}

// SetButtons says which buttons are held on a controller: a bitmask of
// libretro's RETRO_DEVICE_ID_JOYPAD_* (B=0 … R3=15).
func (r *Runner) SetButtons(port int, mask uint16) error {
	if port < 0 || port > 3 {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	_, err := r.in.Write([]byte{'B', byte(port), byte(mask), byte(mask >> 8)})
	return err
}

// SetStick says where an analog stick is: axis 0 and 1 are the left
// stick's x and y, 2 and 3 the right's, from -128 (left, up) to 127.
func (r *Runner) SetStick(port, axis int, v int8) error {
	if port < 0 || port > 3 || axis < 0 || axis > 3 {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	_, err := r.in.Write([]byte{'X', byte(port), byte(axis), byte(v)})
	return err
}

// closeWait is how long the runner may take to save and stop before it is
// killed.
const closeWait = 15 * time.Second

// Close saves the game and stops the runner.
func (r *Runner) Close() error {
	r.mu.Lock()
	r.in.Close()
	r.mu.Unlock()
	// Keep reading what it sends, or a runner stuck writing a frame nobody
	// reads never sees its input close and never stops.
	go func() {
		for {
			if _, _, err := r.next(); err != nil {
				return
			}
		}
	}()
	t := time.AfterFunc(closeWait, func() { r.cmd.Process.Kill() })
	defer t.Stop()
	return r.cmd.Wait()
}

type limitedWriter struct {
	w io.Writer
	n int
}

func (l *limitedWriter) Write(p []byte) (int, error) {
	if l.n <= 0 {
		return len(p), nil
	}
	q := p
	if len(q) > l.n {
		q = q[:l.n]
	}
	l.n -= len(q)
	l.w.Write(q)
	return len(p), nil
}

// CoreInfo is what an emulator core says about itself.
type CoreInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	// Extensions are the files it plays, lower case with a leading dot.
	Extensions   []string `json:"extensions"`
	NeedFullpath bool     `json:"needFullpath"`
}

// ReadCoreInfo asks the runner what the core at path is. It fails for a file
// that isn't a libretro core this server can load (built for another kind
// of computer, say).
func ReadCoreInfo(ctx context.Context, runnerPath, core string) (CoreInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, runnerPath, "--info", core)
	var stderr bytes.Buffer
	cmd.Stderr = &limitedWriter{w: &stderr, n: 4 << 10}
	out, err := cmd.Output()
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return CoreInfo{}, errors.New(msg)
		}
		return CoreInfo{}, err
	}
	var info CoreInfo
	if err := json.Unmarshal(out, &info); err != nil {
		return CoreInfo{}, fmt.Errorf("emulator said: %w", err)
	}
	exts := info.Extensions[:0]
	for _, e := range info.Extensions {
		e = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(e, ".")))
		if e != "" {
			exts = append(exts, "."+e)
		}
	}
	info.Extensions = exts
	if info.Name == "" {
		info.Name = strings.TrimSuffix(strings.TrimSuffix(filepath.Base(core), ".so"), "_libretro")
	}
	return info, nil
}
