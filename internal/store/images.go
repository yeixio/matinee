package store

import (
	"context"
	"crypto/sha1"
	"database/sql"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
)

// An admin can pick an album's or book's cover by hand, from covers found
// online or an image of their own. The image is kept in the database, so
// it outlives the image cache (which may be thrown away, as on Kubernetes),
// and the item's poster becomes "chosen:<item id>:<tag>", which scans and
// online lookups leave alone. The tag changes with each new pick.

// ChosenImage is a cover an admin picked.
type ChosenImage struct {
	Data        []byte
	ContentType string
	Source      string // the URL it came from, or "upload"
}

// chosenTag returns the tag of a picked cover's poster value, "" for any other.
func chosenTag(poster string) string {
	rest, ok := strings.CutPrefix(poster, "chosen:")
	if !ok {
		return ""
	}
	_, tag, _ := strings.Cut(rest, ":")
	return tag
}

// ChosenID returns the item whose picked cover a poster value names.
func ChosenID(poster string) (int64, bool) {
	rest, ok := strings.CutPrefix(poster, "chosen:")
	if !ok {
		return 0, false
	}
	id, _, _ := strings.Cut(rest, ":")
	n, err := strconv.ParseInt(id, 10, 64)
	return n, err == nil
}

// SetChosenImage makes img the item's cover.
func (s *Store) SetChosenImage(ctx context.Context, id int64, img ChosenImage) error {
	sum := sha1.Sum(img.Data)
	poster := "chosen:" + strconv.FormatInt(id, 10) + ":" + hex.EncodeToString(sum[:4])
	t, err := s.db.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer t.Rollback()
	if _, err := t.ExecContext(ctx, "DELETE FROM item_images WHERE item_id = ?", id); err != nil {
		return err
	}
	if _, err := t.ExecContext(ctx, `
		INSERT INTO item_images (item_id, data, content_type, source, created_at) VALUES (?, ?, ?, ?, ?)`,
		id, img.Data, img.ContentType, img.Source, now()); err != nil {
		return err
	}
	if _, err := t.ExecContext(ctx, "UPDATE items SET poster = ? WHERE id = ?", poster, id); err != nil {
		return err
	}
	return t.Commit()
}

// ChosenImage returns the cover an admin picked for an item.
func (s *Store) ChosenImage(ctx context.Context, id int64) (ChosenImage, error) {
	var img ChosenImage
	err := s.db.QueryRowContext(ctx, "SELECT data, content_type, source FROM item_images WHERE item_id = ?", id).
		Scan(&img.Data, &img.ContentType, &img.Source)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return img, err
}

// ClearChosenImage forgets the cover an admin picked, leaving the item to
// find one in its folder, its files or online, as if it had never had one:
// the next scan and lookup fill it in.
func (s *Store) ClearChosenImage(ctx context.Context, id int64) error {
	t, err := s.db.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer t.Rollback()
	if _, err := t.ExecContext(ctx, "DELETE FROM item_images WHERE item_id = ?", id); err != nil {
		return err
	}
	if _, err := t.ExecContext(ctx, `
		UPDATE items SET poster = NULL, meta_at = NULL, tags_read = 0
		WHERE id = ? AND poster LIKE 'chosen:%'`, id); err != nil {
		return err
	}
	return t.Commit()
}

// MBID returns the MusicBrainz id an artist or album was matched to, "" for none.
func (s *Store) MBID(ctx context.Context, id int64) (string, error) {
	var v sql.NullString
	err := s.db.QueryRowContext(ctx, "SELECT mbid FROM items WHERE id = ?", id).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return v.String, err
}
