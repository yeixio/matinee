package store

import "context"

// Albums are filed under their album artist. A soundtrack or compilation
// whose files don't name one would otherwise turn into an album per track
// artist, so after each scan the scanner merges albums of the same title
// in the same folder (see scan's music kind), and each track keeps its own
// artist.

// trackArtist is what UpsertFile stores as a track's own artist.
func trackArtist(it Item) any {
	if it.Kind != "track" {
		return nil
	}
	return nullStr(it.Artist)
}

// AlbumFiles is an album with its artist and the paths of its tracks.
type AlbumFiles struct {
	ID       int64
	Title    string
	ArtistID int64
	Artist   string
	Paths    []string
}

// AlbumsWithFiles lists a music library's albums with their tracks' paths.
func (s *Store) AlbumsWithFiles(ctx context.Context, libraryID int64) ([]AlbumFiles, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT al.id, al.title, ar.id, ar.title, t.path
		FROM items al JOIN items ar ON ar.id = al.parent_id JOIN items t ON t.parent_id = al.id
		WHERE al.library_id = ? AND al.kind = 'album' AND ar.kind = 'artist' AND t.path IS NOT NULL
		ORDER BY al.id, t.path`, libraryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AlbumFiles
	for rows.Next() {
		var a AlbumFiles
		var path string
		if err := rows.Scan(&a.ID, &a.Title, &a.ArtistID, &a.Artist, &path); err != nil {
			return nil, err
		}
		if n := len(out); n == 0 || out[n-1].ID != a.ID {
			out = append(out, a)
		}
		out[len(out)-1].Paths = append(out[len(out)-1].Paths, path)
	}
	return out, rows.Err()
}

// MergeAlbums moves the tracks of the albums in from onto album into, files
// into under the artist artistID, and removes what that leaves empty. Each
// track keeps the name of the artist it was filed under as its own artist,
// unless that is the album's artist. into keeps its cover, or takes the
// first of theirs if it has none.
func (s *Store) MergeAlbums(ctx context.Context, into, artistID int64, from []int64) error {
	tx, err := s.db.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var libraryID int64
	if err := tx.QueryRowContext(ctx, "SELECT library_id FROM items WHERE id = ?", into).Scan(&libraryID); err != nil {
		return err
	}
	for _, id := range append([]int64{into}, from...) {
		// Name each track's artist before its album moves.
		if _, err := tx.ExecContext(ctx, `
			UPDATE items SET track_artist = COALESCE(track_artist,
				(SELECT ar.title FROM items al JOIN items ar ON ar.id = al.parent_id WHERE al.id = ?))
			WHERE parent_id = ? AND kind = 'track'`, id, id); err != nil {
			return err
		}
		if id == into {
			continue
		}
		if _, err := tx.ExecContext(ctx, "UPDATE items SET parent_id = ? WHERE parent_id = ?", into, id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE items SET
				poster = COALESCE(poster, (SELECT o.poster FROM items o WHERE o.id = ?)),
				backdrop = COALESCE(backdrop, (SELECT o.backdrop FROM items o WHERE o.id = ?)),
				year = COALESCE(year, (SELECT o.year FROM items o WHERE o.id = ?))
			WHERE id = ?`, id, id, id, into); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, "UPDATE items SET parent_id = ? WHERE id = ?", artistID, into); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE items SET track_artist = NULL
		WHERE parent_id = ? AND track_artist = (SELECT ar.title FROM items ar WHERE ar.id = ?)`, into, artistID); err != nil {
		return err
	}
	// Emptied albums leave artists with nothing, so repeat.
	for {
		res, err := tx.ExecContext(ctx, `
			DELETE FROM items WHERE library_id = ? AND path IS NULL AND kind IN ('album', 'artist')
			AND NOT EXISTS (SELECT 1 FROM items c WHERE c.parent_id = items.id)`, libraryID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			break
		}
	}
	return tx.Commit()
}
