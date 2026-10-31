package store

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
)

type dialect int

const (
	sqlite dialect = iota
	postgres
)

// db runs queries written with SQLite-style ? placeholders on either
// database, rewriting them to $1, $2… for PostgreSQL.
type db struct {
	*sql.DB
	dialect dialect
}

func (d *db) rebind(q string) string {
	if d.dialect != postgres || !strings.Contains(q, "?") {
		return q
	}
	var b strings.Builder
	n := 0
	for _, r := range q {
		if r == '?' {
			n++
			b.WriteByte('$')
			b.WriteString(strconv.Itoa(n))
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func (d *db) ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error) {
	return d.DB.ExecContext(ctx, d.rebind(q), args...)
}

func (d *db) QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error) {
	return d.DB.QueryContext(ctx, d.rebind(q), args...)
}

func (d *db) QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row {
	return d.DB.QueryRowContext(ctx, d.rebind(q), args...)
}

func (d *db) BeginTx(ctx context.Context) (*tx, error) {
	t, err := d.DB.BeginTx(ctx, nil)
	return &tx{Tx: t, d: d}, err
}

type tx struct {
	*sql.Tx
	d *db
}

func (t *tx) ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error) {
	return t.Tx.ExecContext(ctx, t.d.rebind(q), args...)
}

func (t *tx) QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error) {
	return t.Tx.QueryContext(ctx, t.d.rebind(q), args...)
}

func (t *tx) QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row {
	return t.Tx.QueryRowContext(ctx, t.d.rebind(q), args...)
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}
