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

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schema string

var ErrNotFound = errors.New("not found")

type Store struct {
	db *sql.DB
}

// Open opens (and if needed creates) the database at path. Use ":memory:" in tests.
func Open(path string) (*Store, error) {
	dsn := "file:" + path + "?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if path == ":memory:" {
		// Each connection to :memory: is its own database, so keep exactly one.
		db.SetMaxOpenConns(1)
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	var version int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version >= 1 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(schema); err != nil {
		return err
	}
	if _, err := tx.Exec("PRAGMA user_version = 1"); err != nil {
		return err
	}
	return tx.Commit()
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
	res, err := s.db.ExecContext(ctx,
		"INSERT INTO users (username, password_hash, is_admin, created_at) VALUES (?, ?, ?, ?)",
		username, passwordHash, admin, now())
	if err != nil {
		return User{}, err
	}
	id, _ := res.LastInsertId()
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
		"SELECT id, username, is_admin, password_hash FROM users WHERE username = ?", username))
}

func (s *Store) scanUser(row *sql.Row) (User, error) {
	var u User
	err := row.Scan(&u.ID, &u.Username, &u.IsAdmin, &u.hash)
	if errors.Is(err, sql.ErrNoRows) {
		return u, ErrNotFound
	}
	return u, err
}

func (s *Store) CreateSession(ctx context.Context, userID int64, ttl time.Duration) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	token := hex.EncodeToString(b)
	_, err := s.db.ExecContext(ctx, "INSERT INTO sessions (token, user_id, expires_at) VALUES (?, ?, ?)",
		token, userID, time.Now().Add(ttl).Unix())
	return token, err
}

func (s *Store) SessionUser(ctx context.Context, token string) (User, error) {
	return s.scanUser(s.db.QueryRowContext(ctx, `
		SELECT u.id, u.username, u.is_admin, u.password_hash
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token = ? AND s.expires_at > ?`, token, now()))
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
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Library{}, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, "INSERT INTO libraries (name, kind, created_at) VALUES (?, ?, ?)", name, kind, now())
	if err != nil {
		return Library{}, err
	}
	id, _ := res.LastInsertId()
	for _, p := range paths {
		if _, err := tx.ExecContext(ctx, "INSERT OR IGNORE INTO library_paths (library_id, path) VALUES (?, ?)", id, p); err != nil {
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
