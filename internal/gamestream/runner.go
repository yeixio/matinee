// Package gamestream plays games on the server and streams them to players
// and apps: an emulator runs in its own process (games/runner), FFmpeg
// encodes its pictures and sound, and WebRTC carries them to the player with
// the controller's buttons coming back the other way.
package gamestream

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
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
	out    *bufio.Reader
	mu     sync.Mutex
	in     io.WriteCloser
	stderr *bytes.Buffer
	Info   Info
}

// StartRunner starts the runner for game (empty for a core that needs no
// game) and waits for its Info.
func StartRunner(runnerPath, core, game, saveDir, systemDir string) (*Runner, error) {
	if game == "" {
		game = "-"
	}
	cmd := exec.Command(runnerPath, core, game, saveDir, systemDir)
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

// Close saves the game and stops the runner.
func (r *Runner) Close() error {
	r.mu.Lock()
	r.in.Close()
	r.mu.Unlock()
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
