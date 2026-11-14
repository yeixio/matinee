package main

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/yeixio/matinee/internal/config"
	"github.com/yeixio/matinee/internal/store"
)

// A move an admin asked for happens at start; the server then runs on the
// new database, and keeps finding it through the old one after a restart.
// A move that fails leaves it on the old one, saying why.
func TestOpenDatabaseMoves(t *testing.T) {
	ctx := context.Background()
	cfg, err := config.Load([]string{"--data", t.TempDir(), "--database-url", ""})
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	old, err := store.Open(cfg.Database())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := old.CreateUser(ctx, "mike", "hash", true); err != nil {
		t.Fatal(err)
	}
	to := filepath.Join(t.TempDir(), "new", "matinee.db")
	if err := old.SetSetting(ctx, store.SettingMovePending, to); err != nil {
		t.Fatal(err)
	}
	old.Close()

	opened, err := openDatabase(ctx, cfg, log)
	if err != nil {
		t.Fatal(err)
	}
	if opened.dsn != to || opened.configured != cfg.Database() || opened.backups == nil || opened.backups.DBPath != to {
		t.Fatalf("opened %+v", opened)
	}
	if _, err := opened.db.UserByName(ctx, "mike"); err != nil {
		t.Errorf("the new database has no user: %v", err)
	}
	if moving.Load() != nil {
		t.Error("the waiting page still says it's moving")
	}
	opened.db.Close()

	again, err := openDatabase(ctx, cfg, log)
	if err != nil || again.dsn != to {
		t.Fatalf("after a restart: %+v, %v", again, err)
	}
	// Moving the new database back onto the old one, which isn't empty,
	// fails and changes nothing.
	again.db.SetSetting(ctx, store.SettingMovePending, cfg.Database())
	again.db.Close()
	third, err := openDatabase(ctx, cfg, log)
	if err != nil || third.dsn != to {
		t.Fatalf("after a failed move: %+v, %v", third, err)
	}
	defer third.db.Close()
	if why, _ := third.db.Setting(ctx, store.SettingMoveError); why == "" {
		t.Error("the failed move didn't say why")
	}
	if p, _ := third.db.Setting(ctx, store.SettingMovePending); p != "" {
		t.Errorf("the failed move is still pending: %q", p)
	}
}
