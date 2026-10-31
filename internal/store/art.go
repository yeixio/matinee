package store

import (
	"context"
	"database/sql"
)

// ArtlessContainer is an album or book without a cover from its own files,
// and the files under it, to look for one in their folders.
type ArtlessContainer struct {
	ID    int64
	Paths []string
}

// ContainersWithoutLocalArt lists a library's items of kind (album or book)
// whose cover is missing or comes from online, with their files' paths.
func (s *Store) ContainersWithoutLocalArt(ctx context.Context, libraryID int64, kind string) ([]ArtlessContainer, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT c.id, f.path FROM items c JOIN items f ON f.parent_id = c.id
		WHERE c.library_id = ? AND c.kind = ? AND f.path IS NOT NULL
			AND (c.poster IS NULL OR c.poster LIKE 'http%')
		ORDER BY c.id, f.path`, libraryID, kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ArtlessContainer
	for rows.Next() {
		var id int64
		var path string
		if err := rows.Scan(&id, &path); err != nil {
			return nil, err
		}
		if n := len(out); n == 0 || out[n-1].ID != id {
			out = append(out, ArtlessContainer{ID: id})
		}
		out[len(out)-1].Paths = append(out[len(out)-1].Paths, path)
	}
	return out, rows.Err()
}

// artKinds are the kinds whose pictures come from their files' folders or
// tags first and from an online lookup after.
var artKinds = []string{"artist", "album", "author", "book"}

// ResetArtLookups has the next scan of a music or audiobook library look
// online again (and, for albums, read their files' tags again) for every artist, album, author and book without a picture
// from its own files, including ones that couldn't be matched before. The
// online pictures they have stay until a lookup finds another. It returns
// how many items it reset.
func (s *Store) ResetArtLookups(ctx context.Context, libraryID int64) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
		UPDATE items SET meta_at = NULL, tags_read = CASE WHEN kind = 'album' THEN 0 ELSE tags_read END
		WHERE library_id = ? AND kind IN (?, ?, ?, ?)
			AND (poster IS NULL OR poster LIKE 'http%')`,
		libraryID, artKinds[0], artKinds[1], artKinds[2], artKinds[3])
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// AlbumTags returns the MusicBrainz release and release group ids an
// album's files are tagged with, "" for ones they don't have.
func (s *Store) AlbumTags(ctx context.Context, id int64) (release, group string, err error) {
	var r, g sql.NullString
	err = s.db.QueryRowContext(ctx, "SELECT mb_release, mb_group FROM items WHERE id = ?", id).Scan(&r, &g)
	return r.String, g.String, err
}

// SetAlbumTags records the MusicBrainz ids read from an album's files, ""
// for none, and that they have been read. An album without a cover is
// looked up again when its files name an album they didn't before.
func (s *Store) SetAlbumTags(ctx context.Context, id int64, release, group string) error {
	var r, g, poster sql.NullString
	var read int
	if err := s.db.QueryRowContext(ctx, "SELECT mb_release, mb_group, poster, tags_read FROM items WHERE id = ?", id).
		Scan(&r, &g, &poster, &read); err != nil {
		return err
	}
	changed := release != "" && release != r.String || group != "" && group != g.String
	if read == 1 && !changed {
		return nil
	}
	q := "UPDATE items SET mb_release = COALESCE(?, mb_release), mb_group = COALESCE(?, mb_group), tags_read = 1"
	if changed && !poster.Valid {
		q += ", meta_at = NULL"
	}
	_, err := s.db.ExecContext(ctx, q+" WHERE id = ?", nullStr(release), nullStr(group), id)
	return err
}

// AlbumsToReadTags lists a library's albums without a cover whose files'
// tags haven't been read for MusicBrainz ids, such as albums scanned
// before Matinee kept them, with their files' paths.
func (s *Store) AlbumsToReadTags(ctx context.Context, libraryID int64) ([]ArtlessContainer, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT c.id, f.path FROM items c JOIN items f ON f.parent_id = c.id
		WHERE c.library_id = ? AND c.kind = 'album' AND f.path IS NOT NULL
			AND c.poster IS NULL AND c.tags_read = 0
		ORDER BY c.id, f.path`, libraryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ArtlessContainer
	for rows.Next() {
		var id int64
		var path string
		if err := rows.Scan(&id, &path); err != nil {
			return nil, err
		}
		if n := len(out); n == 0 || out[n-1].ID != id {
			out = append(out, ArtlessContainer{ID: id})
		}
		out[len(out)-1].Paths = append(out[len(out)-1].Paths, path)
	}
	return out, rows.Err()
}
