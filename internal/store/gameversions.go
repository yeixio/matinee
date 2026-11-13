package store

import (
	"context"
	"encoding/json"
)

// GameVersion is a game file of a library, for grouping a game's versions
// (its regions, revisions or arcade clones) as one title.
type GameVersion struct {
	ID   int64
	Path string
	// Data is what the Games library keeps with the game (PluginData).
	Data      json.RawMessage
	VersionOf int64
	// OldEdition is its edition now; Edition is for the caller to fill.
	OldEdition string
	Edition    string
}

// GameVersions lists a library's game files.
func (s *Store) GameVersions(ctx context.Context, libraryID int64) ([]GameVersion, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, path, COALESCE(plugin_data, ''), COALESCE(version_of, 0), COALESCE(edition, '')
		FROM items WHERE library_id = ? AND kind = 'game' AND path IS NOT NULL`, libraryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GameVersion
	for rows.Next() {
		var g GameVersion
		var data string
		if err := rows.Scan(&g.ID, &g.Path, &data, &g.VersionOf, &g.OldEdition); err != nil {
			return nil, err
		}
		g.Data = json.RawMessage(data)
		out = append(out, g)
	}
	return out, rows.Err()
}

// SetGameVersion makes a game file a version of another (of 0 makes it a
// title of its own) and names its edition. A game that was a title of its
// own brings its history (saves aside, which stay with each file) to the
// title it joins, as a movie's versions do.
func (s *Store) SetGameVersion(ctx context.Context, id, of int64, edition string) error {
	t, err := s.db.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer t.Rollback()
	var was int64
	if err := t.QueryRowContext(ctx, "SELECT COALESCE(version_of, 0) FROM items WHERE id = ?", id).Scan(&was); err != nil {
		return err
	}
	if of != 0 && was == 0 {
		if err := carryUserData(ctx, t, of, id, versionTables); err != nil {
			return err
		}
	}
	var versionOf any
	if of != 0 {
		versionOf = of
	}
	if _, err := t.ExecContext(ctx, "UPDATE items SET version_of = ?, edition = ? WHERE id = ?", versionOf, nullStr(edition), id); err != nil {
		return err
	}
	return t.Commit()
}
