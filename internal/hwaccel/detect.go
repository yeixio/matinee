package hwaccel

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Preferences besides a Kind: let Matinee pick, or always use the CPU.
const (
	Auto = "auto"
	Off  = "off"
)

// Manager finds the backends that work on this machine and picks the one
// transcodes use, following the admin's preference.
type Manager struct {
	FFmpeg string
	Log    *slog.Logger

	mu        sync.Mutex
	pref      string
	found     []Accel
	problem   string
	detecting bool
}

func NewManager(ffmpeg string, log *slog.Logger) *Manager {
	return &Manager{FFmpeg: ffmpeg, Log: log, pref: Auto}
}

// Status is what Settings shows about hardware transcoding.
type Status struct {
	// Preference is auto, off, or a backend's id. The API serves it as a
	// setting of its own.
	Preference string `json:"-"`
	// Active is the backend new transcodes use, or null for the CPU.
	Active *Accel `json:"active"`
	// Available lists the backends that passed a test encode at startup.
	Available []Accel `json:"available"`
	// Detecting is true while the startup tests are still running.
	Detecting bool `json:"detecting"`
	// Problem explains, when no accelerator works, why a GPU Matinee can
	// see couldn't be used, such as a missing permission.
	Problem string `json:"problem,omitempty"`
}

// ValidPreference says whether p can be passed to SetPreference.
func ValidPreference(p string) bool { return p == Auto || p == Off || Kind(p).Valid() }

// SetPreference chooses auto, off, or a backend. A backend that isn't
// available leaves transcodes on the CPU (Settings says so) rather than
// switching to another one the admin didn't ask for.
func (m *Manager) SetPreference(p string) {
	if p == "" {
		p = Auto
	}
	m.mu.Lock()
	m.pref = p
	m.mu.Unlock()
}

// Active is the backend new transcodes use, or nil for the CPU.
func (m *Manager) Active() *Accel {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.activeLocked()
}

func (m *Manager) activeLocked() *Accel {
	if m.pref == Off || len(m.found) == 0 {
		return nil
	}
	for i := range m.found {
		if m.pref == Auto || string(m.found[i].Kind) == m.pref {
			a := m.found[i]
			return &a
		}
	}
	return nil
}

func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	return Status{Preference: m.pref, Active: m.activeLocked(), Available: append([]Accel{}, m.found...),
		Detecting: m.detecting, Problem: m.problem}
}

// Detect tests each backend that could work on this OS with a short encode
// and remembers the ones that pass. It takes a few seconds, so the server
// runs it in the background; transcodes use the CPU until it's done.
func (m *Manager) Detect(ctx context.Context) {
	m.mu.Lock()
	m.detecting = true
	m.mu.Unlock()
	found, problem := m.detect(ctx)
	if len(found) > 0 {
		problem = ""
	}
	m.mu.Lock()
	m.found, m.problem, m.detecting = found, problem, false
	active := m.activeLocked()
	m.mu.Unlock()
	switch {
	case active != nil:
		m.Log.Info("hardware transcoding", "using", active.Name, "device", active.Device, "gpu", active.Detail, "decode", active.Decode)
	case problem != "":
		m.Log.Warn("hardware transcoding: " + problem)
	case len(found) == 0:
		m.Log.Info("hardware transcoding: no working accelerator found; transcoding on the CPU")
	}
}

// candidates are the backends worth testing on goos, best first.
func candidates(goos string) []Kind {
	switch goos {
	case "linux":
		return []Kind{NVENC, VAAPI, QSV, V4L2, AMF}
	case "darwin":
		return []Kind{VideoToolbox}
	case "windows":
		return []Kind{NVENC, QSV, AMF}
	}
	return nil
}

// detect returns the backends that work, and why a GPU it found can't be
// opened, if that's the case.
func (m *Manager) detect(ctx context.Context) (found []Accel, problem string) {
	encoders, err := m.list(ctx, "-encoders")
	if err != nil {
		m.Log.Warn("hardware transcoding: can't list FFmpeg's encoders", "err", err)
		return nil, ""
	}
	filters, _ := m.list(ctx, "-filters")
	dir, err := os.MkdirTemp("", "matinee-hwaccel")
	if err != nil {
		return nil, ""
	}
	defer os.RemoveAll(dir)

	for _, k := range candidates(runtime.GOOS) {
		if !encoders[encoder[k]] {
			continue
		}
		for _, dev := range devices(k) {
			if dev != "" {
				if p := deviceProblem(dev); p != "" {
					problem = p
					continue
				}
			}
			a := Accel{Kind: k, Name: k.Name(), Device: dev}
			sample := filepath.Join(dir, string(k)+".mp4")
			log, err := m.try(ctx, &a, Encode, nil, sample)
			if err != nil {
				if dev != "" {
					// There is a GPU, so this failure is worth explaining.
					problem = fmt.Sprintf("The %s test encode on %s failed: %v", k.Name(), dev, err)
					m.Log.Info("hardware transcoding: test encode failed", "accel", k, "device", dev, "err", err)
				} else {
					m.Log.Debug("hardware transcoding: test encode failed", "accel", k, "err", err)
				}
				continue
			}
			a.Detail = gpuName(k, log)
			if _, scaler := gpuScaler[k]; (!scaler || filters[gpuScaler[k]]) && decodes[k] != nil {
				_, err := m.try(ctx, &a, Full, []string{"-i", sample}, "")
				a.Decode = err == nil
				if err != nil {
					m.Log.Debug("hardware transcoding: test decode failed; decoding on the CPU", "accel", k, "err", err)
				}
			}
			found = append(found, a)
			break
		}
	}
	return found, problem
}

// devices lists the render nodes to try for backends that need one.
func devices(k Kind) []string {
	if k != VAAPI && !(k == QSV && runtime.GOOS == "linux") {
		return []string{""}
	}
	if d := os.Getenv("MATINEE_VAAPI_DEVICE"); d != "" {
		return []string{d}
	}
	nodes, _ := filepath.Glob("/dev/dri/renderD*")
	if k == QSV && len(nodes) == 0 {
		return []string{""}
	}
	return nodes
}

// try runs a one-second transcode in mode m: of a test pattern into out (an
// MP4) when in is nil, or of in into nothing. It returns FFmpeg's log.
func (m *Manager) try(ctx context.Context, a *Accel, mode Mode, in []string, out string) (string, error) {
	codec := "rawvideo"
	if in != nil {
		codec = "h264"
	}
	input, output := Args(a, mode, Video{Codec: codec, BurnSub: -1, MaxWidth: 1920, KeyframeSeconds: 4})
	if in == nil {
		in = []string{"-f", "lavfi", "-i", "testsrc2=size=1920x1080:rate=30"}
	}
	sink := []string{"-f", "null", "-"}
	if out != "" {
		sink = []string{"-f", "mp4", "-y", out}
	}
	args := append([]string{"-hide_banner", "-loglevel", "verbose", "-nostdin"}, input...)
	args = append(append(append(args, in...), output...), "-frames:v", "30")
	args = append(args, sink...)

	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	var log bytes.Buffer
	cmd := exec.CommandContext(ctx, m.FFmpeg, args...)
	cmd.Stderr = &log
	if err := cmd.Run(); err != nil {
		return log.String(), fmt.Errorf("%w: %s", err, why(log.String()))
	}
	if out != "" {
		if st, err := os.Stat(out); err != nil || st.Size() == 0 {
			return log.String(), errors.New("no output")
		}
	}
	return log.String(), nil
}

// list returns the names in an FFmpeg listing such as -encoders or -filters,
// whose lines are flags, then a name, then a description.
func (m *Manager) list(ctx context.Context, what string) (map[string]bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, m.FFmpeg, "-hide_banner", what).Output()
	if err != nil {
		return nil, err
	}
	return parseList(string(out)), nil
}

func parseList(out string) map[string]bool {
	names := map[string]bool{}
	for _, l := range strings.Split(out, "\n") {
		if f := strings.Fields(l); len(f) >= 2 {
			names[f[1]] = true
		}
	}
	return names
}

var (
	// "VAAPI driver: Mesa Gallium driver 24.2.8 for AMD Radeon RX 7900 XTX (radeonsi, navi31, ...)."
	vaapiDriverRe = regexp.MustCompile(`VAAPI driver: (.+?)\.?\n`)
	mesaGPURe     = regexp.MustCompile(` for (.+?) \(`)
)

// gpuName picks the GPU's name out of a test encode's log, or asks the
// driver's own tool. It's "" when neither says.
func gpuName(k Kind, log string) string {
	switch k {
	case VAAPI, QSV:
		m := vaapiDriverRe.FindStringSubmatch(log)
		if m == nil {
			return ""
		}
		if g := mesaGPURe.FindStringSubmatch(m[1]); g != nil {
			return g[1]
		}
		return strings.TrimSpace(m[1])
	case NVENC:
		out, err := exec.Command("nvidia-smi", "--query-gpu=name", "--format=csv,noheader").Output()
		if err == nil {
			name, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
			return name
		}
	}
	return ""
}

var failureRe = regexp.MustCompile(`(?i)error|fail|cannot|unable|not supported|no device|invalid`)

// why picks the lines that explain a failure out of FFmpeg's verbose log.
func why(log string) string {
	var lines []string
	for _, l := range strings.Split(strings.TrimSpace(log), "\n") {
		if failureRe.MatchString(l) && !strings.Contains(l, "decode errors") {
			lines = append(lines, strings.TrimSpace(l))
		}
	}
	return strings.Join(lines[max(0, len(lines)-3):], " | ")
}
