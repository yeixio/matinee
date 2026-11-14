package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// Moving the database: copying everything in Matinee's database to another
// one (SQLite to PostgreSQL, PostgreSQL to another PostgreSQL server, or
// back to a SQLite file), checking the copy has every row, and switching
// over to it. The old database keeps all its data; it only gets a note
// saying where the database went (SettingMovedTo), so a server still set up
// with the old one finds the new one, until its settings say the new one.

// Settings that belong to one database and aren't copied to the new one.
const (
	// SettingMovedTo, in the old database, is the new database's address.
	SettingMovedTo = "database_moved_to"
	// SettingMovedFrom, in the new database, says where it was moved from
	// (without a password) and when, as JSON.
	SettingMovedFrom = "database_moved_from"
	// SettingMovePending is a move an admin asked for in Settings, which
	// the server makes the next time it starts, before anything else uses
	// the database.
	SettingMovePending = "database_move_pending"
	// SettingMoveError is why the last move failed.
	SettingMoveError = "database_move_error"
)

const movePrefix = "database_move"

// IsPostgres says whether dsn is a PostgreSQL URL (otherwise it's a SQLite file).
func IsPostgres(dsn string) bool {
	return strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://")
}

// Describe is where dsn is, for people to read: a PostgreSQL URL without
// its password, or a SQLite file's path.
func Describe(dsn string) string {
	if !IsPostgres(dsn) {
		return dsn
	}
	u, err := url.Parse(dsn)
	if err != nil {
		return "a PostgreSQL database"
	}
	if _, ok := u.User.Password(); ok {
		u.User = url.UserPassword(u.User.Username(), "xxxxx")
	}
	q := u.Query()
	q.Del("password")
	u.RawQuery = q.Encode()
	return u.String()
}

// TableCount is how many rows a table has.
type TableCount struct {
	Table string `json:"table"`
	Rows  int64  `json:"rows"`
}

// MoveReport is what a move copied.
type MoveReport struct {
	From   string        `json:"from"`
	To     string        `json:"to"`
	Tables []TableCount  `json:"tables"`
	Rows   int64         `json:"rows"`
	Took   time.Duration `json:"took"`
}

// MoveProgress hears how far a move is: rows copied out of all of them.
type MoveProgress func(table string, done, total int64)

// ErrNotEmpty: the database to move to already has Matinee's data in it.
var ErrNotEmpty = errors.New("that database already has Matinee's data in it; move to an empty database")

// table is one of Matinee's tables, as the copy needs to know it.
type table struct {
	name    string
	columns []column
	// refs are the tables its rows refer to, which are copied first.
	refs []string
	// parents are columns referring to the same table's key (pk), such as
	// an episode's season, or the file a version or a part belongs to; the
	// rows they refer to are copied first.
	parents []string
	pk      string
}

type column struct {
	name     string
	bytes    bool // BYTEA or BLOB
	identity bool // PostgreSQL makes its values; the sequence is moved on after the copy
}

// CheckMoveTarget says whether dsn is somewhere Matinee can move this
// database to: it answers, isn't this database, and has none of Matinee's
// data yet. A SQLite file that doesn't exist yet is fine; it's made.
func (s *Store) CheckMoveTarget(ctx context.Context, dsn string) error {
	to, made, err := openTarget(ctx, dsn)
	if err != nil {
		return err
	}
	defer cleanTarget(to, dsn, made, true)
	_, err = s.prepareMove(ctx, to)
	return err
}

// MoveTo copies this database to the one at dsn, which must be empty, and
// checks every table has the same number of rows in both. The tables are
// made by Matinee's own schema and migrations, in one transaction with the
// copy, so a move that fails leaves nothing behind. This database isn't
// changed. Nothing else should write to it meanwhile: what's written
// during the copy isn't in it (the server moves at start, before anything
// else uses the database, and the command asks for the server to be
// stopped).
func (s *Store) MoveTo(ctx context.Context, dsn string, progress MoveProgress) (MoveReport, error) {
	start := time.Now()
	report := MoveReport{To: Describe(dsn), Tables: []TableCount{}}
	to, made, err := openTarget(ctx, dsn)
	if err != nil {
		return report, err
	}
	ok := false
	defer func() { cleanTarget(to, dsn, made, !ok) }()

	tables, err := s.prepareMove(ctx, to)
	if err != nil {
		return report, err
	}
	// A snapshot of this database, so every table is copied as it was at
	// one moment.
	opts := &sql.TxOptions{}
	if s.db.dialect == postgres {
		opts = &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true}
	}
	src, err := s.db.DB.BeginTx(ctx, opts)
	if err != nil {
		return report, err
	}
	defer src.Rollback()
	srcTx := &tx{Tx: src, d: s.db}

	counts := make(map[string]int64, len(tables))
	var total int64
	for _, t := range tables {
		var n int64
		if err := srcTx.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+quote(t.name)+where(t)).Scan(&n); err != nil {
			return report, fmt.Errorf("count %s: %w", t.name, err)
		}
		counts[t.name] = n
		total += n
	}

	dst, err := to.db.BeginTx(ctx)
	if err != nil {
		return report, err
	}
	defer dst.Rollback()
	if err := migrateIn(ctx, dst, to.db.dialect); err != nil {
		return report, fmt.Errorf("make the tables: %w", err)
	}
	var done int64
	for _, t := range tables {
		n, err := copyTable(ctx, srcTx, dst, to.db.dialect, t, func(n int64) {
			if progress != nil {
				progress(t.name, done+n, total)
			}
		})
		if err != nil {
			return report, fmt.Errorf("copy %s: %w", t.name, err)
		}
		done += n
	}
	if to.db.dialect == postgres {
		if err := moveSequences(ctx, dst, tables); err != nil {
			return report, err
		}
	}
	// Every row is there: the same count in each table.
	for _, t := range tables {
		var n int64
		if err := dst.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+quote(t.name)+where(t)).Scan(&n); err != nil {
			return report, fmt.Errorf("count %s in the new database: %w", t.name, err)
		}
		if n != counts[t.name] {
			return report, fmt.Errorf("%s has %d rows in the new database but %d in this one; nothing was switched over", t.name, n, counts[t.name])
		}
		report.Tables = append(report.Tables, TableCount{Table: t.name, Rows: n})
		report.Rows += n
	}
	if err := dst.Commit(); err != nil {
		return report, err
	}
	ok = true
	if to.db.dialect == postgres {
		// Fresh statistics, so queries are planned for the data now there.
		to.db.ExecContext(ctx, "ANALYZE")
	}
	report.Took = time.Since(start)
	return report, nil
}

// openTarget connects to the database being moved to, without changing
// it. made says it's a SQLite file this made.
func openTarget(ctx context.Context, dsn string) (*Store, bool, error) {
	if IsPostgres(dsn) {
		conn, err := sql.Open("pgx", withoutJIT(dsn))
		if err != nil {
			return nil, false, err
		}
		pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		if err := conn.PingContext(pingCtx); err != nil {
			conn.Close()
			return nil, false, explainConnect(err)
		}
		return &Store{db: &db{DB: conn, dialect: postgres}}, false, nil
	}
	if dsn == "" || dsn == ":memory:" {
		return nil, false, errors.New("give a PostgreSQL URL (postgres://…) or the path of a SQLite file")
	}
	if !filepath.IsAbs(dsn) {
		return nil, false, errors.New("give the SQLite file's full path, such as /data/matinee.db")
	}
	made := false
	if info, err := os.Stat(dsn); errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(filepath.Dir(dsn), 0o755); err != nil {
			return nil, false, err
		}
		made = true
	} else if err != nil {
		return nil, false, err
	} else if info.IsDir() {
		return nil, false, fmt.Errorf("%s is a folder; give a file in it, such as %s", dsn, filepath.Join(dsn, "matinee.db"))
	}
	conn, err := sql.Open("sqlite", "file:"+dsn+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)")
	if err != nil {
		return nil, false, err
	}
	if err := conn.PingContext(ctx); err != nil {
		conn.Close()
		if made {
			removeSQLite(dsn)
		}
		return nil, false, err
	}
	return &Store{db: &db{DB: conn, dialect: sqlite}}, made, nil
}

// cleanTarget closes the database moved to, and takes away a SQLite file
// this made when the move didn't happen.
func cleanTarget(to *Store, dsn string, made, failed bool) {
	to.Close()
	if made && failed {
		removeSQLite(dsn)
	}
}

func removeSQLite(path string) {
	for _, p := range []string{path, path + "-wal", path + "-shm", path + "-journal"} {
		os.Remove(p)
	}
}

// explainConnect says what a PostgreSQL error means for someone typing an
// address in.
func explainConnect(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		switch pg.Code {
		case "28P01", "28000":
			return fmt.Errorf("PostgreSQL says the user name or password is wrong: %w", err)
		case "3D000":
			return fmt.Errorf("that database doesn't exist on the PostgreSQL server yet; make an empty one first (CREATE DATABASE matinee): %w", err)
		}
	}
	return fmt.Errorf("can't reach that PostgreSQL server: %w", err)
}

// prepareMove checks the move can go ahead and lists the tables to copy,
// in an order that copies the rows others refer to first.
func (s *Store) prepareMove(ctx context.Context, to *Store) ([]table, error) {
	if s.schemaVersion(ctx) != SchemaTarget() {
		return nil, fmt.Errorf("this database is at schema version %d, but this Matinee makes version %d; move it with the Matinee that last used it", s.schemaVersion(ctx), SchemaTarget())
	}
	if same, err := sameDatabase(ctx, s, to); err != nil {
		return nil, err
	} else if same {
		return nil, errors.New("that's the database Matinee is using now")
	}
	from, err := s.db.tables(ctx)
	if err != nil {
		return nil, fmt.Errorf("read this database's tables: %w", err)
	}
	// What the new database's tables will be: made in a transaction that's
	// rolled back, so they can be compared without making anything.
	t, err := to.db.BeginTx(ctx)
	if err != nil {
		return nil, err
	}
	defer t.Rollback()
	if v := versionIn(ctx, t); v != 0 && v != SchemaTarget() {
		return nil, fmt.Errorf("that database has Matinee's tables at schema version %d, not %d; move to an empty database", v, SchemaTarget())
	}
	if err := migrateIn(ctx, t, to.db.dialect); err != nil {
		return nil, fmt.Errorf("make Matinee's tables there: %w", err)
	}
	dest, err := to.db.tablesIn(ctx, t)
	if err != nil {
		return nil, fmt.Errorf("read the new database's tables: %w", err)
	}
	byName := map[string]table{}
	for _, d := range dest {
		byName[d.name] = d
	}
	var out []table
	for _, f := range from {
		d, ok := byName[f.name]
		if !ok {
			return nil, fmt.Errorf("the table %s is only in this database", f.name)
		}
		have := map[string]bool{}
		for _, c := range f.columns {
			have[c.name] = true
		}
		for _, c := range d.columns {
			if !have[c.name] {
				return nil, fmt.Errorf("the column %s.%s is only in the new database", d.name, c.name)
			}
		}
		if len(f.columns) != len(d.columns) {
			return nil, fmt.Errorf("the table %s has columns that are only in this database", f.name)
		}
		// Rows already there (a database another Matinee used) would be
		// mixed with these.
		var n int64
		if err := t.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+quote(d.name)).Scan(&n); err != nil {
			return nil, err
		}
		if n > 0 {
			return nil, ErrNotEmpty
		}
		out = append(out, d)
	}
	return order(out)
}

// sameDatabase says whether a and b are the same database, which an empty
// check would catch too, but with a confusing answer.
func sameDatabase(ctx context.Context, a, b *Store) (bool, error) {
	if a.db.dialect != b.db.dialect {
		return false, nil
	}
	ident := func(s *Store) (string, error) {
		var v string
		var err error
		if s.db.dialect == postgres {
			err = s.db.QueryRowContext(ctx, "SELECT current_database() || '@' || COALESCE(inet_server_addr()::text, 'local') || ':' || current_setting('port') || '/' || (SELECT system_identifier::text FROM pg_control_system())").Scan(&v)
			if err != nil {
				// pg_control_system needs more rights on some servers.
				err = s.db.QueryRowContext(ctx, "SELECT current_database() || '@' || COALESCE(inet_server_addr()::text, 'local') || ':' || current_setting('port')").Scan(&v)
			}
		} else {
			err = s.db.QueryRowContext(ctx, "SELECT file FROM pragma_database_list WHERE name = 'main'").Scan(&v)
			if err == nil && v == "" {
				v = fmt.Sprintf("memory:%p", s)
			}
		}
		return v, err
	}
	x, err := ident(a)
	if err != nil {
		return false, err
	}
	y, err := ident(b)
	if err != nil {
		return false, err
	}
	return x == y, nil
}

// versionIn is the schema version of the database t is in, 0 when it has
// none of Matinee's tables.
func versionIn(ctx context.Context, t *tx) int {
	// A failed query spoils a PostgreSQL transaction, so look first.
	var n int
	q := "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = current_schema() AND table_name = 'schema_version'"
	if t.d.dialect == sqlite {
		q = "SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'schema_version'"
	}
	if err := t.QueryRowContext(ctx, q).Scan(&n); err != nil || n == 0 {
		return 0
	}
	var v int
	t.QueryRowContext(ctx, "SELECT COALESCE(MAX(version), 0) FROM schema_version").Scan(&v)
	return v
}

// where leaves out the settings that belong to one database.
func where(t table) string {
	if t.name == "settings" {
		return " WHERE key NOT LIKE '" + movePrefix + "%'"
	}
	return ""
}

// order puts tables after the ones they refer to.
func order(tables []table) ([]table, error) {
	byName := map[string]table{}
	for _, t := range tables {
		byName[t.name] = t
	}
	slices.SortFunc(tables, func(a, b table) int { return strings.Compare(a.name, b.name) })
	var out []table
	state := map[string]int{} // 1 visiting, 2 done
	var visit func(t table) error
	visit = func(t table) error {
		switch state[t.name] {
		case 1:
			return fmt.Errorf("the tables refer to each other in a circle at %s", t.name)
		case 2:
			return nil
		}
		state[t.name] = 1
		for _, r := range t.refs {
			if r == t.name {
				continue
			}
			if rt, ok := byName[r]; ok {
				if err := visit(rt); err != nil {
					return err
				}
			}
		}
		state[t.name] = 2
		out = append(out, t)
		return nil
	}
	for _, t := range tables {
		if err := visit(t); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// tables lists Matinee's tables (not schema_version, which the schema
// makes, nor SQLite's search index, which fills itself).
func (d *db) tables(ctx context.Context) ([]table, error) {
	t, err := d.BeginTx(ctx)
	if err != nil {
		return nil, err
	}
	defer t.Rollback()
	return d.tablesIn(ctx, t)
}

func (d *db) tablesIn(ctx context.Context, t *tx) ([]table, error) {
	if d.dialect == postgres {
		return postgresTables(ctx, t)
	}
	return sqliteTables(ctx, t)
}

func sqliteTables(ctx context.Context, t *tx) ([]table, error) {
	rows, err := t.QueryContext(ctx, "SELECT name, COALESCE(sql, '') FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name")
	if err != nil {
		return nil, err
	}
	var names, virtual []string
	for rows.Next() {
		var name, def string
		if err := rows.Scan(&name, &def); err != nil {
			rows.Close()
			return nil, err
		}
		if strings.HasPrefix(strings.ToUpper(def), "CREATE VIRTUAL TABLE") {
			virtual = append(virtual, name)
			continue
		}
		names = append(names, name)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var out []table
	for _, name := range names {
		if name == "schema_version" || slices.ContainsFunc(virtual, func(v string) bool { return strings.HasPrefix(name, v+"_") }) {
			continue // a full-text index's own tables
		}
		tb := table{name: name}
		rows, err := t.QueryContext(ctx, "SELECT name, type, pk FROM pragma_table_info(?) ORDER BY cid", name)
		if err != nil {
			return nil, err
		}
		var pks []string
		for rows.Next() {
			var c, typ string
			var pk int
			if err := rows.Scan(&c, &typ, &pk); err != nil {
				rows.Close()
				return nil, err
			}
			typ = strings.ToUpper(typ)
			tb.columns = append(tb.columns, column{name: c, bytes: typ == "BLOB" || typ == "BYTEA"})
			if pk > 0 {
				pks = append(pks, c)
			}
		}
		rows.Close()
		rows, err = t.QueryContext(ctx, `SELECT "table", "from", COALESCE("to", '') FROM pragma_foreign_key_list(?)`, name)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var ref, from, to string
			if err := rows.Scan(&ref, &from, &to); err != nil {
				rows.Close()
				return nil, err
			}
			tb.refs = append(tb.refs, ref)
			if ref == name && len(pks) == 1 && (to == "" || to == pks[0]) {
				tb.parents, tb.pk = append(tb.parents, from), pks[0]
			}
		}
		rows.Close()
		out = append(out, tb)
	}
	return out, nil
}

func postgresTables(ctx context.Context, t *tx) ([]table, error) {
	rows, err := t.QueryContext(ctx, `SELECT c.table_name, c.column_name, c.data_type, c.is_identity
		FROM information_schema.columns c
		JOIN information_schema.tables t ON t.table_schema = c.table_schema AND t.table_name = c.table_name
		WHERE c.table_schema = current_schema() AND t.table_type = 'BASE TABLE' AND c.table_name <> 'schema_version'
		ORDER BY c.table_name, c.ordinal_position`)
	if err != nil {
		return nil, err
	}
	var out []table
	index := map[string]int{}
	for rows.Next() {
		var name, col, typ, ident string
		if err := rows.Scan(&name, &col, &typ, &ident); err != nil {
			rows.Close()
			return nil, err
		}
		i, ok := index[name]
		if !ok {
			i = len(out)
			index[name] = i
			out = append(out, table{name: name})
		}
		out[i].columns = append(out[i].columns, column{name: col, bytes: typ == "bytea", identity: ident == "YES"})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Foreign keys, and the primary key of a table that refers to itself.
	rows, err = t.QueryContext(ctx, `SELECT rel.relname, ref.relname,
			(SELECT string_agg(a.attname, ',') FROM pg_attribute a WHERE a.attrelid = con.conrelid AND a.attnum = ANY(con.conkey)),
			(SELECT string_agg(a.attname, ',') FROM pg_attribute a WHERE a.attrelid = con.confrelid AND a.attnum = ANY(con.confkey))
		FROM pg_constraint con
		JOIN pg_class rel ON rel.oid = con.conrelid
		JOIN pg_class ref ON ref.oid = con.confrelid
		WHERE con.contype = 'f' AND con.connamespace = current_schema()::regnamespace`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var name, ref, from, to string
		if err := rows.Scan(&name, &ref, &from, &to); err != nil {
			rows.Close()
			return nil, err
		}
		i, ok := index[name]
		if !ok {
			continue
		}
		out[i].refs = append(out[i].refs, ref)
		if ref == name && !strings.Contains(from, ",") && !strings.Contains(to, ",") {
			out[i].parents, out[i].pk = append(out[i].parents, from), to
		}
	}
	rows.Close()
	return out, rows.Err()
}

// copyTable copies every row of t, a batch of rows to each INSERT.
func copyTable(ctx context.Context, src, dst *tx, to dialect, t table, copied func(int64)) (int64, error) {
	cols := make([]string, len(t.columns))
	for i, c := range t.columns {
		cols[i] = quote(c.name)
	}
	list := strings.Join(cols, ", ")
	q := "SELECT " + list + " FROM " + quote(t.name) + where(t)
	if len(t.parents) > 0 {
		q = parentsFirst(t, cols)
	}
	rows, err := src.QueryContext(ctx, q)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	// SQLite takes up to 32766 values to a statement, PostgreSQL 65535.
	per := max(1, min(moveBatch, 30000/len(cols)))
	insert := func(n int) string {
		row := "(" + strings.TrimSuffix(strings.Repeat("?, ", len(cols)), ", ") + ")"
		return "INSERT INTO " + quote(t.name) + " (" + list + ") VALUES " + strings.TrimSuffix(strings.Repeat(row+", ", n), ", ")
	}
	full := insert(per)
	var batch []any
	var n, inBatch int64
	flush := func() error {
		if inBatch == 0 {
			return nil
		}
		q := full
		if inBatch != int64(per) {
			q = insert(int(inBatch))
		}
		if _, err := dst.ExecContext(ctx, q, batch...); err != nil {
			return err
		}
		n += inBatch
		copied(n)
		batch, inBatch = batch[:0], 0
		return nil
	}
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return n, err
		}
		for i, c := range t.columns {
			vals[i] = convert(vals[i], c, to)
		}
		batch = append(batch, vals...)
		inBatch++
		if inBatch == int64(per) {
			if err := flush(); err != nil {
				return n, err
			}
		}
	}
	if err := rows.Err(); err != nil {
		return n, err
	}
	return n, flush()
}

// parentsFirst selects t's rows so that each comes after every row of t it
// refers to: ordered by the longest chain of references above it (an
// episode after its season after its show; a version or a part after the
// file it belongs to). A row that refers to itself counts as referring to
// nothing. A row in a circle of references, or under one, isn't selected,
// and the counts then say so.
func parentsFirst(t table, cols []string) string {
	pk, name := quote(t.pk), quote(t.name)
	var edges []string
	for _, p := range t.parents {
		p = quote(p)
		edges = append(edges, "SELECT "+pk+" AS child, "+p+" AS parent FROM "+name+" WHERE "+p+" IS NOT NULL AND "+p+" <> "+pk)
	}
	sel := make([]string, len(cols))
	for i, c := range cols {
		sel[i] = "t." + c
	}
	return "WITH RECURSIVE edges(child, parent) AS (" + strings.Join(edges, " UNION ALL ") + "), " +
		"depth(k, d) AS (" +
		"SELECT " + pk + ", 0 FROM " + name + " WHERE " + pk + " NOT IN (SELECT child FROM edges) " +
		"UNION SELECT edges.child, depth.d + 1 FROM edges JOIN depth ON edges.parent = depth.k WHERE depth.d < 64), " +
		"deepest(k, d) AS (SELECT k, MAX(d) FROM depth GROUP BY k) " +
		"SELECT " + strings.Join(sel, ", ") + " FROM " + name + " t JOIN deepest ON deepest.k = t." + pk +
		strings.Replace(where(t), "WHERE", "WHERE t.", 1) + " ORDER BY deepest.d, t." + pk
}

// convert makes a value read from one database fit the other's column.
func convert(v any, c column, to dialect) any {
	switch x := v.(type) {
	case string:
		if c.bytes {
			return []byte(x)
		}
	case []byte:
		if !c.bytes {
			return string(x)
		}
	case bool:
		return b2i(x) // booleans are 0/1 integers in Matinee's tables
	case time.Time:
		if to == sqlite {
			return x.UTC().Format(time.RFC3339Nano)
		}
	}
	return v
}

// moveSequences sets each PostgreSQL identity column to carry on after the
// highest copied value, as SQLite does by itself.
func moveSequences(ctx context.Context, t *tx, tables []table) error {
	for _, tb := range tables {
		for _, c := range tb.columns {
			if !c.identity {
				continue
			}
			q := "SELECT setval(pg_get_serial_sequence(?, ?), COALESCE((SELECT MAX(" + quote(c.name) + ") FROM " + quote(tb.name) + "), 0) + 1, false)"
			if _, err := t.ExecContext(ctx, q, quote(tb.name), c.name); err != nil {
				return fmt.Errorf("move on %s.%s's numbering: %w", tb.name, c.name, err)
			}
		}
	}
	return nil
}

// moveBatch is the most rows one INSERT copies. A reference within one
// statement is only checked at its end, so tests copy a row at a time to
// see that every row comes after the rows it refers to.
var moveBatch = 500

func quote(name string) string { return `"` + strings.ReplaceAll(name, `"`, `""`) + `"` }

// MovedTo is where the database at dsn was moved to, or "" if it wasn't.
// It reads the note without opening the database the usual way, so the old
// database isn't upgraded or changed. It waits for a PostgreSQL server
// like OpenWith does.
func MovedTo(ctx context.Context, dsn string, o Options) (string, error) {
	var conn *sql.DB
	var err error
	if IsPostgres(dsn) {
		if conn, err = sql.Open("pgx", withoutJIT(dsn)); err != nil {
			return "", err
		}
		defer conn.Close()
		if err := waitUntilReady(ctx, conn, o.Wait, o.Waiting); err != nil {
			return "", err
		}
		var n int
		if err := conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = current_schema() AND table_name = 'settings'").Scan(&n); err != nil || n == 0 {
			return "", err
		}
		var to string
		err := conn.QueryRowContext(ctx, "SELECT value FROM settings WHERE key = $1", SettingMovedTo).Scan(&to)
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return to, err
	}
	if dsn == ":memory:" {
		return "", nil
	}
	if _, err := os.Stat(dsn); err != nil {
		return "", nil
	}
	if conn, err = sql.Open("sqlite", "file:"+dsn+"?_pragma=busy_timeout(5000)"); err != nil {
		return "", err
	}
	defer conn.Close()
	var n int
	if err := conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'settings'").Scan(&n); err != nil || n == 0 {
		return "", err
	}
	var to string
	err = conn.QueryRowContext(ctx, "SELECT value FROM settings WHERE key = ?", SettingMovedTo).Scan(&to)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return to, err
}

// Follow is the database Matinee uses when it's set up with dsn: dsn, or
// where it was moved to (and where that was moved to, and so on).
func Follow(ctx context.Context, dsn string, o Options) (string, error) {
	seen := map[string]bool{}
	for range 10 {
		seen[dsn] = true
		to, err := MovedTo(ctx, dsn, o)
		if err != nil {
			return dsn, err
		}
		if to == "" || seen[to] {
			return dsn, nil
		}
		dsn = to
	}
	return dsn, nil
}

// MovedFrom is what the new database says about where it came from.
type MovedFrom struct {
	From string `json:"from"`
	At   int64  `json:"at"` // seconds
	Rows int64  `json:"rows"`
}

// FinishMove switches over once the copy is in the new database (to): the
// old database (s) notes where it went, and the new one where it came from.
func (s *Store) FinishMove(ctx context.Context, from, to string, report MoveReport) error {
	n, made, err := openTarget(ctx, to)
	if err != nil {
		return err
	}
	defer cleanTarget(n, to, made, false)
	b := `{"from":` + strconv.Quote(Describe(from)) + `,"at":` + strconv.FormatInt(now(), 10) + `,"rows":` + strconv.FormatInt(report.Rows, 10) + `}`
	if err := n.SetSetting(ctx, SettingMovedFrom, b); err != nil {
		return err
	}
	return s.SetSetting(ctx, SettingMovedTo, to)
}

// UndoMove takes the note about a move away from the database at dsn, so
// Matinee uses it again, as it was before the move.
func UndoMove(ctx context.Context, dsn string) error {
	s, made, err := openTarget(ctx, dsn)
	if err != nil {
		return err
	}
	defer cleanTarget(s, dsn, made, made)
	if made {
		return errors.New("there's no database there")
	}
	_, err = s.db.ExecContext(ctx, "DELETE FROM settings WHERE key = ?", SettingMovedTo)
	return err
}

// DeleteSetting removes a setting.
func (s *Store) DeleteSetting(ctx context.Context, key string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM settings WHERE key = ?", key)
	return err
}
