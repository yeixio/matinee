package plugin

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/tetratelabs/wazero"

	"github.com/yeixio/matinee/internal/metadata"
	"github.com/yeixio/matinee/internal/scan"
	"github.com/yeixio/matinee/internal/store"
)

// Limits on a plugin package.
const (
	MaxPackageSize  = 1 << 30 // the .matinee-plugin file
	maxUnpackedSize = 2 << 30
	maxFiles        = 20000
)

// Errors an admin can do something about.
var (
	ErrNotFound      = errors.New("no such plugin")
	ErrNotSupported  = errors.New("this plugin was made for a different version of Matinee")
	ErrKindTaken     = errors.New("another plugin already adds a library kind with this name")
	ErrBadPackage    = errors.New("not a Matinee plugin")
	ErrPackageTooBig = errors.New("the plugin is too large")
)

// Plugin is an installed plugin.
type Plugin struct {
	Manifest
	dir     string
	enabled bool
	// problem says why an enabled plugin isn't running.
	problem string
	config  map[string]string
	// approved are the network hosts the admin agreed to when turning the
	// plugin on. An update that asks for more turns it off until they agree
	// again.
	approved []string

	compiled wazero.CompiledModule
	kinds    []*libraryKind
	events   chan Event
	stop     chan struct{}
	// service runs lookups and events; it's started when first needed.
	serviceMu sync.Mutex
	service   *instance
}

// Running reports whether the plugin is on and working.
func (p *Plugin) Running() bool { return p.enabled && p.problem == "" }

// Manager keeps the installed plugins.
type Manager struct {
	// Dir holds one folder per plugin, named by its id, and a <id>.data
	// folder for what the plugin keeps.
	Dir   string
	Store *store.Store
	Log   *slog.Logger
	// OnChange runs after a plugin starts or stops, so the server can pick
	// up its library kinds and metadata.
	OnChange func()
	// Client fetches packages and catalogs; plugins' own fetches use a copy
	// that keeps redirects on their allowed hosts.
	Client *http.Client

	rt      *runtime
	mu      sync.RWMutex
	plugins map[string]*Plugin
	plays   playbackTracker
}

// Open loads the plugins in dir and starts the ones that are on.
func Open(ctx context.Context, dir string, st *store.Store, log *slog.Logger) (*Manager, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	m := &Manager{Dir: dir, Store: st, Log: log, Client: &http.Client{Timeout: 10 * time.Minute}, plugins: map[string]*Plugin{}}
	client := &http.Client{CheckRedirect: noRedirectsElsewhere}
	rt, err := newRuntime(ctx, filepath.Join(dir, ".cache"), client, log)
	if err != nil {
		return nil, err
	}
	m.rt = rt
	m.plays.emit = m.Emit
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") || strings.HasSuffix(e.Name(), ".data") {
			continue
		}
		p, err := m.load(ctx, filepath.Join(dir, e.Name()))
		if err != nil {
			log.Warn("skipping a plugin folder", "dir", e.Name(), "err", err)
			continue
		}
		m.plugins[p.ID] = p
	}
	ids := make([]string, 0, len(m.plugins))
	for id := range m.plugins {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if p := m.plugins[id]; p.enabled {
			m.start(ctx, p)
		}
	}
	return m, nil
}

// Close stops every plugin.
func (m *Manager) Close(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, p := range m.plugins {
		m.stopPlugin(ctx, p)
	}
	return m.rt.close(ctx)
}

type savedState struct {
	Enabled  bool              `json:"enabled"`
	Config   map[string]string `json:"config,omitempty"`
	Approved []string          `json:"approved,omitempty"`
}

func settingKey(id string) string { return "plugin:" + id }

func (m *Manager) load(ctx context.Context, dir string) (*Plugin, error) {
	b, err := os.ReadFile(filepath.Join(dir, "plugin.json"))
	if err != nil {
		return nil, err
	}
	man, err := ParseManifest(b)
	if err != nil {
		return nil, err
	}
	if filepath.Base(dir) != man.ID {
		return nil, fmt.Errorf("folder name doesn't match the plugin's id %q", man.ID)
	}
	p := &Plugin{Manifest: man, dir: dir, config: map[string]string{}}
	raw, err := m.Store.Setting(ctx, settingKey(man.ID))
	if err != nil {
		return nil, err
	}
	if raw != "" {
		var s savedState
		if err := json.Unmarshal([]byte(raw), &s); err == nil {
			p.enabled, p.approved = s.Enabled, s.Approved
			if s.Config != nil {
				p.config = s.Config
			}
		}
	}
	return p, nil
}

func (m *Manager) save(ctx context.Context, p *Plugin) error {
	b, _ := json.Marshal(savedState{Enabled: p.enabled, Config: p.config, Approved: p.approved})
	return m.Store.SetSetting(ctx, settingKey(p.ID), string(b))
}

// start runs an enabled plugin, or records why it can't. Call with m.mu
// held, or before anyone else can see m.
func (m *Manager) start(ctx context.Context, p *Plugin) {
	p.problem = ""
	if err := m.startPlugin(ctx, p); err != nil {
		p.problem = err.Error()
		m.stopPlugin(ctx, p)
		m.Log.Warn("plugin not started", "plugin", p.ID, "err", err)
		return
	}
	m.Log.Info("plugin started", "plugin", p.ID, "version", p.Version)
}

func (m *Manager) startPlugin(ctx context.Context, p *Plugin) error {
	if !p.Supported() {
		return ErrNotSupported
	}
	if extra := missing(p.Permissions.Network, p.approved); len(extra) > 0 {
		return fmt.Errorf("the plugin now asks to reach %s; turn it on again to allow it", strings.Join(extra, ", "))
	}
	for _, k := range p.LibraryKinds {
		if other, ok := scan.KindByName(k.Name); ok {
			if pk, mine := other.(*libraryKind); !mine || pk.p.ID != p.ID {
				return fmt.Errorf("%w: %s", ErrKindTaken, k.Name)
			}
		}
	}
	if p.Server != "" {
		wasm, err := os.ReadFile(filepath.Join(p.dir, filepath.FromSlash(p.Server)))
		if err != nil {
			return err
		}
		if p.compiled, err = m.rt.compile(ctx, wasm); err != nil {
			return err
		}
	}
	for _, k := range p.LibraryKinds {
		lk := &libraryKind{m: m, p: p, k: k, sessions: map[int64]*instance{}}
		p.kinds = append(p.kinds, lk)
		scan.Register(lk)
	}
	if len(p.Events) > 0 {
		p.events, p.stop = make(chan Event, 256), make(chan struct{})
		go m.deliver(p, p.events, p.stop)
	}
	return nil
}

// stopPlugin undoes startPlugin.
func (m *Manager) stopPlugin(ctx context.Context, p *Plugin) {
	for _, k := range p.kinds {
		if cur, ok := scan.KindByName(k.Name()); ok && cur == scan.Kind(k) {
			scan.Unregister(k.Name())
		}
	}
	p.kinds = nil
	if p.stop != nil {
		close(p.stop)
		p.stop, p.events = nil, nil
	}
	p.serviceMu.Lock()
	if p.service != nil {
		p.service.close(ctx)
		p.service = nil
	}
	p.serviceMu.Unlock()
	if p.compiled != nil {
		p.compiled.Close(ctx)
		p.compiled = nil
	}
}

func (m *Manager) changed() {
	if m.OnChange != nil {
		m.OnChange()
	}
}

// missing lists what's in want but not in have.
func missing(want, have []string) []string {
	var out []string
	for _, w := range want {
		if !slices.Contains(have, w) {
			out = append(out, w)
		}
	}
	return out
}

// View is a plugin as Settings shows it.
type View struct {
	Manifest
	Enabled bool `json:"enabled"`
	Running bool `json:"running"`
	// Problem says why an enabled plugin isn't running.
	Problem   string `json:"problem,omitempty"`
	Supported bool   `json:"supported"`
	// Config holds the admin's settings, without secret ones' values.
	Config map[string]string `json:"config"`
	// HasIcon means /plugins/{id}/icon has an image.
	HasIcon bool `json:"hasIcon"`
}

func (p *Plugin) view() View {
	cfg := map[string]string{}
	for _, s := range p.Settings {
		if v := p.config[s.Key]; v != "" {
			if s.Secret {
				v = "••••••"
			}
			cfg[s.Key] = v
		}
	}
	return View{
		Manifest: p.Manifest, Enabled: p.enabled, Running: p.Running(), Problem: p.problem,
		Supported: p.Supported(), Config: cfg, HasIcon: p.Icon != "",
	}
}

// List returns the installed plugins by name.
func (m *Manager) List() []View {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := []View{}
	for _, p := range m.plugins {
		out = append(out, p.view())
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out
}

// Get returns one installed plugin.
func (m *Manager) Get(id string) (View, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.plugins[id]
	if !ok {
		return View{}, ErrNotFound
	}
	return p.view(), nil
}

// SetEnabled turns a plugin on, allowing the network hosts it asks for, or
// off.
func (m *Manager) SetEnabled(ctx context.Context, id string, on bool) (View, error) {
	m.mu.Lock()
	p, ok := m.plugins[id]
	if !ok {
		m.mu.Unlock()
		return View{}, ErrNotFound
	}
	if on && !p.Supported() {
		m.mu.Unlock()
		return View{}, ErrNotSupported
	}
	m.stopPlugin(ctx, p)
	p.enabled, p.problem = on, ""
	if on {
		p.approved = slices.Clone(p.Permissions.Network)
		m.start(ctx, p)
	}
	err := m.save(ctx, p)
	v := p.view()
	m.mu.Unlock()
	m.changed()
	return v, err
}

// SetConfig changes a plugin's settings. Keys it doesn't have are ignored,
// and a secret left as it was shown ("••••••") keeps its value.
func (m *Manager) SetConfig(ctx context.Context, id string, cfg map[string]string) (View, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.plugins[id]
	if !ok {
		return View{}, ErrNotFound
	}
	next := map[string]string{}
	for _, s := range p.Settings {
		v, given := cfg[s.Key]
		v = strings.TrimSpace(v)
		if !given || s.Secret && v == "••••••" {
			v = p.config[s.Key]
		}
		if v != "" {
			next[s.Key] = v
		}
	}
	p.config = next
	return p.view(), m.save(ctx, p)
}

func (p *Plugin) configCopy() map[string]string {
	if len(p.config) == 0 {
		return nil
	}
	out := make(map[string]string, len(p.config))
	for k, v := range p.config {
		out[k] = v
	}
	return out
}

// Remove stops and deletes a plugin, its folder, its data and what its
// player kept for everyone.
func (m *Manager) Remove(ctx context.Context, id string) error {
	m.mu.Lock()
	p, ok := m.plugins[id]
	if !ok {
		m.mu.Unlock()
		return ErrNotFound
	}
	m.stopPlugin(ctx, p)
	delete(m.plugins, id)
	m.mu.Unlock()
	m.changed()
	if err := os.RemoveAll(p.dir); err != nil {
		return err
	}
	os.RemoveAll(m.dataDir(id))
	if err := m.Store.SetSetting(ctx, settingKey(id), ""); err != nil {
		return err
	}
	return m.Store.DeletePluginStorage(ctx, id)
}

func (m *Manager) dataDir(id string) string { return filepath.Join(m.Dir, id+".data") }

// Install unpacks a plugin package. A plugin that's already installed is
// replaced and stays on, unless the new version asks to reach more hosts.
// A new plugin starts off, so the admin can look at it first.
func (m *Manager) Install(ctx context.Context, pkg []byte) (View, error) {
	if len(pkg) > MaxPackageSize {
		return View{}, ErrPackageTooBig
	}
	zr, err := zip.NewReader(bytes.NewReader(pkg), int64(len(pkg)))
	if err != nil {
		return View{}, fmt.Errorf("%w: %v", ErrBadPackage, err)
	}
	man, err := readManifest(zr)
	if err != nil {
		return View{}, err
	}
	tmp, err := os.MkdirTemp(m.Dir, ".install-")
	if err != nil {
		return View{}, err
	}
	defer os.RemoveAll(tmp)
	if err := unpack(zr, tmp); err != nil {
		return View{}, err
	}
	for _, f := range append([]string{man.Server, man.Icon}, entries(man.Players)...) {
		if f == "" {
			continue
		}
		if info, err := os.Stat(filepath.Join(tmp, filepath.FromSlash(f))); err != nil || info.IsDir() {
			return View{}, fmt.Errorf("%w: plugin.json names %s, which isn't in the package", ErrBadPackage, f)
		}
	}
	if man.Server != "" {
		// Check the module before replacing anything.
		wasm, err := os.ReadFile(filepath.Join(tmp, filepath.FromSlash(man.Server)))
		if err != nil {
			return View{}, err
		}
		mod, err := m.rt.compile(ctx, wasm)
		if err != nil {
			return View{}, fmt.Errorf("%w: %v", ErrBadPackage, err)
		}
		mod.Close(ctx)
	}

	m.mu.Lock()
	old := m.plugins[man.ID]
	for _, other := range m.plugins {
		if other.ID == man.ID {
			continue
		}
		for _, k := range man.LibraryKinds {
			if slices.ContainsFunc(other.LibraryKinds, func(o LibraryKind) bool { return o.Name == k.Name }) {
				m.mu.Unlock()
				return View{}, fmt.Errorf("%w: %s (%s)", ErrKindTaken, k.Name, other.Name)
			}
		}
	}
	dir := filepath.Join(m.Dir, man.ID)
	if old != nil {
		m.stopPlugin(ctx, old)
	}
	aside := dir + ".old"
	os.RemoveAll(aside)
	if _, err := os.Stat(dir); err == nil {
		if err := os.Rename(dir, aside); err != nil {
			m.mu.Unlock()
			return View{}, err
		}
	}
	if err := os.Rename(tmp, dir); err != nil {
		os.Rename(aside, dir)
		m.mu.Unlock()
		return View{}, err
	}
	os.RemoveAll(aside)
	os.MkdirAll(m.dataDir(man.ID), 0o755)
	p, err := m.load(ctx, dir)
	if err != nil {
		m.mu.Unlock()
		return View{}, err
	}
	if old == nil {
		// A plugin installed again after being removed starts off too.
		p.enabled, p.approved = false, nil
	}
	m.plugins[p.ID] = p
	if p.enabled {
		m.start(ctx, p)
	}
	err = m.save(ctx, p)
	v := p.view()
	m.mu.Unlock()
	m.changed()
	m.Log.Info("plugin installed", "plugin", p.ID, "version", p.Version)
	return v, err
}

// Download fetches a plugin package from a URL.
func (m *Manager) Download(ctx context.Context, url string) ([]byte, error) {
	if !strings.HasPrefix(url, "https://") && !strings.HasPrefix(url, "http://") {
		return nil, fmt.Errorf("%w: the address must start with https://", ErrBadPackage)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	res, err := m.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s answered %s", req.URL.Host, res.Status)
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, MaxPackageSize+1))
	if err != nil {
		return nil, err
	}
	if len(b) > MaxPackageSize {
		return nil, ErrPackageTooBig
	}
	return b, nil
}

func readManifest(zr *zip.Reader) (Manifest, error) {
	for _, f := range zr.File {
		if f.Name != "plugin.json" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return Manifest{}, err
		}
		defer rc.Close()
		b, err := io.ReadAll(io.LimitReader(rc, 1<<20))
		if err != nil {
			return Manifest{}, err
		}
		return ParseManifest(b)
	}
	return Manifest{}, fmt.Errorf("%w: plugin.json is missing", ErrBadPackage)
}

// unpack writes a package's regular files under dir, refusing anything that
// would land outside it.
func unpack(zr *zip.Reader, dir string) error {
	if len(zr.File) > maxFiles {
		return ErrPackageTooBig
	}
	var total uint64
	for _, f := range zr.File {
		if strings.HasSuffix(f.Name, "/") {
			continue
		}
		if !safePath(f.Name) {
			return fmt.Errorf("%w: %q isn't a path inside the package", ErrBadPackage, f.Name)
		}
		if !f.Mode().IsRegular() {
			return fmt.Errorf("%w: %q isn't a regular file", ErrBadPackage, f.Name)
		}
		total += f.UncompressedSize64
		if total > maxUnpackedSize {
			return ErrPackageTooBig
		}
		dst := filepath.Join(dir, filepath.FromSlash(f.Name))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if err := writeFile(f, dst); err != nil {
			return err
		}
	}
	return nil
}

func writeFile(f *zip.File, dst string) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	// The sizes in a zip can lie; never write more than it claims.
	n, err := io.Copy(out, io.LimitReader(rc, int64(f.UncompressedSize64)+1))
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err == nil && uint64(n) > f.UncompressedSize64 {
		err = ErrPackageTooBig
	}
	return err
}

// LibraryKind is a library kind as the add-library form shows it.
type LibraryKindView struct {
	Name        string `json:"name"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
	Plugin      string `json:"plugin"`
}

// LibraryKinds lists the library kinds running plugins add.
func (m *Manager) LibraryKinds() []LibraryKindView {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := []LibraryKindView{}
	for _, p := range m.plugins {
		if !p.Running() {
			continue
		}
		for _, k := range p.LibraryKinds {
			out = append(out, LibraryKindView{Name: k.Name, Label: k.Label, Description: k.Description, Plugin: p.ID})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Label < out[j].Label })
	return out
}

// PlayerView is a player the web player can load.
type PlayerView struct {
	Plugin    string   `json:"plugin"`
	ItemKinds []string `json:"itemKinds"`
	Entry     string   `json:"entry"` // its path under /plugins/{plugin}/files/
}

// Players lists the players running plugins have.
func (m *Manager) Players() []PlayerView {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := []PlayerView{}
	for _, p := range m.plugins {
		if !p.Running() {
			continue
		}
		for _, pl := range p.Players {
			out = append(out, PlayerView{Plugin: p.ID, ItemKinds: pl.ItemKinds, Entry: pl.Entry})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Plugin < out[j].Plugin })
	return out
}

// PlayerFor returns the running plugin that plays items of a kind.
func (m *Manager) PlayerFor(itemKind string) (string, bool) {
	for _, pl := range m.Players() {
		if slices.Contains(pl.ItemKinds, itemKind) {
			return pl.Plugin, true
		}
	}
	return "", false
}

// File finds a file a running plugin serves to the web player: anything in
// its players' folders, and its icon.
func (m *Manager) File(id, name string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.plugins[id]
	if !ok || !safePath(name) {
		return "", false
	}
	allowed := name == p.Icon
	if p.Running() {
		for _, pl := range p.Players {
			if dir := pathDir(pl.Entry); dir == "" && name == pl.Entry || dir != "" && strings.HasPrefix(name, dir+"/") {
				allowed = true
			}
		}
	}
	if !allowed {
		return "", false
	}
	f := filepath.Join(p.dir, filepath.FromSlash(name))
	if info, err := os.Stat(f); err != nil || info.IsDir() {
		return "", false
	}
	return f, true
}

func pathDir(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[:i]
	}
	return ""
}

// PluginKinds lists the item kinds a running plugin files.
func (m *Manager) PluginKinds(id string) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if p, ok := m.plugins[id]; ok && p.Running() {
		return p.ItemKinds()
	}
	return nil
}

// LibraryKindPlugin returns the plugin that adds a library kind.
func (m *Manager) LibraryKindPlugin(kind string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, p := range m.plugins {
		for _, k := range p.LibraryKinds {
			if k.Name == kind {
				return p.ID, true
			}
		}
	}
	return "", false
}

// Providers lists the metadata providers running plugins add.
func (m *Manager) Providers() []metadata.Provider {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []metadata.Provider
	for _, p := range m.plugins {
		if p.Running() && len(p.Metadata) > 0 {
			out = append(out, &provider{m: m, p: p})
		}
	}
	return out
}

// serviceInstance returns the plugin's long-running instance for lookups
// and events, starting it if needed.
func (m *Manager) serviceInstance(ctx context.Context, p *Plugin) (*instance, error) {
	p.serviceMu.Lock()
	defer p.serviceMu.Unlock()
	if p.service != nil && !p.service.mod.IsClosed() {
		return p.service, nil
	}
	if p.compiled == nil {
		return nil, errors.New("the plugin isn't running")
	}
	in, err := m.rt.instantiate(ctx, p, m.baseMounts(p))
	if err != nil {
		return nil, err
	}
	p.service = in
	return in, nil
}

// baseMounts are what every instance of a plugin sees: its package, read
// only, and its own data folder.
func (m *Manager) baseMounts(p *Plugin) []mount {
	data := m.dataDir(p.ID)
	os.MkdirAll(data, 0o755)
	return []mount{{host: p.dir, guest: packageDir}, {host: data, guest: dataDir, writable: true}}
}

// Where a plugin finds its own package and data folder in the sandbox. They
// sit under one folder no library is likely to be in.
const (
	packageDir = "/_matinee/plugin"
	dataDir    = "/_matinee/data"
)
