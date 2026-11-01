// Package importertest builds a small data folder in the other media
// server's format, for tests.
package importertest

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// The tables and columns an import reads, as that server lays them out
// (trimmed). title_sort uses the ICU collation its real database has.
const schema = `
CREATE TABLE accounts (id INTEGER PRIMARY KEY, name varchar(255), hashed_password varchar(255), salt varchar(255), created_at datetime, updated_at datetime);
CREATE TABLE library_sections (id INTEGER PRIMARY KEY, name varchar(255), section_type integer);
CREATE TABLE metadata_items (id INTEGER PRIMARY KEY, library_section_id integer, parent_id integer, metadata_type integer,
	guid varchar(255), media_item_count integer, title varchar(255), title_sort varchar(255) COLLATE icu_root,
	"index" integer, hash varchar(255), user_thumb_url varchar(255), user_art_url varchar(255), created_at datetime, updated_at datetime);
CREATE INDEX index_title_sort_icu ON metadata_items (title_sort COLLATE icu_root);
CREATE TABLE media_items (id INTEGER PRIMARY KEY, library_section_id integer, section_location_id integer, metadata_item_id integer, duration integer);
CREATE TABLE media_parts (id INTEGER PRIMARY KEY, media_item_id integer, directory_id integer, hash varchar(255), file varchar(255), "index" integer, size integer, duration integer);
CREATE TABLE metadata_item_settings (id INTEGER PRIMARY KEY, account_id integer, guid varchar(255), rating float, view_offset integer,
	view_count integer, last_viewed_at datetime, created_at datetime, updated_at datetime, last_rated_at datetime);
CREATE TABLE tags (id INTEGER PRIMARY KEY, metadata_item_id integer, tag varchar(255), tag_type integer);
CREATE TABLE taggings (id INTEGER PRIMARY KEY, metadata_item_id integer, tag_id integer, "index" integer);
`

// Data is a data folder being filled in.
type Data struct {
	t    *testing.T
	Dir  string // the data folder
	DB   *sql.DB
	next int64
}

// New makes an empty data folder under dir.
func New(t *testing.T, dir string) *Data {
	t.Helper()
	dbDir := filepath.Join(dir, "Plug-in Support", "Databases")
	if err := os.MkdirAll(dbDir, 0o755); err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(filepath.Join(dir, "Metadata"), 0o755)
	db, err := sql.Open("sqlite", "file:"+filepath.Join(dbDir, "com.plexapp.plugins.library.db"))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	// The stock driver has no ICU collation, so the schema is written
	// directly to sqlite_master the way the real database has it.
	if _, err := db.Exec(withoutICU(schema)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("PRAGMA writable_schema = ON"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE sqlite_master SET sql = replace(sql, 'title_sort varchar(255)', 'title_sort varchar(255) COLLATE icu_root') WHERE name = 'metadata_items'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE sqlite_master SET sql = 'CREATE INDEX index_title_sort_icu ON metadata_items (title_sort COLLATE icu_root)' WHERE name = 'index_title_sort_icu'`); err != nil {
		t.Fatal(err)
	}
	db.Exec("PRAGMA writable_schema = OFF")
	return &Data{t: t, Dir: dir, DB: db, next: 100}
}

func withoutICU(s string) string {
	out := []byte{}
	for i := 0; i < len(s); i++ {
		if i+len(" COLLATE icu_root") <= len(s) && s[i:i+len(" COLLATE icu_root")] == " COLLATE icu_root" {
			i += len(" COLLATE icu_root") - 1
			continue
		}
		out = append(out, s[i])
	}
	return string(out)
}

func (d *Data) exec(q string, args ...any) {
	d.t.Helper()
	if _, err := d.DB.Exec(q, args...); err != nil {
		d.t.Fatal(err)
	}
}

// Account adds someone.
func (d *Data) Account(id int64, name string) {
	d.exec("INSERT INTO accounts (id, name) VALUES (?, ?)", id, name)
}

// Item adds an item of the given type (1 movie, 2 show, 3 season, 4
// episode, 8 artist, 9 album, 10 track) and returns its id. Files are its
// media files, in order.
func (d *Data) Item(typ int, parent int64, title, guid string, files ...string) int64 {
	d.t.Helper()
	d.next++
	id := d.next
	var p any
	if parent != 0 {
		p = parent
	}
	d.exec(`INSERT INTO metadata_items (id, parent_id, metadata_type, guid, title, title_sort, hash) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, p, typ, guid, title, title, guid+"hash")
	for i, f := range files {
		d.next++
		d.exec("INSERT INTO media_items (id, metadata_item_id) VALUES (?, ?)", d.next, id)
		d.exec(`INSERT INTO media_parts (media_item_id, file, "index") VALUES (?, ?, ?)`, d.next, f, i)
	}
	return id
}

// View records that an account watched an item (count times, offset
// milliseconds in) at a Unix time.
func (d *Data) View(account int64, guid string, offsetMs, count, at int64) {
	d.exec(`INSERT INTO metadata_item_settings (account_id, guid, view_offset, view_count, last_viewed_at) VALUES (?, ?, ?, ?, ?)`,
		account, guid, offsetMs, count, at)
}

// Rate records an account's rating of an item, out of 10, at a Unix time.
func (d *Data) Rate(account int64, guid string, rating float64, at int64) {
	d.t.Helper()
	res, err := d.DB.Exec("UPDATE metadata_item_settings SET rating = ?, last_rated_at = ? WHERE account_id = ? AND guid = ?", rating, at, account, guid)
	if err != nil {
		d.t.Fatal(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		d.exec("INSERT INTO metadata_item_settings (account_id, guid, rating, last_rated_at) VALUES (?, ?, ?, ?)", account, guid, rating, at)
	}
}

// Collection puts items in a named collection.
func (d *Data) Collection(name string, items ...int64) {
	d.next++
	tag := d.next
	d.exec("INSERT INTO tags (id, tag, tag_type) VALUES (?, ?, 2)", tag, name)
	for i, it := range items {
		d.exec(`INSERT INTO taggings (metadata_item_id, tag_id, "index") VALUES (?, ?, ?)`, it, tag, i)
	}
}

// Poster gives an item a poster someone uploaded, stored in its metadata
// bundle the way that server stores one.
func (d *Data) Poster(item int64, guid string, image []byte) {
	d.t.Helper()
	hash := guid + "hash"
	name := "deadbeef" + guid
	dir := filepath.Join(d.Dir, "Metadata", "Movies", hash[:1], hash[1:]+".bundle", "Uploads", "posters")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		d.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), image, 0o644); err != nil {
		d.t.Fatal(err)
	}
	d.exec("UPDATE metadata_items SET user_thumb_url = ? WHERE id = ?", "upload://posters/"+name, item)
}

// Close writes everything out, so the database can be read as a file.
func (d *Data) Close() { d.DB.Close() }
