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
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
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
	db        *db
	metaKinds atomic.Pointer[map[string]bool]
}

// Open connects to the database. A postgres:// URL uses PostgreSQL, which
// suits Kubernetes and network storage; anything else is a SQLite file path
// (":memory:" in tests).
func Open(dsn string) (*Store, error) {
	var d *db
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		conn, err := sql.Open("pgx", withoutJIT(dsn))
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
	if err := s.backfillFilters(context.Background()); err != nil {
		d.Close()
		return nil, fmt.Errorf("backfill filters: %w", err)
	}
	if err := s.AddStarterCollections(context.Background()); err != nil {
		d.Close()
		return nil, fmt.Errorf("starter collections: %w", err)
	}
	return s, nil
}

// withoutJIT turns off PostgreSQL's JIT compiler for Matinee's connections,
// unless the URL says otherwise. Library filters are quick queries the
// planner thinks are costly, and compiling them took far longer than
// running them.
func withoutJIT(dsn string) string {
	u, err := url.Parse(dsn)
	if err != nil {
		return dsn
	}
	q := u.Query()
	if q.Has("jit") {
		return dsn
	}
	q.Set("jit", "off")
	u.RawQuery = q.Encode()
	return u.String()
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
	// 3: scans now leave out files with nothing to play. Files listed before
	// that ffprobe couldn't read get probed again on the next scan.
	`UPDATE items SET mtime = 0 WHERE path IS NOT NULL AND (
		(kind = 'track' AND COALESCE(audio_codec, '') = '') OR
		(kind IN ('movie', 'episode') AND COALESCE(video_codec, '') = ''))`,
	// 4: MusicBrainz ids for artists and albums.
	"ALTER TABLE items ADD COLUMN mbid TEXT",
	// 5–8: sessions say which device they belong to, so people can see and sign out their devices.
	"ALTER TABLE sessions ADD COLUMN device_name TEXT NOT NULL DEFAULT ''",
	"ALTER TABLE sessions ADD COLUMN platform TEXT NOT NULL DEFAULT ''",
	"ALTER TABLE sessions ADD COLUMN created_at BIGINT NOT NULL DEFAULT 0",
	"ALTER TABLE sessions ADD COLUMN last_seen_at BIGINT NOT NULL DEFAULT 0",
	// 9: pending sign-ins from devices that show a code (see devices.go).
	`CREATE TABLE device_codes (
		device_code TEXT PRIMARY KEY,
		user_code   TEXT NOT NULL UNIQUE,
		device_name TEXT NOT NULL,
		platform    TEXT NOT NULL,
		user_id     BIGINT REFERENCES users(id) ON DELETE CASCADE,
		denied      INTEGER NOT NULL DEFAULT 0,
		expires_at  BIGINT NOT NULL
	)`,
	// 10–11: downloads for offline playback (see downloads.go).
	`CREATE TABLE downloads (
		id           TEXT PRIMARY KEY,
		user_id      BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		device_id    TEXT NOT NULL,
		item_id      BIGINT NOT NULL REFERENCES items(id) ON DELETE CASCADE,
		quality      TEXT NOT NULL,
		subtitle     TEXT NOT NULL DEFAULT '',
		state        TEXT NOT NULL,
		progress     INTEGER NOT NULL DEFAULT 0,
		error        TEXT NOT NULL DEFAULT '',
		attempts     INTEGER NOT NULL DEFAULT 0,
		not_before   BIGINT NOT NULL DEFAULT 0,
		file         TEXT NOT NULL,
		source_size  BIGINT NOT NULL DEFAULT 0,
		source_mtime BIGINT NOT NULL DEFAULT 0,
		size         BIGINT NOT NULL DEFAULT 0,
		sha256       TEXT NOT NULL DEFAULT '',
		md5          TEXT NOT NULL DEFAULT '',
		mime_type    TEXT NOT NULL DEFAULT '',
		created_at   BIGINT NOT NULL,
		updated_at   BIGINT NOT NULL,
		finished_at  BIGINT NOT NULL DEFAULT 0,
		UNIQUE (device_id, item_id)
	)`,
	"CREATE INDEX downloads_file ON downloads(file)",
	// 12: server-wide settings an admin changes in the app (see settings.go).
	"CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL)",
	// 13: the shared account that devices on the home network use without
	// signing in. It has no password and isn't listed with the others.
	"ALTER TABLE users ADD COLUMN guest INTEGER NOT NULL DEFAULT 0",
	// 14: each person's watch list (see watchlist.go).
	`CREATE TABLE watchlist (
		user_id  BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		item_id  BIGINT NOT NULL REFERENCES items(id) ON DELETE CASCADE,
		added_at BIGINT NOT NULL, -- milliseconds
		PRIMARY KEY (user_id, item_id)
	)`,
	// 15: items someone took out of Continue Watching, until they play them again.
	"ALTER TABLE progress ADD COLUMN hidden INTEGER NOT NULL DEFAULT 0",
	// 16–19: titles people ask for, and problems they report (see requests.go).
	`CREATE TABLE requests (
		id                 TEXT PRIMARY KEY,
		user_id            BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		kind               TEXT NOT NULL,
		tmdb_id            BIGINT NOT NULL DEFAULT 0,
		tvdb_id            BIGINT NOT NULL DEFAULT 0,
		title              TEXT NOT NULL,
		year               INTEGER NOT NULL DEFAULT 0,
		poster             TEXT NOT NULL DEFAULT '',
		quality_profile_id INTEGER NOT NULL DEFAULT 0,
		state              TEXT NOT NULL,
		error              TEXT NOT NULL DEFAULT '',
		created_at         BIGINT NOT NULL,
		updated_at         BIGINT NOT NULL
	)`,
	`CREATE TABLE issues (
		id          TEXT PRIMARY KEY,
		user_id     BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		item_id     BIGINT NOT NULL REFERENCES items(id) ON DELETE CASCADE,
		kind        TEXT NOT NULL,
		note        TEXT NOT NULL DEFAULT '',
		resolved    INTEGER NOT NULL DEFAULT 0,
		resolution  TEXT NOT NULL DEFAULT '',
		created_at  BIGINT NOT NULL,
		resolved_at BIGINT NOT NULL DEFAULT 0
	)`,
	"CREATE INDEX issues_item ON issues(item_id)",
	// Requests are matched to the libraries by TMDB id.
	"CREATE INDEX items_tmdb ON items(kind, tmdb_id)",
	// 21–23: what the background analysis found in each file (see mediainfo.go).
	"ALTER TABLE items ADD COLUMN media_info TEXT",
	"ALTER TABLE items ADD COLUMN media_tags TEXT",
	"ALTER TABLE items ADD COLUMN media_key TEXT",
	// 24: genres of movies and shows, for the home screen's genre rows (see genres.go).
	"ALTER TABLE items ADD COLUMN genres TEXT",
	// 25–31: cast, crew and the longer details of movies and shows, and what
	// titles have in common for finding related ones (see details.go).
	"ALTER TABLE items ADD COLUMN details TEXT",
	"ALTER TABLE items ADD COLUMN details_at BIGINT",
	`CREATE TABLE people (
		id         BIGINT PRIMARY KEY, -- TMDB person id
		name       TEXT NOT NULL,
		photo      TEXT,
		bio        TEXT,
		birthday   TEXT,
		deathday   TEXT,
		birthplace TEXT,
		known_for  TEXT,
		info_at    BIGINT
	)`,
	`CREATE TABLE credits (
		item_id    BIGINT NOT NULL REFERENCES items(id) ON DELETE CASCADE,
		person_id  BIGINT NOT NULL REFERENCES people(id),
		kind       TEXT NOT NULL, -- cast, guest or crew
		role       TEXT NOT NULL, -- character or job
		department TEXT NOT NULL DEFAULT '',
		episodes   INTEGER NOT NULL DEFAULT 0,
		ord        INTEGER NOT NULL,
		PRIMARY KEY (item_id, ord)
	)`,
	"CREATE INDEX credits_person ON credits(person_id)",
	`CREATE TABLE item_terms (
		item_id BIGINT NOT NULL REFERENCES items(id) ON DELETE CASCADE,
		kind    TEXT NOT NULL, -- genre, keyword, collection or rec
		term    TEXT NOT NULL,
		PRIMARY KEY (item_id, kind, term)
	)`,
	"CREATE INDEX item_terms_term ON item_terms(kind, term)",
	// 32: release date (first air date for shows, air date for episodes),
	// for filtering and sorting by it (see filter.go).
	"ALTER TABLE items ADD COLUMN released TEXT",
	// 33–37: profiles, which are users without a password that belong to an
	// account, and kids mode (see profiles.go).
	"ALTER TABLE users ADD COLUMN parent_id BIGINT REFERENCES users(id) ON DELETE CASCADE",
	"ALTER TABLE users ADD COLUMN display_name TEXT NOT NULL DEFAULT ''",
	"ALTER TABLE users ADD COLUMN pin_hash TEXT NOT NULL DEFAULT ''",
	"ALTER TABLE users ADD COLUMN kids_level INTEGER NOT NULL DEFAULT 0",
	"ALTER TABLE users ADD COLUMN avatar TEXT NOT NULL DEFAULT ''",
	// 38–39: which profile a device is watching as. Home sessions belong to
	// the home network's guest account and stop working away from home.
	"ALTER TABLE sessions ADD COLUMN profile_id BIGINT REFERENCES users(id) ON DELETE SET NULL",
	"ALTER TABLE sessions ADD COLUMN home_only INTEGER NOT NULL DEFAULT 0",
	// 40: the age rating's level (see the rating package); NULL when unknown.
	// Seasons and episodes go by their show's.
	"ALTER TABLE items ADD COLUMN rating_level INTEGER",
	// 41: the libraries a kids profile can see: everything in them ('all') or
	// only titles rated for it ('rated').
	`CREATE TABLE profile_libraries (
		user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		library_id BIGINT NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
		mode       TEXT NOT NULL,
		PRIMARY KEY (user_id, library_id)
	)`,
	// 42–44: collections, smart (a saved filter) and manual (see collections.go).
	`CREATE TABLE collections (
		id          TEXT PRIMARY KEY,
		name        TEXT NOT NULL,
		kind        TEXT NOT NULL, -- smart or manual
		user_id     BIGINT REFERENCES users(id) ON DELETE CASCADE, -- NULL: shared
		filter      TEXT, -- a smart collection's ItemFilter as JSON
		sort        TEXT NOT NULL DEFAULT '',
		sort_order  TEXT NOT NULL DEFAULT '',
		pin_home    INTEGER NOT NULL DEFAULT 0,
		pin_library BIGINT REFERENCES libraries(id) ON DELETE SET NULL,
		starter     TEXT NOT NULL DEFAULT '',
		created_at  BIGINT NOT NULL, -- milliseconds
		updated_at  BIGINT NOT NULL
	)`,
	`CREATE TABLE collection_items (
		collection_id TEXT NOT NULL REFERENCES collections(id) ON DELETE CASCADE,
		item_id       BIGINT NOT NULL REFERENCES items(id) ON DELETE CASCADE,
		position      INTEGER NOT NULL,
		PRIMARY KEY (collection_id, item_id)
	)`,
	"CREATE INDEX collection_items_item ON collection_items(item_id)",
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
	Username string `json:"username"` // a profile's name, for profiles
	IsAdmin  bool   `json:"isAdmin"`
	// Guest is the home network's shared account (see HomeGuest), or one of its profiles.
	Guest bool `json:"guest,omitempty"`
	// AccountID is the account a profile belongs to; 0 for accounts.
	AccountID int64 `json:"accountId,omitempty"`
	// KidsLevel limits a kids profile to titles rated up to it (see the
	// rating package); 0 for everyone else.
	KidsLevel int    `json:"kidsLevel,omitempty"`
	Avatar    string `json:"avatar,omitempty"` // a colour name the apps know, such as coral
	HasPIN    bool   `json:"hasPin,omitempty"`
	hash      string
	pinHash   string
}

func (u User) PasswordHash() string { return u.hash }
func (u User) PINHash() string      { return u.pinHash }

// Account is the id of the account a user is, or belongs to as a profile.
func (u User) Account() int64 {
	if u.AccountID != 0 {
		return u.AccountID
	}
	return u.ID
}

// userCols selects a user for scanUser; u is the users table.
const userCols = `u.id, u.username, u.is_admin, u.password_hash, u.guest, COALESCE(u.parent_id, 0),
	u.display_name, u.pin_hash, u.kids_level, u.avatar,
	COALESCE((SELECT a.guest FROM users a WHERE a.id = u.parent_id), 0)`

func scanUserRow(r scanner, extra ...any) (User, error) {
	var u User
	var parentGuest bool
	var display string
	err := r.Scan(append([]any{&u.ID, &u.Username, &u.IsAdmin, &u.hash, &u.Guest, &u.AccountID,
		&display, &u.pinHash, &u.KidsLevel, &u.Avatar, &parentGuest}, extra...)...)
	if err != nil {
		return u, err
	}
	u.HasPIN = u.pinHash != ""
	switch {
	case u.AccountID != 0:
		u.Username, u.IsAdmin, u.Guest = display, false, parentGuest
	case u.Guest:
		u.Username = "Home"
	}
	return u, nil
}

func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM users WHERE guest = 0 AND parent_id IS NULL").Scan(&n)
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

// Users lists the accounts people sign in with, leaving out the home
// network's guest account and profiles.
func (s *Store) Users(ctx context.Context) ([]User, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+userCols+" FROM users u WHERE u.guest = 0 AND u.parent_id IS NULL ORDER BY u.username")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		u, err := scanUserRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Store) UserByName(ctx context.Context, username string) (User, error) {
	return s.scanUser(s.db.QueryRowContext(ctx,
		"SELECT "+userCols+" FROM users u WHERE lower(u.username) = lower(?) AND u.guest = 0 AND u.parent_id IS NULL", username))
}

// UserByID returns an account people sign in to: not the guest account, and
// not a profile.
func (s *Store) UserByID(ctx context.Context, id int64) (User, error) {
	return s.scanUser(s.db.QueryRowContext(ctx,
		"SELECT "+userCols+" FROM users u WHERE u.id = ? AND u.guest = 0 AND u.parent_id IS NULL", id))
}

// Viewer returns anyone who can watch: an account, the guest account or a profile.
func (s *Store) Viewer(ctx context.Context, id int64) (User, error) {
	return s.scanUser(s.db.QueryRowContext(ctx, "SELECT "+userCols+" FROM users u WHERE u.id = ?", id))
}

// ErrLastAdmin means a change would leave the server with no admin.
var ErrLastAdmin = errors.New("the server needs at least one admin")

// otherAdmins is true when an account other than the one given is an admin.
// Changes to admins check it in the same statement as the change.
const otherAdmins = "EXISTS (SELECT 1 FROM users o WHERE o.is_admin = 1 AND o.guest = 0 AND o.id <> ?)"

func (s *Store) SetPassword(ctx context.Context, userID int64, passwordHash string) error {
	res, err := s.db.ExecContext(ctx, "UPDATE users SET password_hash = ? WHERE id = ? AND guest = 0 AND parent_id IS NULL", passwordHash, userID)
	return oneRow(res, err)
}

// SetAdmin makes an account an admin or not. The last admin stays one.
func (s *Store) SetAdmin(ctx context.Context, userID int64, admin bool) error {
	if _, err := s.UserByID(ctx, userID); err != nil {
		return err
	}
	q := "UPDATE users SET is_admin = ? WHERE id = ? AND guest = 0"
	args := []any{b2i(admin), userID}
	if !admin {
		q += " AND (is_admin = 0 OR " + otherAdmins + ")"
		args = append(args, userID)
	}
	res, err := s.db.ExecContext(ctx, q, args...)
	if err := oneRow(res, err); errors.Is(err, ErrNotFound) {
		return ErrLastAdmin
	} else if err != nil {
		return err
	}
	return nil
}

// DeleteUser removes an account and everything that belongs to it: sessions,
// watch history, watch list, downloads, requests and settings. The last admin
// can't be removed.
func (s *Store) DeleteUser(ctx context.Context, userID int64) error {
	if _, err := s.UserByID(ctx, userID); err != nil {
		return err
	}
	t, err := s.db.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer t.Rollback()
	// The other tables follow with ON DELETE CASCADE; per-user settings are
	// keys like "theme:user:<id>".
	if _, err := t.ExecContext(ctx, "DELETE FROM settings WHERE key LIKE ?", "%:user:"+strconv.FormatInt(userID, 10)); err != nil {
		return err
	}
	// Its profiles' too.
	if _, err := t.ExecContext(ctx, `DELETE FROM settings WHERE key IN (
		SELECT 'theme:user:' || CAST(id AS TEXT) FROM users WHERE parent_id = ?)`, userID); err != nil {
		return err
	}
	res, err := t.ExecContext(ctx, "DELETE FROM users WHERE id = ? AND guest = 0 AND (is_admin = 0 OR "+otherAdmins+")", userID, userID)
	if err := oneRow(res, err); errors.Is(err, ErrNotFound) {
		return ErrLastAdmin
	} else if err != nil {
		return err
	}
	return t.Commit()
}

// DeleteOtherSessions signs a user out everywhere but the session keep
// ("" signs them out everywhere). Those devices' downloads go with them.
func (s *Store) DeleteOtherSessions(ctx context.Context, userID int64, keep string) error {
	rows, err := s.db.QueryContext(ctx, "SELECT token FROM sessions WHERE user_id = ? AND token <> ?", userID, keep)
	if err != nil {
		return err
	}
	var tokens []string
	for rows.Next() {
		var token string
		if err := rows.Scan(&token); err != nil {
			rows.Close()
			return err
		}
		tokens = append(tokens, token)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, token := range tokens {
		if err := s.DeleteSession(ctx, token); err != nil {
			return err
		}
	}
	return nil
}

// oneRow turns an update that matched nothing into ErrNotFound.
func oneRow(res sql.Result, err error) error {
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) scanUser(row *sql.Row) (User, error) {
	u, err := scanUserRow(row)
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
	return s.createSession(ctx, userID, 0, false, ttl, dev)
}

// CreateHomeSession starts a session for a device at home that picked one of
// the guest account's profiles. It only works from the home network.
func (s *Store) CreateHomeSession(ctx context.Context, guestID, profileID int64, ttl time.Duration, dev Device) (string, error) {
	return s.createSession(ctx, guestID, profileID, true, ttl, dev)
}

func (s *Store) createSession(ctx context.Context, userID, profileID int64, homeOnly bool, ttl time.Duration, dev Device) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	token := hex.EncodeToString(b)
	_, err := s.db.ExecContext(ctx, `INSERT INTO sessions (token, user_id, expires_at, device_name, platform, created_at, last_seen_at, profile_id, home_only)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		token, userID, time.Now().Add(ttl).Unix(), dev.Name, dev.Platform, now(), now(), nullInt64(profileID), b2i(homeOnly))
	return token, err
}

// seenEvery is how stale last_seen_at may get, so requests don't each write to the database.
const seenEvery = 5 * 60

// SessionUser returns who a session watches as: the profile it picked, or
// else its account. homeOnly is true for sessions that only work at home.
func (s *Store) SessionUser(ctx context.Context, token string) (u User, homeOnly bool, err error) {
	var seen int64
	u, err = scanUserRow(s.db.QueryRowContext(ctx, `
		SELECT `+userCols+`, s.last_seen_at, s.home_only
		FROM sessions s JOIN users u ON u.id = COALESCE(s.profile_id, s.user_id)
		WHERE s.token = ? AND s.expires_at > ?`, token, now()), &seen, &homeOnly)
	if errors.Is(err, sql.ErrNoRows) {
		return u, false, ErrNotFound
	}
	if err != nil {
		return u, false, err
	}
	if now()-seen > seenEvery {
		s.db.ExecContext(ctx, "UPDATE sessions SET last_seen_at = ? WHERE token = ?", now(), token)
	}
	return u, homeOnly, nil
}

// SetSessionProfile switches the profile a session watches as; 0 goes back
// to the account itself.
func (s *Store) SetSessionProfile(ctx context.Context, token string, profileID int64) error {
	_, err := s.db.ExecContext(ctx, "UPDATE sessions SET profile_id = ? WHERE token = ?", nullInt64(profileID), token)
	return err
}

// DeleteSession signs a device out. Its downloads go with it.
func (s *Store) DeleteSession(ctx context.Context, token string) error {
	if _, err := s.db.ExecContext(ctx, "DELETE FROM downloads WHERE device_id = ?", SessionID(token)); err != nil {
		return err
	}
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
