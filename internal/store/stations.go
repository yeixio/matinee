package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// Radio stations are items of kind station at the top of a music library.
// They have no file: they play from their stream (stream_url), which the
// server relays (see internal/radio). Scans leave them be, and lists of a
// library's titles leave them out unless asked for them by kind.

// Station is what's needed to add a radio station.
type Station struct {
	Name     string
	URL      string // the stream
	Homepage string
	Image    string // a logo on the internet, or ""
	Genres   []string
	// Home lets the server reach the stream on the home network, which it
	// otherwise refuses (see stream.Remote). Only for stations an admin adds.
	Home bool
}

// ErrStationTaken means the library has that stream already.
var ErrStationTaken = errors.New("that station is already in the library")

// AddStation adds a radio station to a music library.
func (s *Store) AddStation(ctx context.Context, libraryID int64, st Station) (int64, error) {
	var taken int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM items WHERE library_id = ? AND kind = 'station' AND stream_url = ?",
		libraryID, st.URL).Scan(&taken); err != nil {
		return 0, err
	}
	if taken > 0 {
		return 0, ErrStationTaken
	}
	name := strings.TrimSpace(st.Name)
	var id int64
	home := 0
	if st.Home {
		home = 1
	}
	err := s.db.QueryRowContext(ctx, `INSERT INTO items (library_id, kind, title, sort_title, overview, poster, genres, stream_url, stream_home, meta_at, added_at)
		VALUES (?, 'station', ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`,
		libraryID, name, SortTitle(name), nullStr(st.Homepage), nullStr(st.Image), encodeGenres(st.Genres), st.URL, home, now(), now()).Scan(&id)
	return id, err
}

// StationURL is a station's stream, or ErrNotFound for anything else.
func (s *Store) StationURL(ctx context.Context, id int64) (string, error) {
	u, _, err := s.StationStream(ctx, id)
	return u, err
}

// StationStream is a station's stream and whether it's on the home
// network (Station.Home), or ErrNotFound for anything else.
func (s *Store) StationStream(ctx context.Context, id int64) (string, bool, error) {
	var u sql.NullString
	var home int
	err := s.db.QueryRowContext(ctx, "SELECT stream_url, stream_home FROM items WHERE id = ? AND kind = 'station'", id).Scan(&u, &home)
	if errors.Is(err, sql.ErrNoRows) || err == nil && !u.Valid {
		return "", false, ErrNotFound
	}
	return u.String, home == 1, err
}

// RenameStation changes a station's name.
func (s *Store) RenameStation(ctx context.Context, id int64, name string) error {
	name = strings.TrimSpace(name)
	res, err := s.db.ExecContext(ctx, "UPDATE items SET title = ?, sort_title = ? WHERE id = ? AND kind = 'station'", name, SortTitle(name), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteStation removes a station, and what people played of it.
func (s *Store) DeleteStation(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, "DELETE FROM items WHERE id = ? AND kind = 'station'", id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// Stations lists a music library's radio stations by name.
func (s *Store) Stations(ctx context.Context, userID, libraryID int64) ([]Item, error) {
	return s.queryItems(ctx, "i.library_id = ? AND i.kind = 'station'", "i.sort_title, i.id", userID, libraryID)
}

// StationStreams maps each stream in a music library to its station, to
// mark what's added already in directory results.
func (s *Store) StationStreams(ctx context.Context, libraryID int64) (map[string]int64, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT stream_url, id FROM items WHERE library_id = ? AND kind = 'station'", libraryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := map[string]int64{}
	for rows.Next() {
		var u string
		var id int64
		if err := rows.Scan(&u, &id); err != nil {
			return nil, err
		}
		m[u] = id
	}
	return m, rows.Err()
}
