package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
)

// ---- requests for titles to add ----
//
// People ask for a movie or show; an admin approves it (or the server does,
// if admins let everyone add titles), and it's added to Radarr or Sonarr,
// which downloads it. Once a scan finds it, it's available.

// Request states.
const (
	RequestPending  = "pending"  // waiting for an admin
	RequestApproved = "approved" // added to Radarr or Sonarr
	RequestDeclined = "declined"
	RequestFailed   = "failed" // adding it failed; see Error. Approving again retries.
)

type Request struct {
	ID               string `json:"id"`
	Kind             string `json:"kind"` // movie or show
	TMDBID           int    `json:"tmdbId,omitempty"`
	TVDBID           int    `json:"tvdbId,omitempty"`
	Title            string `json:"title"`
	Year             int    `json:"year,omitempty"`
	Poster           string `json:"poster,omitempty"`
	QualityProfileID int    `json:"qualityProfileId,omitempty"` // 0 uses the admin's default
	State            string `json:"state"`
	Error            string `json:"error,omitempty"`
	UserID           int64  `json:"userId"`
	Username         string `json:"username"`
	CreatedAt        int64  `json:"createdAt"`
	UpdatedAt        int64  `json:"updatedAt"`
}

const requestCols = `r.id, r.kind, r.tmdb_id, r.tvdb_id, r.title, r.year, r.poster, r.quality_profile_id, r.state, r.error,
	r.user_id, COALESCE(u.username, ''), r.created_at, r.updated_at`

const requestFrom = ` FROM requests r LEFT JOIN users u ON u.id = r.user_id`

func scanRequest(row scanner) (Request, error) {
	var r Request
	err := row.Scan(&r.ID, &r.Kind, &r.TMDBID, &r.TVDBID, &r.Title, &r.Year, &r.Poster, &r.QualityProfileID, &r.State, &r.Error,
		&r.UserID, &r.Username, &r.CreatedAt, &r.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	return r, err
}

func newID() string {
	b := make([]byte, 12)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// CreateRequest saves a new request, pending.
func (s *Store) CreateRequest(ctx context.Context, r Request) (Request, error) {
	r.ID, r.State, r.CreatedAt, r.UpdatedAt = newID(), RequestPending, now(), now()
	_, err := s.db.ExecContext(ctx, `INSERT INTO requests
		(id, user_id, kind, tmdb_id, tvdb_id, title, year, poster, quality_profile_id, state, error, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '', ?, ?)`,
		r.ID, r.UserID, r.Kind, r.TMDBID, r.TVDBID, r.Title, r.Year, r.Poster, r.QualityProfileID, r.State, r.CreatedAt, r.UpdatedAt)
	if err != nil {
		return r, err
	}
	return s.Request(ctx, r.ID)
}

func (s *Store) Request(ctx context.Context, id string) (Request, error) {
	return scanRequest(s.db.QueryRowContext(ctx, "SELECT "+requestCols+requestFrom+" WHERE r.id = ?", id))
}

// OpenRequest finds a pending or approved request for a title, by TMDB id for
// movies or TVDB id for shows, so it isn't asked for twice.
func (s *Store) OpenRequest(ctx context.Context, kind string, id int) (Request, error) {
	col := "r.tmdb_id"
	if kind == "show" {
		col = "r.tvdb_id"
	}
	return scanRequest(s.db.QueryRowContext(ctx, "SELECT "+requestCols+requestFrom+
		" WHERE r.kind = ? AND "+col+" = ? AND r.state IN ('pending', 'approved') ORDER BY r.created_at DESC LIMIT 1", kind, id))
}

// Requests lists requests, pending ones first, then the newest. userID 0 lists everyone's.
func (s *Store) Requests(ctx context.Context, userID int64, limit int) ([]Request, error) {
	where, args := "1 = 1", []any{}
	if userID != 0 {
		where, args = "r.user_id = ?", append(args, userID)
	}
	rows, err := s.db.QueryContext(ctx, "SELECT "+requestCols+requestFrom+" WHERE "+where+
		" ORDER BY CASE WHEN r.state = 'pending' THEN 0 ELSE 1 END, r.created_at DESC LIMIT ?", append(args, limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Request{}
	for rows.Next() {
		r, err := scanRequest(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// UpdateRequest records a decision on a request.
func (s *Store) UpdateRequest(ctx context.Context, id, state, errMsg string, qualityProfileID int) error {
	res, err := s.db.ExecContext(ctx, "UPDATE requests SET state = ?, error = ?, quality_profile_id = ?, updated_at = ? WHERE id = ?",
		state, errMsg, qualityProfileID, now(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteRequest(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM requests WHERE id = ?", id)
	return err
}

// LibraryMatch finds a movie or show in the libraries, by TMDB id or, for
// items looked up without TMDB, by title and year. It returns 0 when there's none.
func (s *Store) LibraryMatch(ctx context.Context, kind string, tmdbID int, title string, year int) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, "SELECT id FROM items WHERE kind = ? AND tmdb_id = ? LIMIT 1", kind, tmdbID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) && title != "" {
		err = s.db.QueryRowContext(ctx, "SELECT id FROM items WHERE kind = ? AND tmdb_id IS NULL AND sort_title = ? AND (year IS NULL OR year = ? OR ? = 0) LIMIT 1",
			kind, SortTitle(title), year, year).Scan(&id)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return id, err
}

// ---- problems people report with an item ----

// Issue kinds.
var IssueKinds = []string{"video", "audio", "subtitles", "wrong", "other"}

type Issue struct {
	ID         string `json:"id"`
	ItemID     int64  `json:"itemId"`
	Kind       string `json:"kind"` // one of IssueKinds
	Note       string `json:"note,omitempty"`
	Resolved   bool   `json:"resolved"`
	Resolution string `json:"resolution,omitempty"` // replaced, or empty when an admin just closed it
	UserID     int64  `json:"userId"`
	Username   string `json:"username"`
	CreatedAt  int64  `json:"createdAt"`
	ResolvedAt int64  `json:"resolvedAt,omitempty"`
}

const issueCols = `x.id, x.item_id, x.kind, x.note, x.resolved, x.resolution, x.user_id, COALESCE(u.username, ''), x.created_at, x.resolved_at`

const issueFrom = ` FROM issues x LEFT JOIN users u ON u.id = x.user_id`

func scanIssue(row scanner) (Issue, error) {
	var x Issue
	err := row.Scan(&x.ID, &x.ItemID, &x.Kind, &x.Note, &x.Resolved, &x.Resolution, &x.UserID, &x.Username, &x.CreatedAt, &x.ResolvedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return x, ErrNotFound
	}
	return x, err
}

func (s *Store) CreateIssue(ctx context.Context, x Issue) (Issue, error) {
	x.ID, x.CreatedAt = newID(), now()
	_, err := s.db.ExecContext(ctx, "INSERT INTO issues (id, user_id, item_id, kind, note, resolved, resolution, created_at, resolved_at) VALUES (?, ?, ?, ?, ?, 0, '', ?, 0)",
		x.ID, x.UserID, x.ItemID, x.Kind, strings.TrimSpace(x.Note), x.CreatedAt)
	if err != nil {
		return x, err
	}
	return s.Issue(ctx, x.ID)
}

func (s *Store) Issue(ctx context.Context, id string) (Issue, error) {
	return scanIssue(s.db.QueryRowContext(ctx, "SELECT "+issueCols+issueFrom+" WHERE x.id = ?", id))
}

// Issues lists reported problems, open ones first, then the newest. userID 0 lists everyone's.
func (s *Store) Issues(ctx context.Context, userID int64, limit int) ([]Issue, error) {
	where, args := "1 = 1", []any{}
	if userID != 0 {
		where, args = "x.user_id = ?", append(args, userID)
	}
	rows, err := s.db.QueryContext(ctx, "SELECT "+issueCols+issueFrom+" WHERE "+where+
		" ORDER BY x.resolved, x.created_at DESC LIMIT ?", append(args, limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Issue{}
	for rows.Next() {
		x, err := scanIssue(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

func (s *Store) ResolveIssue(ctx context.Context, id, resolution string) error {
	res, err := s.db.ExecContext(ctx, "UPDATE issues SET resolved = 1, resolution = ?, resolved_at = ? WHERE id = ?", resolution, now(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// Waiting counts pending requests and open issues, for admins' badge.
func (s *Store) Waiting(ctx context.Context) (requests, issues int, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT
		(SELECT COUNT(*) FROM requests WHERE state = 'pending'),
		(SELECT COUNT(*) FROM issues WHERE resolved = 0)`).Scan(&requests, &issues)
	return
}
