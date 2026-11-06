package update

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"runtime"
	"runtime/debug"
	"sync"
	"time"

	"github.com/yeixio/matinee/internal/kube"
)

// Settings are the admin's choices.
type Settings struct {
	// Channel is release (the default) or main, the beta.
	Channel string `json:"channel"`
	// Auto installs updates as they're found, where the server can.
	Auto bool `json:"auto"`
}

// DefaultSettings: releases, installed by hand.
var DefaultSettings = Settings{Channel: ChannelRelease}

const settingKey = "updates"

// For an Application that follows main: Argo CD moves to each new edge
// commit by itself and runs the image built from it.
const (
	followRevision = EdgeBranch
	followImageTag = "sha-$ARGOCD_APP_REVISION_SHORT"
)

// Argo is the Argo CD Application Matinee is installed from (kube.ArgoApp).
type Argo interface {
	Source(ctx context.Context) (kube.Source, error)
	SetSource(ctx context.Context, s kube.Source) error
}

// SettingStore keeps the settings (store.Store).
type SettingStore interface {
	Setting(ctx context.Context, key string) (string, error)
	SetSetting(ctx context.Context, key, value string) error
}

// Manager checks for updates and installs them.
type Manager struct {
	Store   SettingStore
	Log     *slog.Logger
	Build   Build
	Install Install
	GitHub  *GitHub
	// Argo is set for KindArgoCD.
	Argo Argo
	// Executable and Restart are for KindBinary: Restart stops the server
	// and starts the new one in its place.
	Executable string
	Restart    func()
	HTTP       *http.Client
	// Every is how often it looks; an hour by default.
	Every time.Duration
	Now   func() time.Time

	mu        sync.Mutex
	checked   time.Time
	available *Candidate
	lastError string
	applying  string // the version being installed
	since     time.Time
}

// applyingFor is how long an update shows as on its way. An Argo CD
// rollout that hasn't replaced this server by then didn't happen.
const applyingFor = 30 * time.Minute

// Status is what Settings shows.
type Status struct {
	Build     Build      `json:"build"`
	Install   Install    `json:"install"`
	Settings  Settings   `json:"settings"`
	Checked   time.Time  `json:"checked,omitzero"`
	Available *Candidate `json:"available,omitempty"`
	// Source is what the Argo CD Application installs now.
	Source *kube.Source `json:"source,omitempty"`
	// Following is whether Argo CD follows main by itself.
	Following bool   `json:"following,omitempty"`
	Applying  string `json:"applying,omitempty"`
	LastError string `json:"lastError,omitempty"`
}

func (m *Manager) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}

func (m *Manager) log() *slog.Logger {
	if m.Log != nil {
		return m.Log
	}
	return slog.Default()
}

// Settings reads the admin's settings, keeping them to what this install can do.
func (m *Manager) Settings(ctx context.Context) Settings {
	s := DefaultSettings
	if v, err := m.Store.Setting(ctx, settingKey); err == nil && v != "" {
		json.Unmarshal([]byte(v), &s)
	} else if err == nil && m.Argo != nil {
		// Until the admin chooses, an Application that follows edge is
		// main, installed automatically: that's what it does.
		if _, following := m.following(ctx); following {
			s = Settings{Channel: ChannelMain, Auto: true}
		}
	}
	return m.normalize(s)
}

func (m *Manager) normalize(s Settings) Settings {
	ok := false
	for _, c := range m.Install.Channels {
		ok = ok || c == s.Channel
	}
	if !ok {
		s.Channel = ChannelRelease
	}
	return s
}

// SaveSettings stores the settings and looks for updates on the channel.
// Under Argo CD it also sets the Application to match: following edge for
// main installed automatically, otherwise pinned to the version running now
// until an update is installed.
func (m *Manager) SaveSettings(ctx context.Context, s Settings) (Settings, error) {
	s = m.normalize(s)
	b, _ := json.Marshal(s)
	if err := m.Store.SetSetting(ctx, settingKey, string(b)); err != nil {
		return s, err
	}
	m.mu.Lock()
	m.available = nil
	m.checked = time.Time{}
	m.mu.Unlock()
	if m.Argo != nil {
		if err := m.pinArgo(ctx, s); err != nil {
			m.fail(err)
			return s, nil
		}
	}
	m.Check(ctx)
	return s, nil
}

func (m *Manager) pinArgo(ctx context.Context, s Settings) error {
	src, err := m.Argo.Source(ctx)
	if err != nil {
		return err
	}
	if s.Channel == ChannelMain && s.Auto {
		if src.TargetRevision == followRevision {
			return nil
		}
		m.log().Info("updates: Argo CD now follows main", "was", src.TargetRevision)
		return m.Argo.SetSource(ctx, kube.Source{TargetRevision: followRevision, ImageTag: followImageTag})
	}
	if src.TargetRevision != followRevision || !m.Build.Known() {
		return nil
	}
	// Stop following edge: stay on this version until an update is installed.
	pin := kube.Source{TargetRevision: m.Build.Revision(), ImageTag: m.Build.ImageTag()}
	m.log().Info("updates: Argo CD stays on this version", "revision", pin.TargetRevision)
	return m.Argo.SetSource(ctx, pin)
}

// Failed shows err in Settings.
func (m *Manager) Failed(err error) { m.fail(err) }

func (m *Manager) fail(err error) {
	m.log().Warn("updates", "err", err)
	m.mu.Lock()
	m.lastError = err.Error()
	m.mu.Unlock()
}

// following is whether Argo CD follows edge by itself, so there's nothing
// for Matinee to install.
func (m *Manager) following(ctx context.Context) (*kube.Source, bool) {
	if m.Argo == nil {
		return nil, false
	}
	src, err := m.Argo.Source(ctx)
	if err != nil {
		m.fail(err)
		return nil, false
	}
	return &src, src.TargetRevision == followRevision
}

// Check looks for a newer version now, and installs it if the admin chose
// automatic updates.
func (m *Manager) Check(ctx context.Context) {
	s := m.Settings(ctx)
	if len(m.Install.Channels) == 0 {
		return
	}
	c, err := m.GitHub.Find(ctx, m.Build, s.Channel)
	m.mu.Lock()
	m.checked = m.now()
	m.lastError = ""
	if err != nil {
		m.lastError = err.Error()
	} else {
		m.available = c
	}
	m.mu.Unlock()
	if err != nil {
		m.log().Warn("updates: couldn't look for a newer version", "err", err)
		return
	}
	if c == nil {
		return
	}
	m.log().Info("updates: a newer version is out", "version", c.Version, "channel", c.Channel)
	if !s.Auto || !m.Install.CanApply {
		return
	}
	if _, following := m.following(ctx); following {
		return
	}
	if err := m.Apply(ctx); err != nil {
		m.fail(err)
	}
}

// ErrNothing is Apply with no update found.
var ErrNothing = errors.New("there's no update to install")

// ErrCantApply is Apply where whatever installed Matinee updates it.
var ErrCantApply = errors.New("this server can't install updates itself")

// Apply installs the update that was found.
func (m *Manager) Apply(ctx context.Context) error {
	if !m.Install.CanApply {
		return ErrCantApply
	}
	m.mu.Lock()
	c := m.available
	if m.applying != "" && m.now().Sub(m.since) > applyingFor {
		m.applying = ""
	}
	if c == nil || m.applying != "" {
		m.mu.Unlock()
		if c == nil {
			return ErrNothing
		}
		return nil
	}
	m.applying, m.since = c.Version, m.now()
	m.lastError = ""
	m.mu.Unlock()

	var err error
	switch m.Install.Kind {
	case KindArgoCD:
		m.log().Info("updates: moving the Argo CD Application", "version", c.Version, "revision", c.Revision)
		err = m.Argo.SetSource(ctx, kube.Source{TargetRevision: c.Revision, ImageTag: c.ImageTag})
		// Argo CD rolls the new version out; this server stops when it's ready.
	case KindBinary:
		m.log().Info("updates: installing", "version", c.Version)
		err = replaceBinary(ctx, m.HTTP, c, m.Executable, runtime.GOOS, runtime.GOARCH, goarm())
		if err == nil && m.Restart != nil {
			m.log().Info("updates: restarting into the new version", "version", c.Version)
			go m.Restart()
		}
	default:
		err = ErrCantApply
	}
	if err != nil {
		m.mu.Lock()
		m.applying = ""
		m.mu.Unlock()
	}
	return err
}

// Status is what Settings shows.
func (m *Manager) Status(ctx context.Context) Status {
	st := Status{Build: m.Build, Install: m.Install, Settings: m.Settings(ctx)}
	st.Source, st.Following = m.following(ctx)
	m.mu.Lock()
	defer m.mu.Unlock()
	st.Checked, st.Available, st.Applying, st.LastError = m.checked, m.available, m.applying, m.lastError
	if st.Applying != "" && m.now().Sub(m.since) > applyingFor {
		st.Applying = ""
	}
	if st.Following && st.Available != nil && st.Available.Channel == ChannelMain {
		// Argo CD is already on its way to it.
		st.Available = nil
	}
	return st
}

// Run looks for updates a minute after the server starts, then every hour.
func (m *Manager) Run(ctx context.Context) {
	if len(m.Install.Channels) == 0 {
		return
	}
	every := m.Every
	if every <= 0 {
		every = time.Hour
	}
	t := time.NewTimer(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			m.Check(ctx)
			t.Reset(every)
		}
	}
}

func goarm() string {
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, s := range bi.Settings {
			if s.Key == "GOARM" {
				return s.Value
			}
		}
	}
	return "7"
}
