// Package storetest opens a throwaway database for tests: in-memory SQLite by
// default, or a fresh PostgreSQL database when MATINEE_TEST_DATABASE_URL is set.
package storetest

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"net/url"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/yeixio/matinee/internal/store"
)

func Open(t *testing.T) *store.Store {
	t.Helper()
	dsn := ":memory:"
	if admin := os.Getenv("MATINEE_TEST_DATABASE_URL"); admin != "" {
		dsn = freshPostgres(t, admin)
	}
	st, err := store.Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func freshPostgres(t *testing.T, admin string) string {
	t.Helper()
	b := make([]byte, 6)
	rand.Read(b)
	name := "matinee_test_" + hex.EncodeToString(b)
	conn, err := sql.Open("pgx", admin)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec("CREATE DATABASE " + name); err != nil {
		t.Fatalf("create test database: %v", err)
	}
	u, err := url.Parse(admin)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + name
	t.Cleanup(func() {
		// Runs after the store closes (cleanups run last-in, first-out).
		conn.Exec("DROP DATABASE IF EXISTS " + name + " WITH (FORCE)")
		conn.Close()
	})
	return u.String()
}
