package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"sync"
	"time"
)

// Modes: what the server does about new versions.
const (
	ModeInstall = "install" // install them when nobody is watching, where it can
	ModeNotify  = "notify"  // look for them and show them in Settings
	ModeOff     = "off"     // never ask GitHub
)

// Settings are the admin's choices in Settings → Updates.
type Settings struct {
	Channel string `json:"channel"` // stable or beta
	Mode    string `json:"mode"`    // install, notify or off
}

// DefaultSettings: stable releases, shown but installed by hand.
var DefaultSettings = Settings{Channel: ChannelStable, Mode: ModeNotify}

const settingKey = "updates"

// Store keeps the settings.
type Store interface {
	Setting(ctx context.Context, key string) (string, error)
	SetSetting(ctx context.Context, key, value string) error
}

// Apply states.
const (
	StateDownloading = "downloading"
	StateInstalling  = "installing"
	StateRestarting  = "restarting"
	StateFailed      = "failed"
)

// ApplyState is how installing an update is going.
type ApplyState struct {
	State   string `json:"state,omitempty"`
	Version string `json:"version,omitempty"`
	Error   string `json:"error,omitempty"`
}

// Status is everything Settings → Updates shows.
type Status struct {
	Current  string   `json:"current"`
	Install  Install  `json:"install"`
	Settings Settings `json:"settings"`
	Checked  int64    `json:"checked,omitempty"` // Unix milliseconds
	Error    string   `json:"error,omitempty"`   // why the last check failed
	Latest   *Release `json:"latest,omitempty"`  // a newer version, if any
	// Ahead is a build newer than anything on the channel, such as a beta
	// build on the stable channel; it's offered the next newer release.
	Ahead bool       `json:"ahead,omitempty"`
	Apply ApplyState `json:"apply"`
	// Steps are the commands that install Latest by hand.
	Steps []string `json:"steps,omitempty"`
}

// Manager checks for updates now and then, and installs them when asked or
// when the admin chose to have them installed.
type Manager struct {
	Store   Store
	Log     *slog.Logger
	Version string
	Install Install
	Checker *Checker
	// Busy is whether anyone is watching or listening, which holds off an
	// automatic install.
	Busy func() bool
	// Restart stops the server and starts exe in its place.
	Restart func(exe string)

	mu      sync.Mutex
	checked time.Time
	err     string
	latest  *Release
	ahead   bool
	apply   ApplyState
	// failed is the version an automatic install last failed on, so it
	// isn't retried every few minutes.
	failed string
}

// Settings are the admin's choices, or the defaults.
func (m *Manager) Settings(ctx context.Context) Settings {
	s := DefaultSettings
	if v, err := m.Store.Setting(ctx, settingKey); err == nil && v != "" {
		json.Unmarshal([]byte(v), &s)
	}
	if s.Channel != ChannelBeta || !m.Install.Beta {
		s.Channel = ChannelStable
	}
	switch s.Mode {
	case ModeInstall, ModeNotify, ModeOff:
	default:
		s.Mode = DefaultSettings.Mode
	}
	return s
}

// ErrBadSettings is a channel or mode that isn't offered here.
var ErrBadSettings = errors.New("unknown channel or mode")

// SaveSettings keeps new choices. A new channel forgets what the old one found.
func (m *Manager) SaveSettings(ctx context.Context, s Settings) error {
	if (s.Channel != ChannelStable && s.Channel != ChannelBeta) || (s.Channel == ChannelBeta && !m.Install.Beta) {
		return ErrBadSettings
	}
	if s.Mode != ModeInstall && s.Mode != ModeNotify && s.Mode != ModeOff {
		return ErrBadSettings
	}
	old := m.Settings(ctx)
	b, _ := json.Marshal(s)
	if err := m.Store.SetSetting(ctx, settingKey, string(b)); err != nil {
		return err
	}
	if old.Channel != s.Channel {
		m.mu.Lock()
		m.latest, m.ahead, m.checked, m.err, m.failed = nil, false, time.Time{}, "", ""
		m.mu.Unlock()
	}
	return nil
}

// Status is what the last check found.
func (m *Manager) Status(ctx context.Context) Status {
	s := m.Settings(ctx)
	m.mu.Lock()
	defer m.mu.Unlock()
	st := Status{Current: Display(m.Version), Install: m.Install, Settings: s, Error: m.err, Latest: m.latest, Ahead: m.ahead, Apply: m.apply}
	if !m.checked.IsZero() {
		st.Checked = m.checked.UnixMilli()
	}
	if m.latest != nil {
		st.Steps = Steps(m.Install, s.Channel, m.latest)
	}
	return st
}

// Check asks GitHub now.
func (m *Manager) Check(ctx context.Context) error {
	s := m.Settings(ctx)
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	latest, ahead, err := m.Checker.Check(ctx, m.Version, s.Channel)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.checked = time.Now()
	if err != nil {
		m.err = err.Error()
		return err
	}
	m.err, m.latest, m.ahead = "", latest, ahead
	if latest != nil && m.apply.State == StateFailed && m.apply.Version != latest.Version {
		m.apply = ApplyState{}
	}
	return nil
}

// ErrCantApply means this server is updated some other way.
var ErrCantApply = errors.New("this server can't install updates itself")

// ErrNothingNew means there's no newer version to install.
var ErrNothingNew = errors.New("there's no newer version to install")

// ErrBusy means an update is already being installed.
var ErrBusy = errors.New("an update is already being installed")

// Apply starts installing the newest version, in the background. The
// server restarts once it's in.
func (m *Manager) Apply() error {
	if !m.Install.CanApply {
		return ErrCantApply
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.latest == nil {
		return ErrNothingNew
	}
	switch m.apply.State {
	case StateDownloading, StateInstalling, StateRestarting:
		return ErrBusy
	}
	rel := *m.latest
	m.apply = ApplyState{State: StateDownloading, Version: rel.Version}
	if m.Install.Kind != KindBinary {
		m.apply.State = StateInstalling
	}
	go m.install(rel)
	return nil
}

func (m *Manager) set(state ApplyState) {
	m.mu.Lock()
	m.apply = state
	if state.State == StateFailed {
		m.failed = state.Version
	}
	m.mu.Unlock()
}

func (m *Manager) install(rel Release) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	m.Log.Info("installing update", "from", Display(m.Version), "to", rel.Version, "install", m.Install.Kind)
	exe := m.Install.exe
	var err error
	switch m.Install.Kind {
	case KindBinary:
		if err = m.Checker.installBinary(ctx, rel.Version, exe); err == nil {
			m.set(ApplyState{State: StateInstalling, Version: rel.Version})
		}
	case KindHomebrew:
		err = upgradeHomebrew(ctx, m.Install.prefix)
		// opt/matinee always points at the installed version.
		exe = filepath.Join(m.Install.prefix, "opt", "matinee", "bin", "matinee")
	case KindDeb, KindRPM:
		// The package's install script restarts the server once it's in.
		err = requestPackage(ctx, PackageRequest, PackageLog)
		if err == nil {
			return
		}
	default:
		err = ErrCantApply
	}
	if err != nil {
		m.Log.Error("couldn't install the update", "to", rel.Version, "err", err)
		m.set(ApplyState{State: StateFailed, Version: rel.Version, Error: err.Error()})
		return
	}
	m.Log.Info("update installed; restarting", "to", rel.Version)
	m.set(ApplyState{State: StateRestarting, Version: rel.Version})
	if m.Restart != nil {
		m.Restart(exe)
	}
}

// Every is how often the server looks for updates, and Retry how soon it
// looks again to install one while someone is watching.
var (
	Every = 6 * time.Hour
	Retry = 15 * time.Minute
)

// Run looks for updates now and then, and installs them when the admin
// chose that and nobody is watching.
func (m *Manager) Run(ctx context.Context) {
	wait := time.Minute // let the server settle first
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		wait = Every
		s := m.Settings(ctx)
		if s.Mode == ModeOff {
			continue
		}
		if err := m.Check(ctx); err != nil {
			if !errors.Is(err, ErrUnknownVersion) {
				m.Log.Warn("couldn't look for updates", "err", err)
			}
			continue
		}
		m.mu.Lock()
		latest, failed := m.latest, m.failed
		m.mu.Unlock()
		if latest == nil {
			continue
		}
		m.Log.Info("an update is available", "version", latest.Version, "running", Display(m.Version))
		if s.Mode != ModeInstall || !m.Install.CanApply || failed == latest.Version {
			continue
		}
		if m.Busy != nil && m.Busy() {
			wait = Retry
			continue
		}
		if err := m.Apply(); err != nil && !errors.Is(err, ErrBusy) {
			m.Log.Warn("couldn't start the update", "err", err)
		}
	}
}

// Steps are the commands that install rel by hand on this kind of install.
// The web player explains them; the commands themselves aren't translated.
func Steps(in Install, channel string, rel *Release) []string {
	version := rel.Version
	plain := version
	if len(plain) > 1 && plain[0] == 'v' {
		plain = plain[1:]
	}
	image := "ghcr.io/yeixio/matinee:" + plain
	tagValue := plain
	if channel == ChannelBeta {
		tagValue = "sha-" + short(rel.commit)
		if rel.commit == "" && len(version) > 5 {
			tagValue = "sha-" + version[5:]
		}
		image = "ghcr.io/yeixio/matinee:main"
	}
	switch in.Kind {
	case KindDocker:
		return []string{
			"docker pull " + image,
			"docker compose up -d   # or remove and run the container again with " + image,
		}
	case KindKubernetes:
		if in.ManagedBy != "" {
			if channel == ChannelBeta {
				return []string{"image:\n  tag: " + tagValue}
			}
			return []string{"targetRevision: " + plain + "   # the chart from oci://ghcr.io/yeixio/charts/matinee"}
		}
		release, ns := in.Release, in.Namespace
		if release == "" {
			release = "matinee"
		}
		nsFlag := ""
		if ns != "" {
			nsFlag = " -n " + ns
		}
		if channel == ChannelBeta {
			return []string{fmt.Sprintf("helm upgrade %s oci://ghcr.io/yeixio/charts/matinee%s --reuse-values --set image.tag=%s", release, nsFlag, tagValue)}
		}
		return []string{fmt.Sprintf("helm upgrade %s oci://ghcr.io/yeixio/charts/matinee%s --reuse-values --version %s", release, nsFlag, plain)}
	case KindDeb:
		return []string{"sudo apt update && sudo apt install --only-upgrade matinee"}
	case KindRPM:
		return []string{"sudo dnf upgrade --refresh matinee"}
	case KindHomebrew:
		return []string{"brew upgrade matinee", "brew services restart matinee"}
	case KindBinary:
		// The release page, which has the archive for every computer.
		return []string{rel.URL}
	}
	return nil
}

// What a check found, for the log.
func (r *Release) String() string { return fmt.Sprintf("%s (%s)", r.Version, r.URL) }
