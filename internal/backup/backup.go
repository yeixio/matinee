// Package backup keeps copies of the server's SQLite database: before the
// schema changes in an upgrade, every day (by default), and whenever an
// admin asks. An admin can download one or restore it, which takes effect
// the next time the server starts. A PostgreSQL database is backed up with
// pg_dump instead.
package backup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/yeixio/matinee/internal/store"
)

// Why a backup was made.
const (
	ReasonScheduled = "scheduled"
	ReasonManual    = "manual"
	ReasonUpgrade   = "upgrade"
	// The database as it was before a restore replaced it.
	ReasonRestore = "before-restore"
)

// Settings is how often backups are made and how many are kept.
type Settings struct {
	// EveryHours between scheduled backups; 0 turns them off.
	EveryHours int `json:"everyHours"`
	// Keep is how many of each kind are kept; older ones are deleted.
	Keep int `json:"keep"`
}

// DefaultSettings: a backup a day, a week of them.
var DefaultSettings = Settings{EveryHours: 24, Keep: 7}

const settingKey = "backups"

// Normalize keeps the settings in range.
func (s *Settings) Normalize() {
	s.EveryHours = min(max(s.EveryHours, 0), 24*30)
	if s.Keep < 1 {
		s.Keep = DefaultSettings.Keep
	}
	s.Keep = min(s.Keep, 100)
}

// Backup is one copy of the database.
type Backup struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	Time   int64  `json:"time"` // milliseconds
	Reason string `json:"reason"`
}

// Names look like matinee-20261104-053000-scheduled.db, in UTC.
var nameRE = regexp.MustCompile(`^matinee-(\d{8}-\d{6})-(scheduled|manual|upgrade|before-restore)(?:-\d+)?\.db$`)

const timeLayout = "20060102-150405"

// ValidName says whether name is one of the backups' names (and nothing
// that leads anywhere else).
func ValidName(name string) bool { return nameRE.MatchString(name) }

func parse(name string) (time.Time, string, bool) {
	m := nameRE.FindStringSubmatch(name)
	if m == nil {
		return time.Time{}, "", false
	}
	t, err := time.ParseInLocation(timeLayout, m[1], time.UTC)
	return t, m[2], err == nil
}

// Manager makes, lists and restores backups in Dir.
type Manager struct {
	Store *store.Store
	// Dir holds the backups; DBPath is the database file.
	Dir    string
	DBPath string
	Log    *slog.Logger
	// Failed hears about a backup that failed, for an admin alert.
	Failed func(ctx context.Context, err error)

	now func() time.Time
	mu  sync.Mutex
	// LastError is why the last backup failed, until one works.
	lastError string
}

func (m *Manager) clock() time.Time {
	if m.now != nil {
		return m.now()
	}
	return time.Now()
}

// Supported says whether this server backs up its own database.
func (m *Manager) Supported() bool { return m != nil && m.Store != nil && m.Store.SQLite() }

// Settings reads the admin's settings.
func (m *Manager) Settings(ctx context.Context) Settings {
	s := DefaultSettings
	if v, err := m.Store.Setting(ctx, settingKey); err == nil && v != "" {
		json.Unmarshal([]byte(v), &s)
	}
	s.Normalize()
	return s
}

// SaveSettings stores them.
func (m *Manager) SaveSettings(ctx context.Context, s Settings) (Settings, error) {
	s.Normalize()
	b, _ := json.Marshal(s)
	return s, m.Store.SetSetting(ctx, settingKey, string(b))
}

// List is the backups, newest first.
func (m *Manager) List() ([]Backup, error) {
	entries, err := os.ReadDir(m.Dir)
	if errors.Is(err, os.ErrNotExist) {
		return []Backup{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := []Backup{}
	for _, e := range entries {
		t, reason, ok := parse(e.Name())
		if !ok || !e.Type().IsRegular() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, Backup{Name: e.Name(), Size: info.Size(), Time: t.UnixMilli(), Reason: reason})
	}
	slices.SortFunc(out, func(a, b Backup) int {
		if a.Time != b.Time {
			return int(b.Time - a.Time)
		}
		return -compareStrings(a.Name, b.Name)
	})
	return out, nil
}

func compareStrings(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// LastError is why the latest backup failed, or "".
func (m *Manager) LastError() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastError
}

// Make backs up the database now.
func (m *Manager) Make(ctx context.Context, reason string) (Backup, error) {
	b, err := m.make(ctx, reason)
	m.mu.Lock()
	if err != nil {
		m.lastError = err.Error()
	} else {
		m.lastError = ""
	}
	m.mu.Unlock()
	if err != nil {
		if m.Log != nil {
			m.Log.Error("database backup failed", "reason", reason, "err", err)
		}
		if m.Failed != nil {
			m.Failed(ctx, err)
		}
		return b, err
	}
	if m.Log != nil {
		m.Log.Info("backed up the database", "file", filepath.Join(m.Dir, b.Name), "reason", reason, "bytes", b.Size)
	}
	m.prune(m.Settings(ctx).Keep)
	return b, nil
}

func (m *Manager) make(ctx context.Context, reason string) (Backup, error) {
	if !m.Supported() {
		return Backup{}, store.ErrBackupUnsupported
	}
	if err := os.MkdirAll(m.Dir, 0o700); err != nil {
		return Backup{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	name, path := m.newName(reason)
	tmp := path + ".part"
	os.Remove(tmp)
	if err := m.Store.Backup(ctx, tmp); err != nil {
		os.Remove(tmp)
		return Backup{}, err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return Backup{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return Backup{}, err
	}
	t, _, _ := parse(name)
	return Backup{Name: name, Size: info.Size(), Time: t.UnixMilli(), Reason: reason}, nil
}

// newName is a name no backup has yet. m.mu is held.
func (m *Manager) newName(reason string) (string, string) {
	base := "matinee-" + m.clock().UTC().Format(timeLayout) + "-" + reason
	name := base + ".db"
	for i := 2; ; i++ {
		if _, err := os.Stat(filepath.Join(m.Dir, name)); errors.Is(err, os.ErrNotExist) {
			return name, filepath.Join(m.Dir, name)
		}
		name = base + "-" + strconv.Itoa(i) + ".db"
	}
}

// prune keeps the newest keep backups of each reason.
func (m *Manager) prune(keep int) {
	list, err := m.List()
	if err != nil {
		return
	}
	count := map[string]int{}
	for _, b := range list {
		count[b.Reason]++
		if count[b.Reason] > keep {
			if err := os.Remove(filepath.Join(m.Dir, b.Name)); err == nil && m.Log != nil {
				m.Log.Info("deleted an old database backup", "file", b.Name)
			}
		}
	}
}

// Path is the file of the backup called name.
func (m *Manager) Path(name string) (string, error) {
	if !ValidName(name) {
		return "", os.ErrNotExist
	}
	p := filepath.Join(m.Dir, name)
	if _, err := os.Stat(p); err != nil {
		return "", err
	}
	return p, nil
}

// Delete removes a backup.
func (m *Manager) Delete(name string) error {
	p, err := m.Path(name)
	if err != nil {
		return err
	}
	return os.Remove(p)
}

// BeforeMigrate backs up the database before an upgrade changes it; it's
// store.Options.BeforeMigrate. A backup that fails stops the upgrade, so
// the old database stays as it was.
func (m *Manager) BeforeMigrate(s *store.Store, from, to int) error {
	if !s.SQLite() {
		return nil
	}
	m.Store = s
	if m.Log != nil {
		m.Log.Info("the database is about to be upgraded; backing it up first", "from", from, "to", to)
	}
	b, err := m.make(context.Background(), ReasonUpgrade)
	if err != nil {
		return fmt.Errorf("back up the database before upgrading it: %w (free some space in %s, or start the older version)", err, m.Dir)
	}
	if m.Log != nil {
		m.Log.Info("backed up the database before upgrading it", "file", filepath.Join(m.Dir, b.Name))
	}
	return nil
}

// Run makes a scheduled backup whenever one is due, until ctx ends.
func (m *Manager) Run(ctx context.Context) {
	if !m.Supported() {
		return
	}
	t := time.NewTicker(10 * time.Minute)
	defer t.Stop()
	for {
		m.runDue(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// runDue makes a scheduled backup if the last one is old enough.
func (m *Manager) runDue(ctx context.Context) {
	s := m.Settings(ctx)
	if s.EveryHours == 0 {
		return
	}
	list, err := m.List()
	if err != nil {
		m.Make(ctx, ReasonScheduled) // says why it fails
		return
	}
	for _, b := range list {
		if b.Reason == ReasonScheduled || b.Reason == ReasonManual {
			if m.clock().Sub(time.UnixMilli(b.Time)) < time.Duration(s.EveryHours)*time.Hour {
				return
			}
			break
		}
	}
	m.Make(ctx, ReasonScheduled)
}

// pending is the file a restore waits in until the next start.
func pending(dbPath string) string { return dbPath + ".restore" }

// Restore puts the backup called name in place of the database the next
// time the server starts.
func (m *Manager) Restore(ctx context.Context, name string) error {
	p, err := m.Path(name)
	if err != nil {
		return err
	}
	if err := store.CheckSQLite(ctx, p); err != nil {
		return err
	}
	if err := copyFile(p, pending(m.DBPath)+".part"); err != nil {
		return err
	}
	if err := os.WriteFile(pending(m.DBPath)+".name", []byte(name), 0o600); err != nil {
		return err
	}
	return os.Rename(pending(m.DBPath)+".part", pending(m.DBPath))
}

// PendingRestore is the backup that replaces the database at the next
// start, or "".
func (m *Manager) PendingRestore() string {
	if _, err := os.Stat(pending(m.DBPath)); err != nil {
		return ""
	}
	name, _ := os.ReadFile(pending(m.DBPath) + ".name")
	return string(name)
}

// CancelRestore keeps the database as it is.
func (m *Manager) CancelRestore() error {
	os.Remove(pending(m.DBPath) + ".name")
	err := os.Remove(pending(m.DBPath))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// ApplyRestore, before the database is opened, puts a restore an admin
// asked for in place: the database as it is goes to the backups first.
// It reports whether it restored one.
func ApplyRestore(ctx context.Context, dbPath, dir string, log *slog.Logger) (bool, error) {
	from := pending(dbPath)
	if _, err := os.Stat(from); errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	name, _ := os.ReadFile(from + ".name")
	if _, err := os.Stat(dbPath); err == nil {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return false, err
		}
		m := &Manager{Dir: dir}
		keep, _ := m.newName(ReasonRestore)
		if err := store.CopySQLite(ctx, dbPath, filepath.Join(dir, keep)); err != nil {
			return false, fmt.Errorf("keep the database before restoring: %w", err)
		}
		if log != nil {
			log.Info("kept the database as it was before the restore", "file", filepath.Join(dir, keep))
		}
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if err := os.Remove(dbPath + suffix); err != nil && !errors.Is(err, os.ErrNotExist) {
			return false, err
		}
	}
	if err := os.Rename(from, dbPath); err != nil {
		return false, err
	}
	os.Remove(from + ".name")
	if log != nil {
		log.Info("restored the database from a backup", "backup", string(name))
	}
	return true, nil
}

func copyFile(from, to string) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(to, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(to)
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
