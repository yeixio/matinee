package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"time"
)

// ---- downloads for offline playback ----
//
// A download is a job owned by one signed-in device: the server prepares a
// file (the original, or a conversion to MP4) and the device fetches it,
// resuming if the transfer breaks. Jobs live in the database, so a restart
// picks up where it left off. Several downloads of the same item at the same
// quality share one prepared file, named by File.

// QualityOriginal downloads the file as it is, unconverted.
const QualityOriginal = "original"

// Download states.
const (
	DownloadQueued    = "queued"    // waiting for the converter
	DownloadPreparing = "preparing" // being converted or checksummed
	DownloadReady     = "ready"     // File can be fetched
	DownloadFailed    = "failed"    // see Error; asking again retries
	DownloadExpired   = "expired"   // the device has it and the server's copy was cleaned up
)

type Download struct {
	ID         string `json:"id"`
	ItemID     int64  `json:"itemId"`
	DeviceID   string `json:"deviceId"`
	Quality    string `json:"quality"`
	Subtitle   string `json:"subtitle,omitempty"` // a picture subtitle track drawn into the video
	State      string `json:"state"`
	Progress   int    `json:"progress"` // thousandths, while preparing
	Error      string `json:"error,omitempty"`
	Size       int64  `json:"size,omitempty"`
	SHA256     string `json:"sha256,omitempty"`
	MimeType   string `json:"mimeType,omitempty"`
	CreatedAt  int64  `json:"createdAt"`
	UpdatedAt  int64  `json:"updatedAt"`
	FinishedAt int64  `json:"finishedAt,omitempty"` // when the device said it has the whole file

	UserID   int64  `json:"-"`
	Attempts int    `json:"-"`
	File     string `json:"-"` // the prepared file's path: the original, or one in the download cache
	// The original's size and mtime when File was named, to notice it changing.
	SourceSize  int64 `json:"-"`
	SourceMtime int64 `json:"-"`
}

const downloadCols = `id, item_id, device_id, quality, subtitle, state, progress, error, size, sha256, mime_type,
	created_at, updated_at, finished_at, user_id, attempts, file, source_size, source_mtime`

func scanDownload(row interface{ Scan(...any) error }) (Download, error) {
	var d Download
	err := row.Scan(&d.ID, &d.ItemID, &d.DeviceID, &d.Quality, &d.Subtitle, &d.State, &d.Progress, &d.Error,
		&d.Size, &d.SHA256, &d.MimeType, &d.CreatedAt, &d.UpdatedAt, &d.FinishedAt,
		&d.UserID, &d.Attempts, &d.File, &d.SourceSize, &d.SourceMtime)
	return d, err
}

// PutDownload asks for an item on a device. A device has at most one download
// per item, so asking again is safe: the same request returns the existing
// download (and retries it if it failed or expired), and a different quality
// replaces it.
func (s *Store) PutDownload(ctx context.Context, d Download) (Download, error) {
	old, err := s.deviceDownload(ctx, d.DeviceID, d.ItemID)
	switch {
	case errors.Is(err, ErrNotFound):
		b := make([]byte, 12)
		rand.Read(b)
		d.ID = hex.EncodeToString(b)
		d.CreatedAt = now()
	case err != nil:
		return d, err
	case old.Quality == d.Quality && old.Subtitle == d.Subtitle && old.File == d.File &&
		old.State != DownloadFailed && old.State != DownloadExpired:
		return old, nil
	default:
		d.ID, d.CreatedAt = old.ID, old.CreatedAt
	}
	d.State, d.Progress, d.Error, d.Size, d.SHA256, d.Attempts, d.UpdatedAt, d.FinishedAt =
		DownloadQueued, 0, "", 0, "", 0, now(), 0
	// Another device may already have this file ready.
	var size int64
	var sum string
	var mtime int64
	err = s.db.QueryRowContext(ctx, `SELECT size, sha256, source_mtime FROM downloads
		WHERE file = ? AND quality = ? AND subtitle = ? AND sha256 <> '' AND state = ? ORDER BY updated_at DESC LIMIT 1`,
		d.File, d.Quality, d.Subtitle, DownloadReady).Scan(&size, &sum, &mtime)
	if err == nil {
		d.State, d.Size, d.SHA256, d.SourceMtime = DownloadReady, size, sum, mtime
	} else if !errors.Is(err, sql.ErrNoRows) {
		return d, err
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO downloads (`+downloadCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET quality = excluded.quality, subtitle = excluded.subtitle,
			state = excluded.state, progress = 0, error = '', size = excluded.size, sha256 = excluded.sha256,
			mime_type = excluded.mime_type, updated_at = excluded.updated_at, finished_at = 0, attempts = 0,
			not_before = 0, file = excluded.file, source_size = excluded.source_size, source_mtime = excluded.source_mtime`,
		d.ID, d.ItemID, d.DeviceID, d.Quality, d.Subtitle, d.State, 0, "", d.Size, d.SHA256, d.MimeType,
		d.CreatedAt, d.UpdatedAt, 0, d.UserID, 0, d.File, d.SourceSize, d.SourceMtime)
	return d, err
}

func (s *Store) deviceDownload(ctx context.Context, deviceID string, itemID int64) (Download, error) {
	d, err := scanDownload(s.db.QueryRowContext(ctx, "SELECT "+downloadCols+" FROM downloads WHERE device_id = ? AND item_id = ?", deviceID, itemID))
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return d, err
}

// UserDownload finds one of a user's downloads.
func (s *Store) UserDownload(ctx context.Context, userID int64, id string) (Download, error) {
	d, err := scanDownload(s.db.QueryRowContext(ctx, "SELECT "+downloadCols+" FROM downloads WHERE id = ? AND user_id = ?", id, userID))
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return d, err
}

// Downloads lists a user's downloads, for one device or (deviceID "") all of them.
func (s *Store) Downloads(ctx context.Context, userID int64, deviceID string) ([]Download, error) {
	q, args := "SELECT "+downloadCols+" FROM downloads WHERE user_id = ?", []any{userID}
	if deviceID != "" {
		q, args = q+" AND device_id = ?", append(args, deviceID)
	}
	return s.queryDownloads(ctx, q+" ORDER BY created_at", args...)
}

// AllDownloads lists every user's downloads, for admins.
func (s *Store) AllDownloads(ctx context.Context) ([]Download, error) {
	return s.queryDownloads(ctx, "SELECT "+downloadCols+" FROM downloads ORDER BY user_id, created_at")
}

func (s *Store) queryDownloads(ctx context.Context, q string, args ...any) ([]Download, error) {
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Download{}
	for rows.Next() {
		d, err := scanDownload(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// DeleteDownload removes a download and returns it, so its file can be
// cleaned up if nothing else uses it. Admins (userID 0) can remove anyone's.
func (s *Store) DeleteDownload(ctx context.Context, userID int64, id string) (Download, error) {
	q, args := "DELETE FROM downloads WHERE id = ?", []any{id}
	if userID != 0 {
		q, args = q+" AND user_id = ?", append(args, userID)
	}
	d, err := scanDownload(s.db.QueryRowContext(ctx, q+" RETURNING "+downloadCols, args...))
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return d, err
}

// FinishDownload records that the device has the whole file and checked it.
func (s *Store) FinishDownload(ctx context.Context, userID int64, id string) error {
	res, err := s.db.ExecContext(ctx, "UPDATE downloads SET finished_at = ?, updated_at = ? WHERE id = ? AND user_id = ? AND state = ?",
		now(), now(), id, userID, DownloadReady)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// FileInUse says whether any download still needs a prepared file.
func (s *Store) FileInUse(ctx context.Context, file string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM downloads WHERE file = ? AND state <> ?", file, DownloadExpired).Scan(&n)
	return n > 0, err
}

// DownloadFiles lists every prepared file a download refers to.
func (s *Store) DownloadFiles(ctx context.Context) (map[string]bool, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT DISTINCT file FROM downloads WHERE state <> ?", DownloadExpired)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var f string
		if err := rows.Scan(&f); err != nil {
			return nil, err
		}
		out[f] = true
	}
	return out, rows.Err()
}

// ---- the converter's side ----

// RequeueInterrupted puts downloads that were being prepared when the server
// stopped back in the queue.
func (s *Store) RequeueInterrupted(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, "UPDATE downloads SET state = ?, progress = 0 WHERE state = ?", DownloadQueued, DownloadPreparing)
	return err
}

// NextDownload claims the oldest queued download whose retry time has come,
// marking it and every other download of the same file as preparing.
func (s *Store) NextDownload(ctx context.Context) (Download, error) {
	for range 5 { // another server sharing the database may claim it first
		d, err := scanDownload(s.db.QueryRowContext(ctx, "SELECT "+downloadCols+` FROM downloads
			WHERE state = ? AND not_before <= ? ORDER BY created_at LIMIT 1`, DownloadQueued, now()))
		if errors.Is(err, sql.ErrNoRows) {
			return d, ErrNotFound
		}
		if err != nil {
			return d, err
		}
		res, err := s.db.ExecContext(ctx, "UPDATE downloads SET state = ?, progress = 0, updated_at = ? WHERE file = ? AND state = ?",
			DownloadPreparing, now(), d.File, DownloadQueued)
		if err != nil {
			return d, err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			d.State = DownloadPreparing
			return d, nil
		}
	}
	return Download{}, ErrNotFound
}

// SetDownloadProgress updates every download being prepared from file.
func (s *Store) SetDownloadProgress(ctx context.Context, file string, permille int) error {
	_, err := s.db.ExecContext(ctx, "UPDATE downloads SET progress = ?, updated_at = ? WHERE file = ? AND state = ?",
		permille, now(), file, DownloadPreparing)
	return err
}

// DownloadPrepared marks every download being prepared from file as ready.
// For an original, sourceMtime is the mtime the checksum is for (0 otherwise).
func (s *Store) DownloadPrepared(ctx context.Context, file string, size int64, sha256 string, sourceMtime int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE downloads SET state = ?, progress = 1000, size = ?, sha256 = ?, error = '', updated_at = ?,
		source_mtime = CASE WHEN ? > 0 THEN ? ELSE source_mtime END WHERE file = ? AND state = ?`,
		DownloadReady, size, sha256, now(), sourceMtime, sourceMtime, file, DownloadPreparing)
	return err
}

// DownloadFailed records a failed attempt for every download being prepared
// from file. Until maxAttempts it queues them again after a growing wait.
func (s *Store) DownloadFailed(ctx context.Context, file, msg string, maxAttempts int) error {
	_, err := s.db.ExecContext(ctx, `UPDATE downloads SET attempts = attempts + 1, error = ?, progress = 0, updated_at = ?,
		state = CASE WHEN attempts + 1 >= ? THEN ? ELSE ? END,
		not_before = ? + 60 * (attempts + 1) * (attempts + 1)
		WHERE file = ? AND state = ?`,
		msg, now(), maxAttempts, DownloadFailed, DownloadQueued, now(), file, DownloadPreparing)
	return err
}

// RequeueDownload prepares a ready download again, as when its file went missing or changed.
func (s *Store) RequeueDownload(ctx context.Context, d Download) error {
	_, err := s.db.ExecContext(ctx, "UPDATE downloads SET state = ?, progress = 0, size = 0, sha256 = '', updated_at = ?, file = ?, source_size = ?, source_mtime = ? WHERE id = ?",
		DownloadQueued, now(), d.File, d.SourceSize, d.SourceMtime, d.ID)
	return err
}

// ExpireFinishedDownloads finds converted files that every device using them
// has had for longer than age, and marks those downloads expired so the
// files can be deleted. Originals are never deleted, so they're left alone.
func (s *Store) ExpireFinishedDownloads(ctx context.Context, age time.Duration) ([]string, error) {
	cutoff := time.Now().Add(-age).Unix()
	rows, err := s.db.QueryContext(ctx, `SELECT file FROM downloads WHERE quality <> ? AND state <> ? GROUP BY file
		HAVING MIN(CASE WHEN state = ? AND finished_at > 0 AND finished_at < ? THEN 1 ELSE 0 END) = 1`,
		QualityOriginal, DownloadExpired, DownloadReady, cutoff)
	if err != nil {
		return nil, err
	}
	var files []string
	for rows.Next() {
		var f string
		if err := rows.Scan(&f); err != nil {
			rows.Close()
			return nil, err
		}
		files = append(files, f)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, f := range files {
		if _, err := s.db.ExecContext(ctx, "UPDATE downloads SET state = ?, updated_at = ? WHERE file = ? AND state = ?", DownloadExpired, now(), f, DownloadReady); err != nil {
			return nil, err
		}
	}
	return files, nil
}

// ---- progress made offline ----

// ProgressEvent is a change to watch progress that a device made, possibly
// while offline, at the time At (unix seconds).
type ProgressEvent struct {
	ItemID     int64 `json:"itemId"`
	PositionMs int64 `json:"positionMs"`
	Played     bool  `json:"played"`
	At         int64 `json:"at"`
}

// MergeProgress applies a change unless the item's progress changed more
// recently, so watching on another device in the meantime isn't undone.
// It reports whether the change was applied.
func (s *Store) MergeProgress(ctx context.Context, userID int64, e ProgressEvent) (bool, error) {
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO progress (user_id, item_id, position_ms, played, updated_at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (user_id, item_id) DO UPDATE SET
			position_ms = excluded.position_ms, played = excluded.played, updated_at = excluded.updated_at
		WHERE progress.updated_at <= excluded.updated_at`,
		userID, e.ItemID, e.PositionMs, b2i(e.Played), e.At)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}
