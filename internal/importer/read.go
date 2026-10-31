package importer

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"modernc.org/sqlite"
)

// The other server's database sorts some columns with an ICU collation
// SQLite doesn't have built in. Reading needs a stand-in, registered on a
// driver of its own so Matinee's own database is untouched.
func init() {
	d := &sqlite.Driver{}
	d.MustRegisterCollationUtf8("icu_root", func(a, b string) int { return strings.Compare(strings.ToLower(a), strings.ToLower(b)) })
	sql.Register("sqlite-import", d)
}

// Item types in the other server's database.
const (
	typeMovie   = 1
	typeShow    = 2
	typeSeason  = 3
	typeEpisode = 4
	typeArtist  = 8
	typeAlbum   = 9
	typeTrack   = 10
)

// Account is someone who watched on the other server.
type Account struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// Item is a movie, show, season, episode, artist, album or track there.
type Item struct {
	ID       int64
	ParentID int64
	Type     int
	GUID     string
	Hash     string // names its metadata bundle
	Poster   string // the poster it uses; "upload://posters/…" for one someone uploaded
	Title    string
}

// View is where one account is in one item, keyed by the item's GUID.
type View struct {
	AccountID  int64
	GUID       string
	OffsetMs   int64
	Count      int64
	LastViewed int64 // Unix seconds; 0 when unknown
}

// Collection is a named group of items.
type Collection struct {
	Name  string
	Items []int64
}

// Snapshot is everything an import uses, read in one go.
type Snapshot struct {
	Location    Location
	Accounts    []Account
	Items       map[int64]*Item
	Files       map[int64][]string // item id → its files, in part order
	Views       []View
	Collections []Collection
}

// Read copies the database (and its write-ahead log, so recent changes
// count) aside and reads what an import needs. The copy means the other
// server can keep running, and its files are never changed.
func Read(ctx context.Context, loc Location) (*Snapshot, error) {
	tmp, err := os.MkdirTemp("", "matinee-import-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	copyDB := filepath.Join(tmp, "library.db")
	if err := copyFile(loc.DB, copyDB); err != nil {
		return nil, fmt.Errorf("copy the database: %w", err)
	}
	if _, err := os.Stat(loc.DB + "-wal"); err == nil {
		if err := copyFile(loc.DB+"-wal", copyDB+"-wal"); err != nil {
			return nil, fmt.Errorf("copy the database: %w", err)
		}
	}
	db, err := sql.Open("sqlite-import", "file:"+copyDB+"?_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	snap := &Snapshot{Location: loc, Items: map[int64]*Item{}, Files: map[int64][]string{}}
	if err := snap.read(ctx, db); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnreadable, err)
	}
	return snap, nil
}

// ErrUnreadable means the file isn't a library database Matinee can read.
var ErrUnreadable = fmt.Errorf("that isn't a media server database Matinee can read")

func copyFile(from, to string) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(to)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func (s *Snapshot) read(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, "SELECT id, COALESCE(name, '') FROM accounts ORDER BY id")
	if err != nil {
		return err
	}
	for rows.Next() {
		var a Account
		if err := rows.Scan(&a.ID, &a.Name); err != nil {
			rows.Close()
			return err
		}
		if a.Name == "" {
			a.Name = "#" + strconv.FormatInt(a.ID, 10)
		}
		s.Accounts = append(s.Accounts, a)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	rows, err = db.QueryContext(ctx, `
		SELECT id, COALESCE(parent_id, 0), metadata_type, COALESCE(guid, ''), COALESCE(hash, ''),
			COALESCE(user_thumb_url, ''), COALESCE(title, '')
		FROM metadata_items WHERE metadata_type IN (1, 2, 3, 4, 8, 9, 10)`)
	if err != nil {
		return err
	}
	for rows.Next() {
		it := &Item{}
		if err := rows.Scan(&it.ID, &it.ParentID, &it.Type, &it.GUID, &it.Hash, &it.Poster, &it.Title); err != nil {
			rows.Close()
			return err
		}
		s.Items[it.ID] = it
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	rows, err = db.QueryContext(ctx, `
		SELECT mi.metadata_item_id, mp.file FROM media_parts mp JOIN media_items mi ON mi.id = mp.media_item_id
		WHERE mp.file IS NOT NULL AND mp.file != ''
		ORDER BY mi.metadata_item_id, mi.id, COALESCE(mp."index", 0), mp.id`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id int64
		var file string
		if err := rows.Scan(&id, &file); err != nil {
			rows.Close()
			return err
		}
		if _, ok := s.Items[id]; ok {
			s.Files[id] = append(s.Files[id], file)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	rows, err = db.QueryContext(ctx, `
		SELECT account_id, guid, COALESCE(view_offset, 0), COALESCE(view_count, 0), last_viewed_at, updated_at
		FROM metadata_item_settings
		WHERE guid IS NOT NULL AND (COALESCE(view_count, 0) > 0 OR COALESCE(view_offset, 0) > 0)`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var v View
		var last, updated any
		if err := rows.Scan(&v.AccountID, &v.GUID, &v.OffsetMs, &v.Count, &last, &updated); err != nil {
			rows.Close()
			return err
		}
		v.LastViewed = unixTime(last)
		if v.LastViewed == 0 {
			v.LastViewed = unixTime(updated)
		}
		s.Views = append(s.Views, v)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	// Collections are tags of type 2 on the items in them.
	rows, err = db.QueryContext(ctx, `
		SELECT t.id, t.tag, tg.metadata_item_id FROM tags t JOIN taggings tg ON tg.tag_id = t.id
		WHERE t.tag_type = 2 AND t.tag IS NOT NULL AND t.tag != ''
		ORDER BY t.id, tg.id`)
	if err != nil {
		return err
	}
	byTag := map[int64]int{}
	for rows.Next() {
		var tag, item int64
		var name string
		if err := rows.Scan(&tag, &name, &item); err != nil {
			rows.Close()
			return err
		}
		i, ok := byTag[tag]
		if !ok {
			i = len(s.Collections)
			byTag[tag] = i
			s.Collections = append(s.Collections, Collection{Name: name})
		}
		s.Collections[i].Items = append(s.Collections[i].Items, item)
	}
	rows.Close()
	return rows.Err()
}

// unixTime reads a time stored as Unix seconds (newer databases) or as
// text (older ones).
func unixTime(v any) int64 {
	switch t := v.(type) {
	case int64:
		return t
	case float64:
		return int64(t)
	case time.Time:
		return t.Unix()
	case []byte:
		return unixTime(string(t))
	case string:
		if n, err := strconv.ParseInt(t, 10, 64); err == nil {
			return n
		}
		for _, layout := range []string{"2006-01-02 15:04:05", time.RFC3339, "2006-01-02T15:04:05"} {
			if tm, err := time.Parse(layout, t); err == nil {
				return tm.Unix()
			}
		}
	}
	return 0
}
