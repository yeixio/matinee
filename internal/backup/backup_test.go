package backup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yeixio/matinee/internal/store"
)

func open(t *testing.T, path string, o store.Options) *store.Store {
	t.Helper()
	s, err := store.OpenWith(path, o)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestMakeListPrune(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	db := open(t, filepath.Join(dir, "matinee.db"), store.Options{})
	defer db.Close()
	clock := time.Date(2026, 11, 4, 5, 30, 0, 0, time.UTC)
	m := &Manager{Store: db, Dir: filepath.Join(dir, "backups"), DBPath: filepath.Join(dir, "matinee.db"), now: func() time.Time { return clock }}
	m.SaveSettings(ctx, Settings{EveryHours: 24, Keep: 2})

	// Due at once, then not again for a day.
	m.runDue(ctx)
	m.runDue(ctx)
	if list, _ := m.List(); len(list) != 1 || list[0].Reason != ReasonScheduled || list[0].Size == 0 {
		t.Fatalf("first: %+v", list)
	}
	for i := 0; i < 3; i++ {
		clock = clock.Add(25 * time.Hour)
		m.runDue(ctx)
	}
	clock = clock.Add(time.Minute)
	if _, err := m.Make(ctx, ReasonManual); err != nil {
		t.Fatal(err)
	}
	list, _ := m.List()
	if len(list) != 3 || list[0].Reason != ReasonManual || list[1].Reason != ReasonScheduled {
		t.Fatalf("kept: %+v", list)
	}
	// Two made in the same second get different names.
	if _, err := m.Make(ctx, ReasonManual); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Path("../matinee.db"); err == nil {
		t.Fatal("a path out of the folder")
	}
	if err := store.CheckSQLite(ctx, filepath.Join(m.Dir, list[0].Name)); err != nil {
		t.Fatal(err)
	}
}

// An upgrade backs up first; a restore swaps the database at the next start.
func TestUpgradeAndRestore(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	path := filepath.Join(dir, "matinee.db")
	backups := filepath.Join(dir, "backups")
	db := open(t, path, store.Options{})
	db.SetSetting(ctx, "marker", "before")
	m := &Manager{Store: db, Dir: backups, DBPath: path}
	b, err := m.Make(ctx, ReasonManual)
	if err != nil {
		t.Fatal(err)
	}
	db.SetSetting(ctx, "marker", "after")
	if err := m.Restore(ctx, b.Name); err != nil {
		t.Fatal(err)
	}
	if m.PendingRestore() != b.Name {
		t.Fatalf("pending: %q", m.PendingRestore())
	}
	db.Close()

	restored, err := ApplyRestore(ctx, path, backups, nil)
	if err != nil || !restored {
		t.Fatalf("apply: %v %v", restored, err)
	}
	db = open(t, path, store.Options{})
	if v, _ := db.Setting(ctx, "marker"); v != "before" {
		t.Fatalf("marker after restore: %q", v)
	}
	db.Close()
	list, _ := (&Manager{Dir: backups}).List()
	var kept bool
	for _, x := range list {
		if x.Reason == ReasonRestore {
			kept = true
			old := open(t, filepath.Join(backups, x.Name), store.Options{})
			if v, _ := old.Setting(ctx, "marker"); v != "after" {
				t.Fatalf("kept database: %q", v)
			}
			old.Close()
		}
	}
	if !kept {
		t.Fatalf("the database before the restore wasn't kept: %+v", list)
	}
	if again, _ := ApplyRestore(ctx, path, backups, nil); again {
		t.Fatal("restored twice")
	}

	// An older schema is backed up before it's upgraded. (The upgrade is
	// stopped here, since this database already has the newer schema.)
	db = open(t, path, store.Options{})
	db.SetSchemaVersionForTest(ctx, store.SchemaTarget()-1)
	db.Close()
	called := false
	up := &Manager{Dir: backups, DBPath: path}
	stop := errors.New("stop")
	_, err = store.OpenWith(path, store.Options{BeforeMigrate: func(s *store.Store, from, to int) error {
		called = from == store.SchemaTarget()-1 && to == store.SchemaTarget()
		if err := up.BeforeMigrate(s, from, to); err != nil {
			return err
		}
		return stop
	}})
	if !errors.Is(err, stop) {
		t.Fatalf("open: %v", err)
	}
	list, _ = up.List()
	if !called || list[0].Reason != ReasonUpgrade {
		t.Fatalf("upgrade: %v %+v", called, list)
	}
	os.Remove(filepath.Join(backups, list[0].Name))
}
