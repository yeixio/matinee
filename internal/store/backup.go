package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"time"
)

// ErrBackupUnsupported: Matinee copies SQLite databases itself; a
// PostgreSQL database is backed up with pg_dump.
var ErrBackupUnsupported = errors.New("backups are made for SQLite; back up PostgreSQL with pg_dump")

// Options change how Open opens the database.
type Options struct {
	// BeforeMigrate runs when an existing database is about to be changed
	// to a newer schema, from version from to version to, so it can be
	// backed up first. An error stops the start.
	BeforeMigrate func(s *Store, from, to int) error
	// Wait is how long to keep trying a PostgreSQL server that doesn't
	// answer yet, such as one starting at the same time as Matinee. Zero
	// tries once.
	Wait time.Duration
	// Waiting is told each time the database didn't answer and when the
	// next try is.
	Waiting func(err error, next time.Duration)
}

// SQLite says whether the database is a SQLite file (which Backup copies).
func (s *Store) SQLite() bool { return s.db.dialect == sqlite }

// Backup copies the whole database to path, a file that mustn't exist yet,
// while it stays in use (SQLite's VACUUM INTO).
func (s *Store) Backup(ctx context.Context, path string) error {
	if s.db.dialect != sqlite {
		return ErrBackupUnsupported
	}
	_, err := s.db.DB.ExecContext(ctx, "VACUUM INTO ?", path)
	return err
}

// SchemaTarget is the schema version this Matinee migrates databases to.
func SchemaTarget() int { return len(migrations) + 1 }

// schemaVersion is the database's schema version, 0 for a new one.
func (s *Store) schemaVersion(ctx context.Context) int {
	var v int
	if err := s.db.DB.QueryRowContext(ctx, "SELECT COALESCE(MAX(version), 0) FROM schema_version").Scan(&v); err != nil {
		return 0
	}
	return v
}

// CopySQLite copies the SQLite database at from (with anything still in its
// write-ahead log) to the new file to, without Matinee having it open.
func CopySQLite(ctx context.Context, from, to string) error {
	if _, err := os.Stat(from); err != nil {
		return err
	}
	conn, err := sql.Open("sqlite", "file:"+from+"?_pragma=busy_timeout(5000)")
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "VACUUM INTO ?", to); err != nil {
		return fmt.Errorf("copy %s: %w", from, err)
	}
	return nil
}

// CheckSQLite says whether the file at path is a SQLite database Matinee
// made, as one being restored must be.
func CheckSQLite(ctx context.Context, path string) error {
	conn, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return err
	}
	defer conn.Close()
	var ok string
	if err := conn.QueryRowContext(ctx, "PRAGMA quick_check").Scan(&ok); err != nil {
		return fmt.Errorf("not a database: %w", err)
	}
	if ok != "ok" {
		return fmt.Errorf("damaged: %s", ok)
	}
	var n int
	if err := conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_version").Scan(&n); err != nil {
		return errors.New("not a Matinee database")
	}
	return nil
}

// SetSchemaVersionForTest pretends the database has an older schema, for
// tests of what happens before an upgrade. Its migrations run again.
func (s *Store) SetSchemaVersionForTest(ctx context.Context, v int) error {
	_, err := s.db.ExecContext(ctx, "UPDATE schema_version SET version = ?", v)
	return err
}
