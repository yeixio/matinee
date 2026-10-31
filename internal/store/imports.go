package store

import "context"

// FileRef is a file Matinee knows and the two items above it (a season and
// show, an album and artist, a book and author), for matching another
// server's records to Matinee's items by path (see the importer package).
type FileRef struct {
	ID              int64
	Path            string
	Kind            string
	ParentID        int64
	ParentKind      string
	GrandparentID   int64
	GrandparentKind string
}

// FileRefs lists every file in every library.
func (s *Store) FileRefs(ctx context.Context) ([]FileRef, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT i.id, i.path, i.kind, COALESCE(p.id, 0), COALESCE(p.kind, ''), COALESCE(g.id, 0), COALESCE(g.kind, '')
		FROM items i LEFT JOIN items p ON p.id = i.parent_id LEFT JOIN items g ON g.id = p.parent_id
		WHERE i.path IS NOT NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FileRef
	for rows.Next() {
		var f FileRef
		if err := rows.Scan(&f.ID, &f.Path, &f.Kind, &f.ParentID, &f.ParentKind, &f.GrandparentID, &f.GrandparentKind); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// Viewers lists everyone with their own watch history: accounts and every
// profile, the home network's included, but not the home network's
// account itself. A profile's AccountID says whose it is.
func (s *Store) Viewers(ctx context.Context) ([]User, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+userCols+` FROM users u
		WHERE u.guest = 0 ORDER BY COALESCE(u.parent_id, u.id), u.parent_id IS NOT NULL, u.id`)
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
