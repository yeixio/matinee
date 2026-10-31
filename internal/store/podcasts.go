package store

import (
	"context"
	"crypto/sha1"
	"database/sql"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"
)

// Podcasts: a podcast library holds shows (kind podcast), each with its
// episodes (kind podcastEpisode). A show is kept by its feed's address and
// an episode by its id in the feed (podcast_episodes.guid). An episode's
// path is the address of its audio, plus a #fragment that keeps it unique
// (see EpisodeURL), so it plays from where the show publishes it.

// PodcastFeed is where a show's feed is and how its last refresh went.
type PodcastFeed struct {
	ID           int64  `json:"id"`
	LibraryID    int64  `json:"libraryId"`
	Title        string `json:"title"`
	URL          string `json:"feedUrl"`
	Link         string `json:"link,omitempty"`
	ETag         string `json:"-"`
	LastModified string `json:"-"`
	RefreshedAt  int64  `json:"refreshedAt"` // Unix seconds; 0 before the first
	Error        string `json:"error,omitempty"`
}

// PodcastInfo is what a feed says about its show.
type PodcastInfo struct {
	Title       string
	Description string
	Author      string
	Link        string
	Image       string
	Language    string
	Categories  []string
}

// PodcastEpisode is one episode as a feed lists it.
type PodcastEpisode struct {
	GUID        string
	Title       string
	Description string
	URL         string
	MimeType    string
	Size        int64
	Published   time.Time
	DurationMs  int64
	Season      int
	Number      int
	Image       string
	Link        string
	Kind        string // full, trailer or bonus
}

// ErrPodcastTitleTaken means the library already has a different show
// with the same name.
var ErrPodcastTitleTaken = errors.New("this library already has a show with that name")

// EpisodeURL is where an episode's audio is: its path without the
// #fragment that keeps it unique.
func EpisodeURL(path string) string {
	u, _, _ := strings.Cut(path, "#")
	return u
}

// IsRemote says whether an item's path is an address on the internet
// rather than a file on the server, as podcast episodes' are.
func IsRemote(path string) bool {
	return strings.HasPrefix(path, "https://") || strings.HasPrefix(path, "http://")
}

func episodePath(podcastID int64, guid, url string) string {
	sum := sha1.Sum([]byte(strconv.FormatInt(podcastID, 10) + "\x00" + guid))
	return EpisodeURL(url) + "#matinee-" + hex.EncodeToString(sum[:6])
}

// PodcastByFeed finds a library's show by its feed's address.
func (s *Store) PodcastByFeed(ctx context.Context, libraryID int64, feedURL string) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `SELECT p.item_id FROM podcasts p JOIN items i ON i.id = p.item_id
		WHERE i.library_id = ? AND p.feed_url = ?`, libraryID, feedURL).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	return id, err
}

// CreatePodcast adds a show to a library.
func (s *Store) CreatePodcast(ctx context.Context, libraryID int64, feedURL string, info PodcastInfo) (int64, error) {
	t, err := s.db.BeginTx(ctx)
	if err != nil {
		return 0, err
	}
	defer t.Rollback()
	var taken int
	if err := t.QueryRowContext(ctx, `SELECT COUNT(*) FROM items WHERE library_id = ? AND kind = 'podcast' AND parent_id IS NULL AND title = ?`,
		libraryID, info.Title).Scan(&taken); err != nil {
		return 0, err
	}
	if taken > 0 {
		return 0, ErrPodcastTitleTaken
	}
	var id int64
	err = t.QueryRowContext(ctx, `INSERT INTO items (library_id, kind, title, sort_title, overview, poster, genres, meta_at, added_at)
		VALUES (?, 'podcast', ?, ?, ?, ?, ?, ?, ?) RETURNING id`,
		libraryID, info.Title, SortTitle(info.Title), nullStr(info.Description), nullStr(info.Image), encodeGenres(info.Categories), now(), now()).Scan(&id)
	if err != nil {
		return 0, err
	}
	if _, err := t.ExecContext(ctx, `INSERT INTO podcasts (item_id, feed_url, link, author, language) VALUES (?, ?, ?, ?, ?)`,
		id, feedURL, info.Link, info.Author, info.Language); err != nil {
		return 0, err
	}
	return id, t.Commit()
}

// UpdatePodcast keeps a show's details in line with its feed. A show
// renamed to the name of another in the library keeps its old name.
func (s *Store) UpdatePodcast(ctx context.Context, id int64, info PodcastInfo) error {
	_, err := s.db.ExecContext(ctx, `UPDATE items SET
		title = CASE WHEN ? = '' OR EXISTS (SELECT 1 FROM items o WHERE o.library_id = items.library_id AND o.kind = 'podcast'
			AND o.parent_id IS NULL AND o.title = ? AND o.id <> items.id) THEN title ELSE ? END,
		sort_title = CASE WHEN ? = '' OR EXISTS (SELECT 1 FROM items o WHERE o.library_id = items.library_id AND o.kind = 'podcast'
			AND o.parent_id IS NULL AND o.title = ? AND o.id <> items.id) THEN sort_title ELSE ? END,
		overview = COALESCE(?, overview), poster = COALESCE(?, poster), genres = COALESCE(?, genres)
		WHERE id = ? AND kind = 'podcast'`,
		info.Title, info.Title, info.Title, info.Title, info.Title, SortTitle(info.Title),
		nullStr(info.Description), nullStr(info.Image), encodeGenres(info.Categories), id)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE podcasts SET link = ?, author = ?, language = ? WHERE item_id = ?`,
		info.Link, info.Author, info.Language, id)
	return err
}

// PodcastFeedOf returns a show's feed.
func (s *Store) PodcastFeedOf(ctx context.Context, id int64) (PodcastFeed, error) {
	feeds, err := s.podcastFeeds(ctx, "p.item_id = ?", id)
	if err != nil {
		return PodcastFeed{}, err
	}
	if len(feeds) == 0 {
		return PodcastFeed{}, ErrNotFound
	}
	return feeds[0], nil
}

// PodcastFeeds lists a library's shows' feeds, by name.
func (s *Store) PodcastFeeds(ctx context.Context, libraryID int64) ([]PodcastFeed, error) {
	return s.podcastFeeds(ctx, "i.library_id = ?", libraryID)
}

// StalePodcasts lists every show last refreshed before the given time
// (Unix seconds), the longest waiting first.
func (s *Store) StalePodcasts(ctx context.Context, before int64) ([]PodcastFeed, error) {
	return s.podcastFeeds(ctx, "p.refreshed_at < ?", before)
}

func (s *Store) podcastFeeds(ctx context.Context, where string, args ...any) ([]PodcastFeed, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT p.item_id, i.library_id, i.title, p.feed_url, p.link, p.etag, p.last_modified, p.refreshed_at, p.error
		FROM podcasts p JOIN items i ON i.id = p.item_id WHERE `+where+` ORDER BY p.refreshed_at, i.sort_title`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PodcastFeed{}
	for rows.Next() {
		var f PodcastFeed
		if err := rows.Scan(&f.ID, &f.LibraryID, &f.Title, &f.URL, &f.Link, &f.ETag, &f.LastModified, &f.RefreshedAt, &f.Error); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// PodcastRefreshed records a refresh of a show's feed: where the feed is
// now, what to send to ask only for changes next time, and what went wrong
// ("" for nothing).
func (s *Store) PodcastRefreshed(ctx context.Context, id int64, feedURL, etag, lastModified, problem string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE podcasts SET feed_url = ?, etag = ?, last_modified = ?, refreshed_at = ?, error = ? WHERE item_id = ?`,
		feedURL, etag, lastModified, now(), problem, id)
	return err
}

// PutEpisode adds an episode to a show, or brings one it has in line with
// the feed, keeping everyone's place in it. added is true for a new one.
func (s *Store) PutEpisode(ctx context.Context, libraryID, podcastID int64, e PodcastEpisode) (id int64, added bool, err error) {
	var published int64
	var released, year any
	if !e.Published.IsZero() {
		published = e.Published.Unix()
		released, year = e.Published.Format("2006-01-02"), e.Published.Year()
	}
	path := episodePath(podcastID, e.GUID, e.URL)
	var dur any
	if e.DurationMs > 0 {
		dur = e.DurationMs
	}
	t, err := s.db.BeginTx(ctx)
	if err != nil {
		return 0, false, err
	}
	defer t.Rollback()
	err = t.QueryRowContext(ctx, "SELECT item_id FROM podcast_episodes WHERE podcast_id = ? AND guid = ?", podcastID, e.GUID).Scan(&id)
	switch {
	case err == nil:
		// The feed's duration is a guess; a file's own, once probed, isn't
		// replaced by it, but a missing one is filled in.
		_, err = t.ExecContext(ctx, `UPDATE items SET title = ?, sort_title = ?, overview = ?, path = ?, size = ?, mtime = ?,
			duration_ms = COALESCE(duration_ms, ?), container = ?, audio_codec = ?, poster = ?, released = ?, year = ?,
			index_number = ?, disc_number = ? WHERE id = ?`,
			e.Title, SortTitle(e.Title), nullStr(e.Description), path, e.Size, published,
			dur, nullStr(containerOf(e.MimeType)), nullStr(codecOf(e.MimeType)), nullStr(e.Image), released, year,
			nullInt(e.Number), nullInt(e.Season), id)
		if err == nil {
			_, err = t.ExecContext(ctx, `UPDATE podcast_episodes SET published = ?, mime_type = ?, link = ?, episode_type = ? WHERE item_id = ?`,
				published, e.MimeType, e.Link, e.Kind, id)
		}
	case errors.Is(err, sql.ErrNoRows):
		added = true
		err = t.QueryRowContext(ctx, `INSERT INTO items (library_id, parent_id, kind, title, sort_title, overview, path, size, mtime,
			duration_ms, container, audio_codec, poster, released, year, index_number, disc_number, meta_at, added_at)
			VALUES (?, ?, 'podcastEpisode', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`,
			libraryID, podcastID, e.Title, SortTitle(e.Title), nullStr(e.Description), path, e.Size, published,
			dur, nullStr(containerOf(e.MimeType)), nullStr(codecOf(e.MimeType)), nullStr(e.Image), released, year,
			nullInt(e.Number), nullInt(e.Season), now(), now()).Scan(&id)
		if err == nil {
			_, err = t.ExecContext(ctx, `INSERT INTO podcast_episodes (item_id, podcast_id, guid, published, mime_type, link, episode_type)
				VALUES (?, ?, ?, ?, ?, ?, ?)`, id, podcastID, e.GUID, published, e.MimeType, e.Link, e.Kind)
		}
	}
	if err != nil {
		return 0, false, err
	}
	return id, added, t.Commit()
}

// containerOf and codecOf name an episode's file format the way ffprobe
// would, for players deciding whether they can play it as it is.
func containerOf(mime string) string {
	switch mime {
	case "audio/mpeg":
		return "mp3"
	case "audio/mp4", "audio/aac", "audio/x-m4a":
		return "mov,mp4,m4a,3gp,3g2,mj2"
	case "audio/ogg", "audio/opus":
		return "ogg"
	case "audio/flac":
		return "flac"
	case "audio/wav", "audio/x-wav":
		return "wav"
	}
	return ""
}

func codecOf(mime string) string {
	switch mime {
	case "audio/mpeg":
		return "mp3"
	case "audio/mp4", "audio/aac", "audio/x-m4a":
		return "aac"
	case "audio/ogg", "audio/opus":
		return "opus"
	case "audio/flac":
		return "flac"
	case "audio/wav", "audio/x-wav":
		return "pcm_s16le"
	}
	return ""
}

// SetEpisodeDuration records how long an episode really is, once a player
// knows: feeds often leave it out or get it wrong.
func (s *Store) SetEpisodeDuration(ctx context.Context, id, ms int64) error {
	_, err := s.db.ExecContext(ctx, "UPDATE items SET duration_ms = ? WHERE id = ? AND kind = 'podcastEpisode'", ms, id)
	return err
}

// EpisodeInfo is what an episode's page shows besides the item itself.
type EpisodeInfo struct {
	Link string `json:"link,omitempty"`
	// Type is full, trailer or bonus.
	Type string `json:"episodeType"`
}

// EpisodeInfoOf returns an episode's link and type.
func (s *Store) EpisodeInfoOf(ctx context.Context, id int64) (EpisodeInfo, error) {
	var e EpisodeInfo
	err := s.db.QueryRowContext(ctx, "SELECT link, episode_type FROM podcast_episodes WHERE item_id = ?", id).Scan(&e.Link, &e.Type)
	if errors.Is(err, sql.ErrNoRows) {
		return e, ErrNotFound
	}
	return e, err
}

// ---- following ----

// Follow adds a show to the ones a person follows, whose new episodes they
// see on Home.
func (s *Store) Follow(ctx context.Context, userID, podcastID int64) error {
	_, err := s.db.ExecContext(ctx, "INSERT INTO podcast_follows (user_id, item_id, followed_at) VALUES (?, ?, ?) ON CONFLICT DO NOTHING",
		userID, podcastID, time.Now().UnixMilli())
	return err
}

func (s *Store) Unfollow(ctx context.Context, userID, podcastID int64) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM podcast_follows WHERE user_id = ? AND item_id = ?", userID, podcastID)
	return err
}

// Follows says whether a person follows a show.
func (s *Store) Follows(ctx context.Context, userID, podcastID int64) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM podcast_follows WHERE user_id = ? AND item_id = ?", userID, podcastID).Scan(&n)
	return n > 0, err
}

// FollowedPodcasts lists the shows a person follows, the one with the
// newest episode first.
func (s *Store) FollowedPodcasts(ctx context.Context, userID int64) ([]Item, error) {
	return s.queryItems(ctx, "i.kind = 'podcast' AND i.id IN (SELECT f.item_id FROM podcast_follows f WHERE f.user_id = me.id)",
		"(SELECT COALESCE(MAX(pe.published), 0) FROM podcast_episodes pe WHERE pe.podcast_id = i.id) DESC, i.sort_title", userID)
}

// NewEpisodes lists episodes of the shows a person follows that came out
// since the given time (Unix seconds) and that they haven't started, the
// newest first. Trailers are left out.
func (s *Store) NewEpisodes(ctx context.Context, userID, since int64, limit int) ([]Item, error) {
	return s.queryItems(ctx, `i.kind = 'podcastEpisode' AND p.user_id IS NULL
		AND i.parent_id IN (SELECT f.item_id FROM podcast_follows f WHERE f.user_id = me.id)
		AND i.id IN (SELECT pe.item_id FROM podcast_episodes pe WHERE pe.published >= ? AND pe.episode_type <> 'trailer')`,
		"(SELECT pe.published FROM podcast_episodes pe WHERE pe.item_id = i.id) DESC, i.id DESC LIMIT ?", userID, since, limit)
}

// MarkPodcastPlayed marks every episode of a show played for a person, or
// unplayed, which also forgets where they were in each.
func (s *Store) MarkPodcastPlayed(ctx context.Context, userID, podcastID int64, played bool) error {
	if !played {
		_, err := s.db.ExecContext(ctx, `DELETE FROM progress WHERE user_id = ? AND item_id IN (SELECT id FROM items WHERE parent_id = ?)`, userID, podcastID)
		return err
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO progress (user_id, item_id, position_ms, played, updated_at)
		SELECT CAST(? AS BIGINT), id, 0, 1, CAST(? AS BIGINT) FROM items WHERE parent_id = ? AND kind = 'podcastEpisode'
		ON CONFLICT (user_id, item_id) DO UPDATE SET position_ms = 0, played = 1, updated_at = excluded.updated_at`,
		userID, now(), podcastID)
	return err
}

// UnplayedEpisodes counts each of the given shows' episodes a person
// hasn't played, by show.
func (s *Store) UnplayedEpisodes(ctx context.Context, userID int64, podcastIDs []int64) (map[int64]int, error) {
	out := map[int64]int{}
	if len(podcastIDs) == 0 {
		return out, nil
	}
	args := []any{userID}
	for _, id := range podcastIDs {
		args = append(args, id)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT i.parent_id, COUNT(*) FROM items i
		LEFT JOIN progress p ON p.item_id = i.id AND p.user_id = ?
		WHERE i.kind = 'podcastEpisode' AND COALESCE(p.played, 0) = 0 AND i.parent_id IN (`+strings.TrimSuffix(strings.Repeat("?, ", len(podcastIDs)), ", ")+`)
		GROUP BY i.parent_id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}

// PodcastsByFeed finds the shows any library has for these feed
// addresses, by address.
func (s *Store) PodcastsByFeed(ctx context.Context, feedURLs []string) (map[string]int64, error) {
	out := map[string]int64{}
	if len(feedURLs) == 0 {
		return out, nil
	}
	args := make([]any, len(feedURLs))
	for i, u := range feedURLs {
		args[i] = u
	}
	rows, err := s.db.QueryContext(ctx, `SELECT feed_url, MIN(item_id) FROM podcasts WHERE feed_url IN (`+
		strings.TrimSuffix(strings.Repeat("?, ", len(feedURLs)), ", ")+`) GROUP BY feed_url`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var u string
		var id int64
		if err := rows.Scan(&u, &id); err != nil {
			return nil, err
		}
		out[u] = id
	}
	return out, rows.Err()
}

// DeleteItem removes an item and everything under it, such as a show and
// its episodes.
func (s *Store) DeleteItem(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, "DELETE FROM items WHERE id = ?", id)
	return oneRow(res, err)
}
