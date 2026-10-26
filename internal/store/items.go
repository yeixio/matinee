package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"unicode"
)

// Item is anything browsable: a playable file or a container of other items.
type Item struct {
	ID          int64    `json:"id"`
	LibraryID   int64    `json:"libraryId"`
	ParentID    *int64   `json:"parentId,omitempty"`
	Kind        string   `json:"kind"`
	Title       string   `json:"title"`
	Year        *int     `json:"year,omitempty"`
	IndexNumber *int     `json:"index,omitempty"`
	DiscNumber  *int     `json:"disc,omitempty"`
	DurationMs  *int64   `json:"durationMs,omitempty"`
	Container   string   `json:"container,omitempty"`
	VideoCodec  string   `json:"videoCodec,omitempty"`
	AudioCodec  string   `json:"audioCodec,omitempty"`
	Width       *int     `json:"width,omitempty"`
	Height      *int     `json:"height,omitempty"`
	AddedAt     int64    `json:"addedAt"`
	Playable    bool     `json:"playable"`
	ChildCount  int      `json:"childCount,omitempty"`
	PositionMs  int64    `json:"positionMs,omitempty"`
	Played      bool     `json:"played,omitempty"`
	Overview    string   `json:"overview,omitempty"`
	Rating      *float64 `json:"rating,omitempty"`
	HasPoster   bool     `json:"hasPoster"`
	HasBackdrop bool     `json:"hasBackdrop"`
	// For episodes: the show's title and season number, for cards outside the show's pages.
	ShowTitle   string `json:"showTitle,omitempty"`
	SeasonIndex *int   `json:"seasonIndex,omitempty"`
	OnWatchList bool   `json:"onWatchList,omitempty"`
	// Genres of a movie or show, from its online match.
	Genres []string `json:"genres,omitempty"`
	// MediaTags sums up what the background analysis found, such as 4k,
	// hdr, dolby-vision, atmos or 5.1 (see probe.MediaInfo.Tags). Shows get
	// every tag any of their episodes has. Empty until analyzed.
	MediaTags []string `json:"mediaTags,omitempty"`

	Poster   string `json:"-"`
	Backdrop string `json:"-"`
	TMDBID   *int64 `json:"-"`
	MetaAt   int64  `json:"-"` // 0 until online metadata was looked up
	// Pending is true while an online lookup for the item is still to come.
	Pending bool   `json:"pending,omitempty"`
	Path    string `json:"-"`
	Size    int64  `json:"-"`
	Mtime   int64  `json:"-"`
	// SubtitleStreams is the probed embedded subtitle streams as JSON, only
	// set when upserting a file. See Store.SubtitleStreams.
	SubtitleStreams string `json:"-"`
}

// itemCols selects an item plus the given user's progress and whether it's
// on their watch list. The user id is the first bind argument.
const itemCols = `
	i.id, i.library_id, i.parent_id, i.kind, i.title, i.year, i.index_number, i.disc_number,
	i.duration_ms, COALESCE(i.container, ''), COALESCE(i.video_codec, ''), COALESCE(i.audio_codec, ''),
	i.width, i.height, i.added_at, COALESCE(i.path, ''), COALESCE(i.size, 0), COALESCE(i.mtime, 0),
	(SELECT COUNT(*) FROM items c WHERE c.parent_id = i.id),
	COALESCE(p.position_ms, 0), COALESCE(p.played, 0),
	COALESCE(i.overview, ''), i.rating, COALESCE(i.poster, ''), COALESCE(i.backdrop, ''), i.tmdb_id, COALESCE(i.meta_at, 0),
	CASE WHEN i.kind = 'episode' THEN (SELECT COALESCE(sh.title, '') FROM items se JOIN items sh ON sh.id = se.parent_id WHERE se.id = i.parent_id) ELSE '' END,
	CASE WHEN i.kind = 'episode' THEN (SELECT se.index_number FROM items se WHERE se.id = i.parent_id) END,
	EXISTS (SELECT 1 FROM watchlist wl WHERE wl.user_id = me.id AND wl.item_id = i.id),
	COALESCE(i.media_tags, ''),
	COALESCE(i.genres, '')`

const itemFrom = ` FROM (SELECT CAST(? AS BIGINT) AS id) me CROSS JOIN items i LEFT JOIN progress p ON p.item_id = i.id AND p.user_id = me.id`

type scanner interface{ Scan(...any) error }

func scanItem(r scanner) (Item, error) {
	var it Item
	var parent sql.NullInt64
	var year, idx, disc, w, h sql.NullInt64
	var dur, tmdb sql.NullInt64
	var rating sql.NullFloat64
	var seasonIdx sql.NullInt64
	var showTitle sql.NullString
	var tags, genres string
	err := r.Scan(&it.ID, &it.LibraryID, &parent, &it.Kind, &it.Title, &year, &idx, &disc,
		&dur, &it.Container, &it.VideoCodec, &it.AudioCodec, &w, &h, &it.AddedAt, &it.Path, &it.Size, &it.Mtime,
		&it.ChildCount, &it.PositionMs, &it.Played,
		&it.Overview, &rating, &it.Poster, &it.Backdrop, &tmdb, &it.MetaAt,
		&showTitle, &seasonIdx, &it.OnWatchList, &tags, &genres)
	if err != nil {
		return it, err
	}
	if parent.Valid {
		it.ParentID = &parent.Int64
	}
	it.Year, it.IndexNumber, it.DiscNumber = intPtr(year), intPtr(idx), intPtr(disc)
	it.Width, it.Height = intPtr(w), intPtr(h)
	if dur.Valid {
		it.DurationMs = &dur.Int64
	}
	if rating.Valid {
		it.Rating = &rating.Float64
	}
	if tmdb.Valid {
		it.TMDBID = &tmdb.Int64
	}
	it.ShowTitle, it.SeasonIndex = showTitle.String, intPtr(seasonIdx)
	it.Genres = decodeGenres(genres)
	it.Playable = it.Path != ""
	it.MediaTags = splitTags(tags)
	it.HasPoster, it.HasBackdrop = it.Poster != "", it.Backdrop != ""
	return it, nil
}

func intPtr(n sql.NullInt64) *int {
	if !n.Valid {
		return nil
	}
	v := int(n.Int64)
	return &v
}

func (s *Store) queryItems(ctx context.Context, where, order string, args ...any) ([]Item, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+itemCols+itemFrom+" WHERE "+where+" ORDER BY "+order, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Item{}
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			return nil, err
		}
		s.markPending(&it)
		out = append(out, it)
	}
	return out, rows.Err()
}

// Item returns one item with the user's progress.
func (s *Store) Item(ctx context.Context, userID, id int64) (Item, error) {
	it, err := scanItem(s.db.QueryRowContext(ctx, "SELECT "+itemCols+itemFrom+" WHERE i.id = ?", userID, id))
	if errors.Is(err, sql.ErrNoRows) {
		return it, ErrNotFound
	}
	s.markPending(&it)
	return it, err
}

// SetMetadataKinds says which item kinds an online provider looks up, so
// those items can show as pending until their lookup is done.
func (s *Store) SetMetadataKinds(kinds ...string) {
	m := map[string]bool{}
	for _, k := range kinds {
		m[k] = true
	}
	s.metaKinds.Store(&m)
}

func (s *Store) markPending(it *Item) {
	if m := s.metaKinds.Load(); m != nil && it.MetaAt == 0 {
		it.Pending = (*m)[it.Kind]
	}
}

// TopLevel lists a library's root items: movies, shows or artists.
func (s *Store) TopLevel(ctx context.Context, userID, libraryID int64) ([]Item, error) {
	items, err := s.queryItems(ctx, "i.library_id = ? AND i.parent_id IS NULL", "i.sort_title", userID, libraryID)
	if err != nil {
		return nil, err
	}
	return items, s.fillContainerTags(ctx, items)
}

// Children lists seasons of a show, episodes of a season, albums of an artist or tracks of an album.
func (s *Store) Children(ctx context.Context, userID, parentID int64) ([]Item, error) {
	items, err := s.queryItems(ctx, "i.parent_id = ?", "COALESCE(i.disc_number, 0), COALESCE(i.index_number, 0), i.sort_title", userID, parentID)
	if err != nil {
		return nil, err
	}
	return items, s.fillContainerTags(ctx, items)
}

// Search matches titles by word prefix, e.g. "star wa" finds "Star Wars".
func (s *Store) Search(ctx context.Context, userID int64, q string, limit int) ([]Item, error) {
	// Keep letters and digits only, so user input can't change the query syntax.
	var words []string
	for _, w := range strings.FieldsFunc(strings.ToLower(q), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		words = append(words, w)
	}
	if len(words) == 0 {
		return []Item{}, nil
	}
	if s.db.dialect == postgres {
		return s.queryItems(ctx,
			"to_tsvector('simple', i.title) @@ to_tsquery('simple', ?) AND i.kind <> 'season'",
			"i.sort_title LIMIT ?", userID, strings.Join(words, ":* & ")+":*", limit)
	}
	return s.queryItems(ctx,
		"i.id IN (SELECT rowid FROM items_fts WHERE items_fts MATCH ?) AND i.kind <> 'season'",
		"i.sort_title LIMIT ?", userID, `"`+strings.Join(words, `"* "`)+`"*`, limit)
}

// RecentlyAdded lists the newest movies, albums and shows. A show counts as
// new when an episode is, so a fresh season doesn't fill the row with episodes.
func (s *Store) RecentlyAdded(ctx context.Context, userID int64, limit int) ([]Item, error) {
	return s.queryItems(ctx, "i.kind IN ('movie', 'show', 'album')", `
		CASE WHEN i.kind = 'show' THEN (
			SELECT MAX(e.added_at) FROM items se JOIN items e ON e.parent_id = se.id WHERE se.parent_id = i.id
		) ELSE i.added_at END DESC, i.id DESC LIMIT ?`, userID, limit)
}

// ContinueWatching lists videos the user started but didn't finish, leaving
// out the ones they took off the row.
func (s *Store) ContinueWatching(ctx context.Context, userID int64, limit int) ([]Item, error) {
	return s.queryItems(ctx, "p.played = 0 AND p.position_ms > 0 AND p.hidden = 0 AND i.kind IN ('movie', 'episode')",
		"p.updated_at DESC LIMIT ?", userID, limit)
}

// HideFromContinueWatching takes an item off the user's Continue Watching
// row. Its place is kept, so its page still offers to resume, and playing it
// again brings it back.
func (s *Store) HideFromContinueWatching(ctx context.Context, userID, itemID int64) error {
	_, err := s.db.ExecContext(ctx, "UPDATE progress SET hidden = 1, updated_at = ? WHERE user_id = ? AND item_id = ?",
		now(), userID, itemID)
	return err
}

// SetProgress saves where the user is in an item. Any progress brings an
// item back to Continue Watching.
func (s *Store) SetProgress(ctx context.Context, userID, itemID, positionMs int64, played bool) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO progress (user_id, item_id, position_ms, played, updated_at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (user_id, item_id) DO UPDATE SET
			position_ms = excluded.position_ms, played = excluded.played, updated_at = excluded.updated_at, hidden = 0`,
		userID, itemID, positionMs, b2i(played), now())
	return err
}

// ---- scanner support ----

// FileStat returns the stored size and mtime for a file path, or ErrNotFound.
func (s *Store) FileStat(ctx context.Context, path string) (id, size, mtime int64, err error) {
	err = s.db.QueryRowContext(ctx, "SELECT id, COALESCE(size, 0), COALESCE(mtime, 0) FROM items WHERE path = ?", path).
		Scan(&id, &size, &mtime)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return
}

// EnsureContainer finds or creates a show, season, artist or album and returns its id.
func (s *Store) EnsureContainer(ctx context.Context, libraryID int64, parentID *int64, kind, title string, index, year *int) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `
		SELECT id FROM items
		WHERE path IS NULL AND library_id = ? AND kind = ? AND COALESCE(parent_id, 0) = COALESCE(?, 0)
		  AND title = ? AND COALESCE(index_number, -1) = COALESCE(?, -1)`,
		libraryID, kind, parentID, title, index).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	err = s.db.QueryRowContext(ctx, `
		INSERT INTO items (library_id, parent_id, kind, title, sort_title, year, index_number, added_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`,
		libraryID, parentID, kind, title, SortTitle(title), year, index, now()).Scan(&id)
	return id, err
}

// UpsertFile inserts or updates a playable item keyed by its path. The
// scanner only calls it for new or changed files, so an update drops what
// the background analysis knew about the old file.
func (s *Store) UpsertFile(ctx context.Context, it Item) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO items (library_id, parent_id, kind, title, sort_title, year, index_number, disc_number,
			path, size, mtime, duration_ms, container, video_codec, audio_codec, width, height, poster, backdrop, subtitle_streams, added_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (path) DO UPDATE SET
			library_id = excluded.library_id, parent_id = excluded.parent_id, kind = excluded.kind,
			title = excluded.title, sort_title = excluded.sort_title, year = excluded.year,
			index_number = excluded.index_number, disc_number = excluded.disc_number,
			size = excluded.size, mtime = excluded.mtime, duration_ms = excluded.duration_ms,
			container = excluded.container, video_codec = excluded.video_codec, audio_codec = excluded.audio_codec,
			width = excluded.width, height = excluded.height, subtitle_streams = excluded.subtitle_streams,
			media_info = NULL, media_tags = NULL, -- the file changed; analyzed again soon
			poster = COALESCE(excluded.poster, items.poster), backdrop = COALESCE(excluded.backdrop, items.backdrop)
		RETURNING id`,
		it.LibraryID, it.ParentID, it.Kind, it.Title, SortTitle(it.Title), it.Year, it.IndexNumber, it.DiscNumber,
		it.Path, it.Size, it.Mtime, it.DurationMs, nullStr(it.Container), nullStr(it.VideoCodec), nullStr(it.AudioCodec),
		it.Width, it.Height, nullStr(it.Poster), nullStr(it.Backdrop), nullStr(it.SubtitleStreams), now()).Scan(&id)
	return id, err
}

// FileUnder returns the path of one file under a show or season, so callers
// can find the folder it lives in. It returns "" when there's none.
func (s *Store) FileUnder(ctx context.Context, id int64) (string, error) {
	var p string
	err := s.db.QueryRowContext(ctx, `
		SELECT path FROM items
		WHERE path IS NOT NULL AND (parent_id = ? OR parent_id IN (SELECT id FROM items WHERE parent_id = ?))
		ORDER BY path LIMIT 1`, id, id).Scan(&p)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return p, err
}

// SubtitleStreams returns a file's embedded subtitle streams as stored by the
// scanner, as JSON. ok is false for files scanned before Matinee looked for them.
func (s *Store) SubtitleStreams(ctx context.Context, id int64) (streams string, ok bool, err error) {
	var v sql.NullString
	err = s.db.QueryRowContext(ctx, "SELECT subtitle_streams FROM items WHERE id = ?", id).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return v.String, v.Valid, err
}

// SetSubtitleStreams records a file's embedded subtitle streams as JSON.
func (s *Store) SetSubtitleStreams(ctx context.Context, id int64, streams string) error {
	_, err := s.db.ExecContext(ctx, "UPDATE items SET subtitle_streams = ? WHERE id = ?", streams, id)
	return err
}

// SetArt records artwork found on disk. Local art wins over anything fetched
// online, since a file next to the media is the user's own choice.
func (s *Store) SetArt(ctx context.Context, id int64, poster, backdrop string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE items SET poster = COALESCE(?, poster), backdrop = COALESCE(?, backdrop) WHERE id = ?`,
		nullStr(poster), nullStr(backdrop), id)
	return err
}

// NeedsMetadata lists a library's items of the given kinds not yet looked
// up online, shows before seasons and episodes so those can use the show's match.
func (s *Store) NeedsMetadata(ctx context.Context, libraryID int64, kinds []string) ([]Item, error) {
	if len(kinds) == 0 {
		return nil, nil
	}
	in := strings.TrimSuffix(strings.Repeat("?, ", len(kinds)), ", ")
	args := []any{0, libraryID}
	for _, k := range kinds {
		args = append(args, k)
	}
	return s.queryItems(ctx, "i.library_id = ? AND i.meta_at IS NULL AND i.kind IN ("+in+")",
		"CASE i.kind WHEN 'show' THEN 0 WHEN 'season' THEN 1 ELSE 2 END, i.id", args...)
}

// Metadata is what an online provider knows about an item.
type Metadata struct {
	TMDBID   int64
	MBID     string // MusicBrainz id, for artists and albums
	Title    string // only applied to episodes, whose file names rarely carry one
	Overview string
	Year     int
	Rating   float64
	Poster   string // URL
	Backdrop string // URL
	// Genres of a movie or show; nil when the provider doesn't say.
	Genres []string
}

// ApplyMetadata stores a provider match (nil for no match) without
// overwriting local artwork or the year from the file name.
func (s *Store) ApplyMetadata(ctx context.Context, id int64, m *Metadata) error {
	if m == nil {
		_, err := s.db.ExecContext(ctx, "UPDATE items SET meta_at = ? WHERE id = ?", now(), id)
		return err
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE items SET
			tmdb_id = ?, mbid = ?, overview = ?, rating = ?,
			year = COALESCE(year, ?), poster = COALESCE(poster, ?), backdrop = COALESCE(backdrop, ?),
			genres = COALESCE(?, genres),
			title = CASE WHEN kind = 'episode' AND ? != '' AND title LIKE 'Episode %' THEN ? ELSE title END,
			meta_at = ?
		WHERE id = ?`,
		nullInt64(m.TMDBID), nullStr(m.MBID), nullStr(m.Overview), nullFloat(m.Rating), nullInt(m.Year), nullStr(m.Poster), nullStr(m.Backdrop),
		encodeGenres(m.Genres),
		m.Title, m.Title, now(), id)
	return err
}

func nullInt(n int) any {
	if n == 0 {
		return nil
	}
	return n
}

func nullInt64(n int64) any {
	if n == 0 {
		return nil
	}
	return n
}

func nullFloat(f float64) any {
	if f == 0 {
		return nil
	}
	return f
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// RemoveMissing deletes a library's files that the last scan didn't see, then
// any containers left empty.
func (s *Store) RemoveMissing(ctx context.Context, libraryID int64, seen func(path string) bool) (int, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id, path FROM items WHERE library_id = ? AND path IS NOT NULL", libraryID)
	if err != nil {
		return 0, err
	}
	var gone []int64
	for rows.Next() {
		var id int64
		var p string
		if err := rows.Scan(&id, &p); err != nil {
			rows.Close()
			return 0, err
		}
		if !seen(p) {
			gone = append(gone, id)
		}
	}
	rows.Close()
	for _, id := range gone {
		if _, err := s.db.ExecContext(ctx, "DELETE FROM items WHERE id = ?", id); err != nil {
			return 0, err
		}
	}
	// Empty seasons make empty shows, so repeat until nothing changes.
	for {
		res, err := s.db.ExecContext(ctx, `
			DELETE FROM items WHERE library_id = ? AND path IS NULL
			AND NOT EXISTS (SELECT 1 FROM items c WHERE c.parent_id = items.id)`, libraryID)
		if err != nil {
			return 0, err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			break
		}
	}
	return len(gone), nil
}
