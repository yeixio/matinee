package store

import (
	"context"
	"database/sql"
	"errors"
)

// Someone watching can find subtitles for a video online (see the
// subtitle package's OpenSubtitles). What they pick is kept here, and the
// video lists it with its own tracks.

// FoundSubtitle is a subtitle file found online for a video.
type FoundSubtitle struct {
	ID       string
	ItemID   int64
	Language string
	Label    string
	Release  string
	SDH      bool
	Forced   bool
	Source   string
	Data     []byte
	Format   string
	AddedBy  int64
	AddedAt  int64
}

// AddFoundSubtitle keeps a subtitle found for a video, and returns it with
// its id. A file from the same source for the same video is kept once.
func (s *Store) AddFoundSubtitle(ctx context.Context, f FoundSubtitle) (FoundSubtitle, error) {
	var id string
	err := s.db.QueryRowContext(ctx, "SELECT id FROM found_subtitles WHERE item_id = ? AND source = ?", f.ItemID, f.Source).Scan(&id)
	if err == nil {
		return s.FoundSubtitle(ctx, f.ItemID, id)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return f, err
	}
	f.ID, f.AddedAt = newID(), now()
	_, err = s.db.ExecContext(ctx, `INSERT INTO found_subtitles
		(id, item_id, language, label, release, sdh, forced, source, data, format, added_by, added_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		f.ID, f.ItemID, f.Language, f.Label, f.Release, b2i(f.SDH), b2i(f.Forced), f.Source, f.Data, f.Format, f.AddedBy, f.AddedAt)
	return f, err
}

// FoundSubtitles lists the subtitles found for a video, oldest first,
// without their files.
func (s *Store) FoundSubtitles(ctx context.Context, itemID int64) ([]FoundSubtitle, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, item_id, language, label, release, sdh, forced, source, format, added_by, added_at
		FROM found_subtitles WHERE item_id = ? ORDER BY added_at, id`, itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FoundSubtitle
	for rows.Next() {
		var f FoundSubtitle
		var sdh, forced int
		if err := rows.Scan(&f.ID, &f.ItemID, &f.Language, &f.Label, &f.Release, &sdh, &forced, &f.Source, &f.Format, &f.AddedBy, &f.AddedAt); err != nil {
			return nil, err
		}
		f.SDH, f.Forced = sdh != 0, forced != 0
		out = append(out, f)
	}
	return out, rows.Err()
}

// FoundSubtitle returns one subtitle found for a video, with its file.
func (s *Store) FoundSubtitle(ctx context.Context, itemID int64, id string) (FoundSubtitle, error) {
	var f FoundSubtitle
	var sdh, forced int
	err := s.db.QueryRowContext(ctx, `SELECT id, item_id, language, label, release, sdh, forced, source, data, format, added_by, added_at
		FROM found_subtitles WHERE item_id = ? AND id = ?`, itemID, id).
		Scan(&f.ID, &f.ItemID, &f.Language, &f.Label, &f.Release, &sdh, &forced, &f.Source, &f.Data, &f.Format, &f.AddedBy, &f.AddedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return f, ErrNotFound
	}
	f.SDH, f.Forced = sdh != 0, forced != 0
	return f, err
}

// RemoveFoundSubtitle forgets a subtitle found for a video.
func (s *Store) RemoveFoundSubtitle(ctx context.Context, itemID int64, id string) error {
	res, err := s.db.ExecContext(ctx, "DELETE FROM found_subtitles WHERE item_id = ? AND id = ?", itemID, id)
	return oneRow(res, err)
}
