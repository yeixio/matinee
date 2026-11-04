package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
)

// Live TV: an admin adds sources (an M3U playlist, or a pick of iptv-org's
// lists, each with an optional XMLTV guide), Matinee reads their channels
// into live_channels and the guide into live_programs, and checks each
// channel's stream now and then (see internal/livetv and api/livetv.go).
// Channels whose checks keep failing are broken, and only admins see them.

// LiveFilter is what a source picks from iptv-org's lists.
type LiveFilter struct {
	Countries  []string `json:"countries,omitempty"`
	Categories []string `json:"categories,omitempty"`
	Languages  []string `json:"languages,omitempty"`
}

// LiveSource is a playlist of channels.
type LiveSource struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	PlaylistURL string      `json:"playlistUrl,omitempty"`
	IPTVOrg     *LiveFilter `json:"iptvOrg,omitempty"`
	GuideURL    string      `json:"guideUrl,omitempty"`
	CreatedAt   int64       `json:"createdAt"`
	RefreshedAt int64       `json:"refreshedAt,omitempty"`
	Error       string      `json:"error,omitempty"`
	GuideAt     int64       `json:"guideAt,omitempty"`
	GuideError  string      `json:"guideError,omitempty"`
	// Counts of its channels: all of them, those whose streams don't
	// answer, and those an admin hid.
	Channels int `json:"channels"`
	Broken   int `json:"broken"`
	Hidden   int `json:"hidden"`
	Programs int `json:"programs"` // shows in the guide from now on
}

// LiveChannel is a TV channel from a source.
type LiveChannel struct {
	ID         string   `json:"id"`
	SourceID   string   `json:"sourceId"`
	Name       string   `json:"name"`
	Number     string   `json:"number,omitempty"`
	GuideID    string   `json:"-"`
	Logo       string   `json:"logo,omitempty"`
	Categories []string `json:"categories,omitempty"`
	StreamURL  string   `json:"-"`
	UserAgent  string   `json:"-"`
	Referrer   string   `json:"-"`
	Hidden     bool     `json:"hidden,omitempty"`
	Broken     bool     `json:"broken,omitempty"`
	CheckedAt  int64    `json:"checkedAt,omitempty"`
}

// LiveProgram is a show on a channel; times are Unix seconds.
type LiveProgram struct {
	ChannelID   string `json:"channelId"`
	Start       int64  `json:"start"`
	Stop        int64  `json:"stop"`
	Title       string `json:"title"`
	Subtitle    string `json:"subtitle,omitempty"`
	Description string `json:"description,omitempty"`
	Category    string `json:"category,omitempty"`
	Image       string `json:"image,omitempty"`
}

// LiveChannelID is the id a channel gets from its source and stream.
func LiveChannelID(sourceID, streamURL string) string {
	h := sha256.Sum256([]byte(sourceID + "\n" + streamURL))
	return hex.EncodeToString(h[:8])
}

// LiveBrokenAfter is how many checks in a row a channel fails before it's hidden.
const LiveBrokenAfter = 2

const liveSourceCols = `s.id, s.name, s.playlist_url, s.iptv_org, s.guide_url, s.created_at, s.refreshed_at, s.error, s.guide_at, s.guide_error,
	(SELECT COUNT(*) FROM live_channels c WHERE c.source_id = s.id),
	(SELECT COUNT(*) FROM live_channels c WHERE c.source_id = s.id AND c.broken = 1),
	(SELECT COUNT(*) FROM live_channels c WHERE c.source_id = s.id AND c.hidden = 1),
	(SELECT COUNT(*) FROM live_programs p JOIN live_channels c ON c.id = p.channel_id WHERE c.source_id = s.id AND p.stop_at > ?)`

func scanLiveSource(row interface{ Scan(...any) error }) (LiveSource, error) {
	var src LiveSource
	var filter sql.NullString
	err := row.Scan(&src.ID, &src.Name, &src.PlaylistURL, &filter, &src.GuideURL, &src.CreatedAt, &src.RefreshedAt, &src.Error,
		&src.GuideAt, &src.GuideError, &src.Channels, &src.Broken, &src.Hidden, &src.Programs)
	if err != nil {
		return src, err
	}
	if filter.Valid {
		src.IPTVOrg = &LiveFilter{}
		json.Unmarshal([]byte(filter.String), src.IPTVOrg)
	}
	return src, nil
}

// LiveSources lists the sources, oldest first.
func (s *Store) LiveSources(ctx context.Context) ([]LiveSource, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+liveSourceCols+" FROM live_sources s ORDER BY s.created_at, s.id", now())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []LiveSource{}
	for rows.Next() {
		src, err := scanLiveSource(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, src)
	}
	return out, rows.Err()
}

// LiveSource is one source.
func (s *Store) LiveSource(ctx context.Context, id string) (LiveSource, error) {
	src, err := scanLiveSource(s.db.QueryRowContext(ctx, "SELECT "+liveSourceCols+" FROM live_sources s WHERE s.id = ?", now(), id))
	if errors.Is(err, sql.ErrNoRows) {
		return src, ErrNotFound
	}
	return src, err
}

func filterJSON(f *LiveFilter) any {
	if f == nil {
		return nil
	}
	b, _ := json.Marshal(f)
	return string(b)
}

// AddLiveSource saves a new source, with no channels yet.
func (s *Store) AddLiveSource(ctx context.Context, src LiveSource) (string, error) {
	id := newID()
	_, err := s.db.ExecContext(ctx, `INSERT INTO live_sources (id, name, playlist_url, iptv_org, guide_url, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`, id, strings.TrimSpace(src.Name), src.PlaylistURL, filterJSON(src.IPTVOrg), src.GuideURL, now())
	return id, err
}

// UpdateLiveSource changes a source's name, playlist, pick and guide.
func (s *Store) UpdateLiveSource(ctx context.Context, src LiveSource) error {
	res, err := s.db.ExecContext(ctx, "UPDATE live_sources SET name = ?, playlist_url = ?, iptv_org = ?, guide_url = ? WHERE id = ?",
		strings.TrimSpace(src.Name), src.PlaylistURL, filterJSON(src.IPTVOrg), src.GuideURL, src.ID)
	return oneRow(res, err)
}

// DeleteLiveSource removes a source with its channels and guide.
func (s *Store) DeleteLiveSource(ctx context.Context, id string) error {
	t, err := s.db.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer t.Rollback()
	// SQLite only cascades with foreign keys on; delete by hand to be sure.
	if _, err := t.ExecContext(ctx, "DELETE FROM live_programs WHERE channel_id IN (SELECT id FROM live_channels WHERE source_id = ?)", id); err != nil {
		return err
	}
	if _, err := t.ExecContext(ctx, "DELETE FROM live_channels WHERE source_id = ?", id); err != nil {
		return err
	}
	res, err := t.ExecContext(ctx, "DELETE FROM live_sources WHERE id = ?", id)
	if err := oneRow(res, err); err != nil {
		return err
	}
	return t.Commit()
}

// SetLiveSourceError saves why a source's refresh failed, leaving its
// channels as they were.
func (s *Store) SetLiveSourceError(ctx context.Context, id, msg string) error {
	_, err := s.db.ExecContext(ctx, "UPDATE live_sources SET error = ?, refreshed_at = ? WHERE id = ?", msg, now(), id)
	return err
}

// SaveLiveChannels replaces a source's channels with what its playlist
// lists now. Channels it still lists keep their id, whether an admin hid
// them, and how their checks went.
func (s *Store) SaveLiveChannels(ctx context.Context, sourceID string, chans []LiveChannel) error {
	t, err := s.db.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer t.Rollback()
	old := map[string]bool{}
	rows, err := t.QueryContext(ctx, "SELECT id FROM live_channels WHERE source_id = ?", sourceID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		old[id] = true
	}
	rows.Close()
	seen := map[string]bool{}
	for i, c := range chans {
		id := LiveChannelID(sourceID, c.StreamURL)
		if seen[id] {
			continue
		}
		seen[id] = true
		cats := strings.Join(c.Categories, ";")
		if old[id] {
			_, err = t.ExecContext(ctx, `UPDATE live_channels SET position = ?, name = ?, number = ?, guide_id = ?, logo = ?, categories = ?,
				user_agent = ?, referrer = ? WHERE id = ?`, i, c.Name, c.Number, c.GuideID, c.Logo, cats, c.UserAgent, c.Referrer, id)
		} else {
			_, err = t.ExecContext(ctx, `INSERT INTO live_channels (id, source_id, position, name, number, guide_id, logo, categories, stream_url, user_agent, referrer)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, id, sourceID, i, c.Name, c.Number, c.GuideID, c.Logo, cats, c.StreamURL, c.UserAgent, c.Referrer)
		}
		if err != nil {
			return err
		}
	}
	for id := range old {
		if !seen[id] {
			if _, err := t.ExecContext(ctx, "DELETE FROM live_programs WHERE channel_id = ?", id); err != nil {
				return err
			}
			if _, err := t.ExecContext(ctx, "DELETE FROM live_channels WHERE id = ?", id); err != nil {
				return err
			}
		}
	}
	if _, err := t.ExecContext(ctx, "UPDATE live_sources SET error = '', refreshed_at = ? WHERE id = ?", now(), sourceID); err != nil {
		return err
	}
	return t.Commit()
}

const liveChannelCols = "id, source_id, name, number, guide_id, logo, categories, stream_url, user_agent, referrer, hidden, broken, checked_at"

func scanLiveChannel(row interface{ Scan(...any) error }) (LiveChannel, error) {
	var c LiveChannel
	var cats string
	var hidden, broken int
	err := row.Scan(&c.ID, &c.SourceID, &c.Name, &c.Number, &c.GuideID, &c.Logo, &cats, &c.StreamURL, &c.UserAgent, &c.Referrer, &hidden, &broken, &c.CheckedAt)
	if cats != "" {
		c.Categories = strings.Split(cats, ";")
	}
	c.Hidden, c.Broken = hidden == 1, broken == 1
	return c, err
}

// LiveChannels lists channels in their sources' order. Without all, only
// the ones that play and aren't hidden; with sourceID, only that source's.
func (s *Store) LiveChannels(ctx context.Context, sourceID string, all bool) ([]LiveChannel, error) {
	q := "SELECT " + liveChannelCols + " FROM live_channels WHERE 1 = 1"
	var args []any
	if sourceID != "" {
		q += " AND source_id = ?"
		args = append(args, sourceID)
	}
	if !all {
		q += " AND hidden = 0 AND broken = 0"
	}
	q += " ORDER BY (SELECT created_at FROM live_sources WHERE live_sources.id = source_id), source_id, position"
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []LiveChannel{}
	for rows.Next() {
		c, err := scanLiveChannel(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// LiveChannel is one channel.
func (s *Store) LiveChannel(ctx context.Context, id string) (LiveChannel, error) {
	c, err := scanLiveChannel(s.db.QueryRowContext(ctx, "SELECT "+liveChannelCols+" FROM live_channels WHERE id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrNotFound
	}
	return c, err
}

// SetLiveChannelHidden hides a channel from everyone, or shows it again.
func (s *Store) SetLiveChannelHidden(ctx context.Context, id string, hidden bool) error {
	res, err := s.db.ExecContext(ctx, "UPDATE live_channels SET hidden = ? WHERE id = ?", b2i(hidden), id)
	return oneRow(res, err)
}

// LiveChannelsToCheck are the channels checked longest ago, none since before.
func (s *Store) LiveChannelsToCheck(ctx context.Context, before int64, limit int) ([]LiveChannel, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+liveChannelCols+" FROM live_channels WHERE checked_at < ? ORDER BY checked_at, id LIMIT ?", before, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LiveChannel
	for rows.Next() {
		c, err := scanLiveChannel(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// RecordLiveCheck saves how a check of a channel's stream went. A channel
// that fails LiveBrokenAfter in a row is broken until one works again.
func (s *Store) RecordLiveCheck(ctx context.Context, id string, ok bool) error {
	if ok {
		_, err := s.db.ExecContext(ctx, "UPDATE live_channels SET fails = 0, broken = 0, checked_at = ? WHERE id = ?", now(), id)
		return err
	}
	_, err := s.db.ExecContext(ctx, `UPDATE live_channels SET fails = fails + 1, checked_at = ?,
		broken = CASE WHEN fails + 1 >= ? THEN 1 ELSE broken END WHERE id = ?`, now(), LiveBrokenAfter, id)
	return err
}

// SaveLiveGuide replaces what's on a source's channels from now on, and
// says the guide was read (or why it couldn't be, keeping the old one).
func (s *Store) SaveLiveGuide(ctx context.Context, sourceID string, programs []LiveProgram, guideErr string) error {
	t, err := s.db.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer t.Rollback()
	if guideErr == "" {
		if _, err := t.ExecContext(ctx, "DELETE FROM live_programs WHERE channel_id IN (SELECT id FROM live_channels WHERE source_id = ?)", sourceID); err != nil {
			return err
		}
		seen := map[string]bool{}
		for _, p := range programs {
			key := p.ChannelID + "\n" + strconv.FormatInt(p.Start, 10)
			if seen[key] {
				continue
			}
			seen[key] = true
			if _, err := t.ExecContext(ctx, `INSERT INTO live_programs (channel_id, start_at, stop_at, title, subtitle, description, category, image)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, p.ChannelID, p.Start, p.Stop, p.Title, p.Subtitle, p.Description, p.Category, p.Image); err != nil {
				return err
			}
		}
	}
	if _, err := t.ExecContext(ctx, "UPDATE live_sources SET guide_at = ?, guide_error = ? WHERE id = ?", now(), guideErr, sourceID); err != nil {
		return err
	}
	return t.Commit()
}

// LivePrograms lists what's on between from and to (Unix seconds), on the
// given channels or, with none, on every channel.
func (s *Store) LivePrograms(ctx context.Context, from, to int64, channels []string) ([]LiveProgram, error) {
	q := `SELECT channel_id, start_at, stop_at, title, subtitle, description, category, image FROM live_programs
		WHERE stop_at > ? AND start_at < ?`
	args := []any{from, to}
	if len(channels) > 0 {
		q += " AND channel_id IN (?" + strings.Repeat(", ?", len(channels)-1) + ")"
		for _, c := range channels {
			args = append(args, c)
		}
	}
	q += " ORDER BY channel_id, start_at"
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []LiveProgram{}
	for rows.Next() {
		var p LiveProgram
		if err := rows.Scan(&p.ChannelID, &p.Start, &p.Stop, &p.Title, &p.Subtitle, &p.Description, &p.Category, &p.Image); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// PruneLivePrograms drops shows that ended before then.
func (s *Store) PruneLivePrograms(ctx context.Context, before int64) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM live_programs WHERE stop_at < ?", before)
	return err
}
