package store

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
)

// Some shows, anime most of all, are named by each episode's number
// counted from the show's first: "Show - 0123.mkv". The scanner files
// such an episode as season 1, episode 123 (items.absolute_number says
// how it was named) until its show is matched online. Then it's put in
// the season it's in, in the order the show is watched in
// (items.episode_order: aired, dvd or absolute), and items.placed_order
// records the order it was put in by, so rescans keep it there. When its
// place online differs from where it's shown, as in the DVD order,
// items.tmdb_season and tmdb_episode say where to look it up.

// Episode orders a show's absolute-numbered episodes can be put in.
const (
	OrderAired    = "aired"    // the seasons as they aired (the default)
	OrderDVD      = "dvd"      // the seasons of the DVD and Blu-ray sets
	OrderAbsolute = "absolute" // one long season, numbered from the first
)

// ValidEpisodeOrder reports whether o is an episode order.
func ValidEpisodeOrder(o string) bool { return o == OrderAired || o == OrderDVD || o == OrderAbsolute }

// keptPlace is SQL for "a rescan of a file leaves the episode where it
// was put": it's named by the same number and it's still the same show's.
const keptPlace = `(items.placed_order <> '' AND excluded.absolute_number IS NOT NULL
	AND items.absolute_number = excluded.absolute_number AND items.absolute_compact = excluded.absolute_compact
	AND (SELECT x.parent_id FROM items x WHERE x.id = excluded.parent_id) = (SELECT y.parent_id FROM items y WHERE y.id = items.parent_id))`

// orderOf is SQL for a show's episode order, from its row sh.
const orderOf = "COALESCE(NULLIF(sh.episode_order, ''), 'aired')"

// ShowToPlace is a matched show with episodes to put in seasons.
type ShowToPlace struct {
	ID     int64
	TMDBID int64
	Order  string
}

// ShowsToPlace lists a library's matched shows that have episodes named
// by absolute number not yet put in seasons by the show's order.
func (s *Store) ShowsToPlace(ctx context.Context, libraryID int64) ([]ShowToPlace, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT sh.id, sh.tmdb_id, `+orderOf+` FROM items sh
		WHERE sh.library_id = ? AND sh.kind = 'show' AND sh.tmdb_id IS NOT NULL AND EXISTS (
			SELECT 1 FROM items se JOIN items e ON e.parent_id = se.id
			WHERE se.parent_id = sh.id AND e.absolute_number IS NOT NULL AND e.placed_order <> `+orderOf+`)
		ORDER BY sh.id`, libraryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ShowToPlace
	for rows.Next() {
		var sp ShowToPlace
		if err := rows.Scan(&sp.ID, &sp.TMDBID, &sp.Order); err != nil {
			return nil, err
		}
		out = append(out, sp)
	}
	return out, rows.Err()
}

// AbsoluteEpisode is an episode named by its number from the show's first.
type AbsoluteEpisode struct {
	ID      int64
	Number  int
	Compact bool // the number might be a season and episode run together
}

// AbsoluteEpisodes lists a show's episodes named by absolute number.
func (s *Store) AbsoluteEpisodes(ctx context.Context, showID int64) ([]AbsoluteEpisode, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT e.id, e.absolute_number, e.absolute_compact FROM items se JOIN items e ON e.parent_id = se.id
		WHERE se.parent_id = ? AND e.absolute_number IS NOT NULL ORDER BY e.absolute_number, e.id`, showID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AbsoluteEpisode
	for rows.Next() {
		var e AbsoluteEpisode
		if err := rows.Scan(&e.ID, &e.Number, &e.Compact); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// Place is where an episode goes: the season and episode it's shown as,
// and where it's found online.
type Place struct {
	Season, Episode         int
	TMDBSeason, TMDBEpisode int
}

// PlaceEpisode puts an episode named by absolute number in its season by
// the show's order. It's looked up online again when its place changed.
func (s *Store) PlaceEpisode(ctx context.Context, libraryID, showID, id int64, p Place, order string) (moved bool, err error) {
	seasonID, err := s.EnsureContainer(ctx, libraryID, &showID, "season", "Season "+strconv.Itoa(p.Season), &p.Season, nil)
	if err != nil {
		return false, err
	}
	var tmdbSeason, tmdbEpisode any
	if p.TMDBSeason != p.Season || p.TMDBEpisode != p.Episode {
		tmdbSeason, tmdbEpisode = p.TMDBSeason, p.TMDBEpisode
	}
	var parent sql.NullInt64
	var idx, ts, te sql.NullInt64
	err = s.db.QueryRowContext(ctx, "SELECT parent_id, index_number, tmdb_season, tmdb_episode FROM items WHERE id = ?", id).
		Scan(&parent, &idx, &ts, &te)
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrNotFound
	}
	if err != nil {
		return false, err
	}
	moved = parent.Int64 != seasonID || int(idx.Int64) != p.Episode ||
		ts.Valid != (tmdbSeason != nil) || ts.Valid && (int(ts.Int64) != p.TMDBSeason || int(te.Int64) != p.TMDBEpisode)
	q := "UPDATE items SET placed_order = ? WHERE id = ?"
	args := []any{order, id}
	if moved {
		// Its title from online was another episode's; the next lookup
		// gives it its own.
		q = `UPDATE items SET parent_id = ?, index_number = ?, tmdb_season = ?, tmdb_episode = ?, placed_order = ?,
			title = CASE WHEN ` + keptFromScan(FieldTitle) + ` THEN title ELSE ? END,
			sort_title = CASE WHEN ` + keptFromScan(FieldTitle) + ` THEN sort_title ELSE ? END,
			meta_at = NULL, details_at = NULL, translations_at = NULL WHERE id = ?`
		title := "Episode " + strconv.Itoa(p.Episode)
		args = []any{seasonID, p.Episode, tmdbSeason, tmdbEpisode, order, title, SortTitle(title), id}
	}
	_, err = s.db.ExecContext(ctx, q, args...)
	return moved, err
}

// MarkPlaced records that a show's episode was left where the scan put
// it, as when the show has no such episode online.
func (s *Store) MarkPlaced(ctx context.Context, id int64, order string) error {
	_, err := s.db.ExecContext(ctx, "UPDATE items SET placed_order = ? WHERE id = ?", order, id)
	return err
}

// EpisodePlace says how an episode is looked up online: absolute is true
// for one named by absolute number, placed once it was put in its season
// by its show's order, and tmdbSeason and tmdbEpisode are where it's
// found online when that's not where it's shown (0 otherwise).
func (s *Store) EpisodePlace(ctx context.Context, id int64) (absolute, placed bool, tmdbSeason, tmdbEpisode int, err error) {
	var ts, te sql.NullInt64
	err = s.db.QueryRowContext(ctx, `
		SELECT e.absolute_number IS NOT NULL, e.placed_order = (SELECT `+orderOf+` FROM items se JOIN items sh ON sh.id = se.parent_id WHERE se.id = e.parent_id),
			e.tmdb_season, e.tmdb_episode
		FROM items e WHERE e.id = ?`, id).Scan(&absolute, &placed, &ts, &te)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return absolute, placed, int(ts.Int64), int(te.Int64), err
}

// MovedEpisodes lists the episodes of a season that are found online
// somewhere other than where they're shown.
func (s *Store) MovedEpisodes(ctx context.Context, seasonID int64) (map[int64]bool, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id FROM items WHERE parent_id = ? AND tmdb_season IS NOT NULL", seasonID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// EpisodeOrder returns a show's episode order, and whether it has any
// episodes named by absolute number, for which the order matters.
func (s *Store) EpisodeOrder(ctx context.Context, showID int64) (order string, absolute bool, err error) {
	err = s.db.QueryRowContext(ctx, `
		SELECT `+orderOf+`, EXISTS (SELECT 1 FROM items se JOIN items e ON e.parent_id = se.id
			WHERE se.parent_id = sh.id AND e.absolute_number IS NOT NULL)
		FROM items sh WHERE sh.id = ? AND sh.kind = 'show'`, showID).Scan(&order, &absolute)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return order, absolute, err
}

// AbsoluteNumber returns the number an episode's file is named by from
// the show's first, 0 when it's named otherwise.
func (s *Store) AbsoluteNumber(ctx context.Context, id int64) (int, error) {
	var n sql.NullInt64
	err := s.db.QueryRowContext(ctx, "SELECT absolute_number FROM items WHERE id = ?", id).Scan(&n)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return int(n.Int64), err
}

// SetEpisodeOrder sets the order a show's absolute-numbered episodes are
// put in seasons by; they're put in again on the next lookup.
func (s *Store) SetEpisodeOrder(ctx context.Context, showID int64, order string) error {
	if !ValidEpisodeOrder(order) {
		return errors.New("unknown episode order")
	}
	stored := order
	if order == OrderAired {
		stored = ""
	}
	res, err := s.db.ExecContext(ctx, "UPDATE items SET episode_order = ? WHERE id = ? AND kind = 'show'", stored, showID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
