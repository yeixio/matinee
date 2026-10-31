package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"time"
)

// State is what the tray says about the server.
type State int

const (
	Stopped  State = iota
	Starting       // running, but not answering yet
	Running
	Failed // stopped by itself and kept stopping, so it isn't restarted
)

// crashLimit crashes within crashWindow and the server stays stopped,
// rather than restarting over and over (a port in use, a broken database).
const (
	crashLimit  = 3
	crashWindow = 5 * time.Minute
)

// Server runs the matinee server as a child process. Closing its stdin
// stops it (MATINEE_STOP_ON_STDIN_CLOSE in cmd/matinee), since Windows has
// no SIGTERM; that also stops it if the tray app goes away.
type Server struct {
	Exe       string
	Args      []string
	Env       []string // added to this process's environment
	Log       io.Writer
	HealthURL string          // the server's /api/v1/health
	Prepare   func(*exec.Cmd) // hides the console window on Windows
	// RestartDelay is how long after a crash the server starts again.
	RestartDelay time.Duration

	mu      sync.Mutex
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	done    chan struct{}
	wanted  bool
	crashes []time.Time
	failed  bool
	lastErr error
}

// Start starts the server unless it's running already.
func (s *Server) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.wanted, s.failed, s.crashes = true, false, nil
	return s.startLocked()
}

func (s *Server) startLocked() error {
	if s.cmd != nil {
		return nil
	}
	cmd := exec.Command(s.Exe, s.Args...)
	cmd.Env = append(os.Environ(), s.Env...)
	cmd.Env = append(cmd.Env, "MATINEE_STOP_ON_STDIN_CLOSE=1")
	cmd.Stdout, cmd.Stderr = s.Log, s.Log
	if s.Prepare != nil {
		s.Prepare(cmd)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		s.lastErr = err
		s.failed = true
		return fmt.Errorf("start %s: %w", s.Exe, err)
	}
	done := make(chan struct{})
	s.cmd, s.stdin, s.done, s.lastErr = cmd, stdin, done, nil
	go s.wait(cmd, done)
	return nil
}

// wait notices the server stopping, and starts it again if nobody asked it to stop.
func (s *Server) wait(cmd *exec.Cmd, done chan struct{}) {
	err := cmd.Wait()
	s.mu.Lock()
	defer s.mu.Unlock()
	close(done)
	if s.cmd != cmd {
		return
	}
	s.cmd, s.stdin = nil, nil
	if !s.wanted {
		return
	}
	if err == nil {
		err = errors.New("the server stopped by itself")
	}
	s.lastErr = err
	now := time.Now()
	recent := s.crashes[:0]
	for _, t := range s.crashes {
		if now.Sub(t) < crashWindow {
			recent = append(recent, t)
		}
	}
	s.crashes = append(recent, now)
	if len(s.crashes) >= crashLimit {
		s.failed = true
		fmt.Fprintf(s.Log, "tray: the server stopped %d times in a few minutes, so it stays stopped: %v\n", len(s.crashes), err)
		return
	}
	fmt.Fprintf(s.Log, "tray: the server stopped (%v); starting it again\n", err)
	time.AfterFunc(s.RestartDelay, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.wanted && !s.failed {
			s.startLocked()
		}
	})
}

// Stop asks the server to stop and waits up to timeout for it, then ends it.
func (s *Server) Stop(timeout time.Duration) {
	s.mu.Lock()
	s.wanted = false
	cmd, stdin, done := s.cmd, s.stdin, s.done
	s.mu.Unlock()
	if cmd == nil {
		return
	}
	stdin.Close()
	select {
	case <-done:
	case <-time.After(timeout):
		cmd.Process.Kill()
		<-done
	}
}

// Restart stops the server and starts it again.
func (s *Server) Restart(timeout time.Duration) error {
	s.Stop(timeout)
	return s.Start()
}

// Wanted is whether the server should be running: it was started and not stopped.
func (s *Server) Wanted() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.wanted
}

// LastError is why the server last stopped by itself, or failed to start.
func (s *Server) LastError() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastErr
}

// State checks on the server, asking it whether it's answering.
func (s *Server) State(ctx context.Context) (State, string) {
	s.mu.Lock()
	running, failed := s.cmd != nil, s.failed
	s.mu.Unlock()
	switch {
	case failed && !running:
		return Failed, ""
	case !running:
		return Stopped, ""
	}
	if v, err := Health(ctx, s.HealthURL); err == nil {
		return Running, v
	}
	return Starting, ""
}

// Health asks a server whether it's up and returns its version.
func Health(ctx context.Context, url string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var h struct {
		Status  string `json:"status"`
		Version string `json:"version"`
		State   string `json:"state"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&h); err != nil {
		return "", err
	}
	if h.Status != "ok" || h.State == "restarting" {
		return "", errors.New("not ready")
	}
	return h.Version, nil
}
