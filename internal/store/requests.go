package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"strconv"
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
	// Ask is what a request with Box Office is for, beyond adding a title:
	// AskEdition (another edition of a movie in a library, Edition, to
	// replace its copy or go alongside it, Mode), AskBetter (a better
	// copy) or AskReplace (another copy, the one there being bad: Reason
	// says why). "" is a title to add. Season and Episode name the episode
	// of a show that a better or replacement copy is for.
	Ask     string `json:"ask,omitempty"`
	Edition string `json:"edition,omitempty"`
	Mode    string `json:"mode,omitempty"`
	Reason  string `json:"reason,omitempty"`
	Season  int    `json:"seasonNumber,omitempty"`
	Episode int    `json:"episodeNumber,omitempty"`
	// BoxOfficeID is the request's id on Box Office's Requests page, for
	// one filed there; 0 for one Matinee decides itself.
	BoxOfficeID int64 `json:"boxOfficeId,omitempty"`
	// Problem is the problem reported (an Issue's kind) that an
	// AskReplace is for, and Language the language (ISO 639-1) the sound
	// should be in, for the wrong language.
	Problem  string `json:"problem,omitempty"`
	Language string `json:"language,omitempty"`
	// FileName is the name of the file reported, for a Problem.
	FileName string `json:"-"`
}

// What a request asks Box Office for, beyond adding a title.
const (
	AskEdition = "edition"
	AskBetter  = "better"
	AskReplace = "replace"
)

const requestCols = `r.id, r.kind, r.tmdb_id, r.tvdb_id, r.title, r.year, r.poster, r.quality_profile_id, r.state, r.error,
	r.user_id, COALESCE(u.username, ''), r.created_at, r.updated_at, r.ask, r.edition, r.mode, r.reason, r.season, r.episode, r.boxoffice_id, r.problem, r.language, r.file_name`

const requestFrom = ` FROM requests r LEFT JOIN users u ON u.id = r.user_id`

func scanRequest(row scanner) (Request, error) {
	var r Request
	err := row.Scan(&r.ID, &r.Kind, &r.TMDBID, &r.TVDBID, &r.Title, &r.Year, &r.Poster, &r.QualityProfileID, &r.State, &r.Error,
		&r.UserID, &r.Username, &r.CreatedAt, &r.UpdatedAt, &r.Ask, &r.Edition, &r.Mode, &r.Reason, &r.Season, &r.Episode, &r.BoxOfficeID, &r.Problem, &r.Language, &r.FileName)
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

// CreateRequest saves a new request, pending unless it says otherwise (as
// one Box Office decided at once does).
func (s *Store) CreateRequest(ctx context.Context, r Request) (Request, error) {
	r.ID, r.CreatedAt, r.UpdatedAt = newID(), now(), now()
	if r.State == "" {
		r.State = RequestPending
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO requests
		(id, user_id, kind, tmdb_id, tvdb_id, title, year, poster, quality_profile_id, state, error, created_at, updated_at, ask, edition, mode, reason, season, episode, boxoffice_id, problem, language, file_name)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ID, r.UserID, r.Kind, r.TMDBID, r.TVDBID, r.Title, r.Year, r.Poster, r.QualityProfileID, r.State, r.CreatedAt, r.UpdatedAt,
		r.Ask, r.Edition, r.Mode, r.Reason, r.Season, r.Episode, r.BoxOfficeID, r.Problem, r.Language, r.FileName)
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
		" WHERE r.kind = ? AND "+col+" = ? AND r.ask = '' AND r.state IN ('pending', 'approved') ORDER BY r.created_at DESC LIMIT 1", kind, id))
}

// OpenRequestTMDB is like OpenRequest, by TMDB id for shows too.
func (s *Store) OpenRequestTMDB(ctx context.Context, kind string, tmdbID int) (Request, error) {
	return scanRequest(s.db.QueryRowContext(ctx, "SELECT "+requestCols+requestFrom+
		" WHERE r.kind = ? AND r.tmdb_id = ? AND r.tmdb_id != 0 AND r.ask = '' AND r.state IN ('pending', 'approved') ORDER BY r.created_at DESC LIMIT 1", kind, tmdbID))
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

// BoxOfficeRequests are the requests of a kind filed with Box Office
// that are still waiting there.
func (s *Store) BoxOfficeRequests(ctx context.Context, kind string) ([]Request, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+requestCols+requestFrom+
		" WHERE r.kind = ? AND r.boxoffice_id <> 0 AND r.state = 'pending' ORDER BY r.created_at LIMIT 500", kind)
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

// Issue kinds. "language" is sound in the wrong language.
var IssueKinds = []string{"video", "audio", "language", "subtitles", "wrong", "other"}

// FixableIssues are the kinds Box Office fixes by itself, by getting
// another copy: the others wait for an admin.
var FixableIssues = []string{"video", "audio", "language", "wrong"}

// Issue resolutions.
const (
	// IssueReplaced: another copy took the reported one's place.
	IssueReplaced = "replaced"
)

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
	// Language is the language (ISO 639-1) the sound should be in, for
	// the wrong language.
	Language string `json:"language,omitempty"`
	// RequestID is the request for another copy sent to Box Office for
	// it, if any, and FileKey the reported file (see FileKey), to tell
	// when another has taken its place.
	RequestID string `json:"requestId,omitempty"`
	FileKey   string `json:"-"`
}

const issueCols = `x.id, x.item_id, x.kind, x.note, x.resolved, x.resolution, x.user_id, COALESCE(u.username, ''), x.created_at, x.resolved_at,
	x.language, x.request_id, x.file_key`

const issueFrom = ` FROM issues x LEFT JOIN users u ON u.id = x.user_id`

func scanIssue(row scanner) (Issue, error) {
	var x Issue
	err := row.Scan(&x.ID, &x.ItemID, &x.Kind, &x.Note, &x.Resolved, &x.Resolution, &x.UserID, &x.Username, &x.CreatedAt, &x.ResolvedAt,
		&x.Language, &x.RequestID, &x.FileKey)
	if errors.Is(err, sql.ErrNoRows) {
		return x, ErrNotFound
	}
	return x, err
}

func (s *Store) CreateIssue(ctx context.Context, x Issue) (Issue, error) {
	x.ID, x.CreatedAt = newID(), now()
	_, err := s.db.ExecContext(ctx, "INSERT INTO issues (id, user_id, item_id, kind, note, resolved, resolution, created_at, resolved_at, language, request_id, file_key) VALUES (?, ?, ?, ?, ?, 0, '', ?, 0, ?, ?, ?)",
		x.ID, x.UserID, x.ItemID, x.Kind, strings.TrimSpace(x.Note), x.CreatedAt, x.Language, x.RequestID, x.FileKey)
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

// OpenIssue reports whether an item has a problem reported that isn't
// resolved yet.
func (s *Store) OpenIssue(ctx context.Context, itemID int64) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM issues WHERE item_id = ? AND resolved = 0", itemID).Scan(&n)
	return n > 0, err
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

// SetIssueRequest links an issue to the request for another copy sent
// to Box Office for it.
func (s *Store) SetIssueRequest(ctx context.Context, id, requestID string) error {
	_, err := s.db.ExecContext(ctx, "UPDATE issues SET request_id = ? WHERE id = ?", requestID, id)
	return err
}

// FixingIssues are the open issues Box Office was asked to fix.
func (s *Store) FixingIssues(ctx context.Context) ([]Issue, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+issueCols+issueFrom+" WHERE x.resolved = 0 AND x.request_id <> ''")
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

// FileKey says which file an item has, to notice another taking its
// place: its path, size and modification time. "" for an item without
// a file.
func (s *Store) FileKey(ctx context.Context, itemID int64) (string, error) {
	var path sql.NullString
	var size, mtime sql.NullInt64
	err := s.db.QueryRowContext(ctx, "SELECT path, size, mtime FROM items WHERE id = ?", itemID).Scan(&path, &size, &mtime)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil || !path.Valid {
		return "", err
	}
	return path.String + "|" + strconv.FormatInt(size.Int64, 10) + "|" + strconv.FormatInt(mtime.Int64, 10), nil
}

// Waiting counts pending requests and open issues, for admins' badge.
// Issues Box Office is fixing, or whose fix is a pending request, don't
// count.
func (s *Store) Waiting(ctx context.Context) (requests, issues int, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT
		(SELECT COUNT(*) FROM requests WHERE state = 'pending'),
		(SELECT COUNT(*) FROM issues x WHERE x.resolved = 0 AND NOT EXISTS
			(SELECT 1 FROM requests r WHERE r.id = x.request_id AND r.state IN ('pending', 'approved')))`).Scan(&requests, &issues)
	return
}

// ---- told when a title arrives ----

// NotifyMe asks for a person to be told when a title on its way arrives,
// as the people who asked for it are. kind is movie or show.
func (s *Store) NotifyMe(ctx context.Context, userID int64, kind string, tmdbID, tvdbID int, on bool) error {
	if !on {
		_, err := s.db.ExecContext(ctx, "DELETE FROM notify_me WHERE user_id = ? AND kind = ? AND tmdb_id = ? AND tvdb_id = ?", userID, kind, tmdbID, tvdbID)
		return err
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO notify_me (user_id, kind, tmdb_id, tvdb_id, created_at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (user_id, kind, tmdb_id, tvdb_id) DO NOTHING`, userID, kind, tmdbID, tvdbID, now())
	return err
}

// NotifyMeTitles are the titles a person asked to be told about, as
// "kind:tmdb:tvdb" keys.
func (s *Store) NotifyMeTitles(ctx context.Context, userID int64) (map[string]bool, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT kind, tmdb_id, tvdb_id FROM notify_me WHERE user_id = ?", userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var kind string
		var tmdb, tvdb int
		if err := rows.Scan(&kind, &tmdb, &tvdb); err != nil {
			return nil, err
		}
		out[NotifyMeKey(kind, tmdb, tvdb)] = true
	}
	return out, rows.Err()
}

// NotifyMeKey names a title for NotifyMeTitles.
func NotifyMeKey(kind string, tmdbID, tvdbID int) string {
	return kind + ":" + strconv.Itoa(tmdbID) + ":" + strconv.Itoa(tvdbID)
}
