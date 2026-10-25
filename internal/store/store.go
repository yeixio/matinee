// Package store keeps all server state in a single SQLite database.
package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

//go:embed schema_sqlite.sql
var schemaSQLite string

//go:embed schema_postgres.sql
var schemaPostgres string

var ErrNotFound = errors.New("not found")

type Store struct {
	db *db
}

// Open connects to the database. A postgres:// URL uses PostgreSQL, which
// suits Kubernetes and network storage; anything else is a SQLite file path
// (":memory:" in tests).
func Open(dsn string) (*Store, error) {
	var d *db
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		conn, err := sql.Open("pgx", dsn)
		if err != nil {
			return nil, err
		}
		conn.SetMaxOpenConns(20)
		d = &db{DB: conn, dialect: postgres}
	} else {
		conn, err := sql.Open("sqlite", "file:"+dsn+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)")
		if err != nil {
			return nil, err
		}
		if dsn == ":memory:" {
			// Each connection to :memory: is its own database, so keep exactly one.
			conn.SetMaxOpenConns(1)
		}
		d = &db{DB: conn, dialect: sqlite}
	}
	s := &Store{db: d}
	if err := s.migrate(); err != nil {
		d.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

// Ping checks the database is reachable, for readiness probes.
func (s *Store) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }

func (s *Store) migrate() error {
	ctx := context.Background()
	t, err := s.db.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer t.Rollback()
	schema := schemaSQLite
	if s.db.dialect == postgres {
		schema = schemaPostgres
		// Several pods may start at once; only one migrates.
		if _, err := t.ExecContext(ctx, "SELECT pg_advisory_xact_lock(7171)"); err != nil {
			return err
		}
	}
	if _, err := t.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS schema_version (version INTEGER NOT NULL)"); err != nil {
		return err
	}
	var version int
	if err := t.QueryRowContext(ctx, "SELECT COALESCE(MAX(version), 0) FROM schema_version").Scan(&version); err != nil {
		return err
	}
	if version < 1 {
		if _, err := t.ExecContext(ctx, schema); err != nil {
			return err
		}
		version = 1
	}
	if version > len(migrations) {
		return t.Commit() // up to date, or made by a newer Matinee
	}
	// Later changes, in order. Each runs once on databases made before it.
	for _, m := range migrations[version-1:] {
		if _, err := t.ExecContext(ctx, m); err != nil {
			return err
		}
		version++
	}
	if _, err := t.ExecContext(ctx, "DELETE FROM schema_version"); err != nil {
		return err
	}
	if _, err := t.ExecContext(ctx, "INSERT INTO schema_version (version) VALUES (?)", version); err != nil {
		return err
	}
	return t.Commit()
}

// migrations take the schema from version 1 (the schema files) upward. They
// must work on both SQLite and PostgreSQL.
var migrations = []string{
	// 2: embedded subtitle streams as JSON; NULL until the file is probed for them.
	"ALTER TABLE items ADD COLUMN subtitle_streams TEXT",
	// 3–6: sessions say which device they belong to, so people can see and sign out their devices.
	"ALTER TABLE sessions ADD COLUMN device_name TEXT NOT NULL DEFAULT ''",
	"ALTER TABLE sessions ADD COLUMN platform TEXT NOT NULL DEFAULT ''",
	"ALTER TABLE sessions ADD COLUMN created_at BIGINT NOT NULL DEFAULT 0",
	"ALTER TABLE sessions ADD COLUMN last_seen_at BIGINT NOT NULL DEFAULT 0",
	// 7: pending sign-ins from devices that show a code (see devices.go).
	`CREATE TABLE device_codes (
		device_code TEXT PRIMARY KEY,
		user_code   TEXT NOT NULL UNIQUE,
		device_name TEXT NOT NULL,
		platform    TEXT NOT NULL,
		user_id     BIGINT REFERENCES users(id) ON DELETE CASCADE,
		denied      INTEGER NOT NULL DEFAULT 0,
		expires_at  BIGINT NOT NULL
	)`,
}

func now() int64 { return time.Now().Unix() }

// SortTitle drops a leading article so "The Matrix" sorts under M.
func SortTitle(title string) string {
	t := strings.ToLower(strings.TrimSpace(title))
	for _, a := range []string{"the ", "a ", "an "} {
		if strings.HasPrefix(t, a) && len(t) > len(a) {
			return t[len(a):]
		}
	}
	return t
}

// ---- users and sessions ----

type User struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	IsAdmin  bool   `json:"isAdmin"`
	hash     string
}

func (u User) PasswordHash() string { return u.hash }

func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM users").Scan(&n)
	return n, err
}

func (s *Store) CreateUser(ctx context.Context, username, passwordHash string, admin bool) (User, error) {
	var id int64
	err := s.db.QueryRowContext(ctx,
		"INSERT INTO users (username, password_hash, is_admin, created_at) VALUES (?, ?, ?, ?) RETURNING id",
		username, passwordHash, b2i(admin), now()).Scan(&id)
	if err != nil {
		return User{}, err
	}
	return User{ID: id, Username: username, IsAdmin: admin, hash: passwordHash}, nil
}

func (s *Store) Users(ctx context.Context) ([]User, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id, username, is_admin, password_hash FROM users ORDER BY username")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Username, &u.IsAdmin, &u.hash); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Store) UserByName(ctx context.Context, username string) (User, error) {
	return s.scanUser(s.db.QueryRowContext(ctx,
		"SELECT id, username, is_admin, password_hash FROM users WHERE lower(username) = lower(?)", username))
}

func (s *Store) scanUser(row *sql.Row) (User, error) {
	var u User
	err := row.Scan(&u.ID, &u.Username, &u.IsAdmin, &u.hash)
	if errors.Is(err, sql.ErrNoRows) {
		return u, ErrNotFound
	}
	return u, err
}

// Device names the app or browser a session belongs to.
type Device struct {
	Name     string
	Platform string // ios, android, web, tv…
}

func (s *Store) CreateSession(ctx context.Context, userID int64, ttl time.Duration, dev Device) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	token := hex.EncodeToString(b)
	_, err := s.db.ExecContext(ctx, `INSERT INTO sessions (token, user_id, expires_at, device_name, platform, created_at, last_seen_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		token, userID, time.Now().Add(ttl).Unix(), dev.Name, dev.Platform, now(), now())
	return token, err
}

// seenEvery is how stale last_seen_at may get, so requests don't each write to the database.
const seenEvery = 5 * 60

func (s *Store) SessionUser(ctx context.Context, token string) (User, error) {
	var u User
	var seen int64
	err := s.db.QueryRowContext(ctx, `
		SELECT u.id, u.username, u.is_admin, u.password_hash, s.last_seen_at
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token = ? AND s.expires_at > ?`, token, now()).Scan(&u.ID, &u.Username, &u.IsAdmin, &u.hash, &seen)
	if errors.Is(err, sql.ErrNoRows) {
		return u, ErrNotFound
	}
	if err != nil {
		return u, err
	}
	if now()-seen > seenEvery {
		s.db.ExecContext(ctx, "UPDATE sessions SET last_seen_at = ? WHERE token = ?", now(), token)
	}
	return u, nil
}

func (s *Store) DeleteSession(ctx context.Context, token string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM sessions WHERE token = ?", token)
	return err
}

// ---- libraries ----

type Library struct {
	ID    int64    `json:"id"`
	Name  string   `json:"name"`
	Kind  string   `json:"kind"` // movies, tv or music
	Paths []string `json:"paths"`
}

func (s *Store) CreateLibrary(ctx context.Context, name, kind string, paths []string) (Library, error) {
	tx, err := s.db.BeginTx(ctx)
	if err != nil {
		return Library{}, err
	}
	defer tx.Rollback()
	var id int64
	err = tx.QueryRowContext(ctx, "INSERT INTO libraries (name, kind, created_at) VALUES (?, ?, ?) RETURNING id", name, kind, now()).Scan(&id)
	if err != nil {
		return Library{}, err
	}
	for _, p := range paths {
		if _, err := tx.ExecContext(ctx, "INSERT INTO library_paths (library_id, path) VALUES (?, ?) ON CONFLICT DO NOTHING", id, p); err != nil {
			return Library{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return Library{}, err
	}
	return Library{ID: id, Name: name, Kind: kind, Paths: paths}, nil
}

func (s *Store) Libraries(ctx context.Context) ([]Library, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id, name, kind FROM libraries ORDER BY name")
	if err != nil {
		return nil, err
	}
	out := []Library{}
	byID := map[int64]int{}
	for rows.Next() {
		l := Library{Paths: []string{}}
		if err := rows.Scan(&l.ID, &l.Name, &l.Kind); err != nil {
			rows.Close()
			return nil, err
		}
		byID[l.ID] = len(out)
		out = append(out, l)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	prows, err := s.db.QueryContext(ctx, "SELECT library_id, path FROM library_paths ORDER BY path")
	if err != nil {
		return nil, err
	}
	defer prows.Close()
	for prows.Next() {
		var id int64
		var p string
		if err := prows.Scan(&id, &p); err != nil {
			return nil, err
		}
		if i, ok := byID[id]; ok {
			out[i].Paths = append(out[i].Paths, p)
		}
	}
	return out, prows.Err()
}

func (s *Store) Library(ctx context.Context, id int64) (Library, error) {
	libs, err := s.Libraries(ctx)
	if err != nil {
		return Library{}, err
	}
	for _, l := range libs {
		if l.ID == id {
			return l, nil
		}
	}
	return Library{}, ErrNotFound
}

func (s *Store) DeleteLibrary(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, "DELETE FROM libraries WHERE id = ?", id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
