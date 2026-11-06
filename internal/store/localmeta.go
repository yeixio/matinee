package store

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strings"
)

// A title's details can come from the files next to it as well as from
// online: an .nfo file says what it is and describes it. Admins can also
// edit a title's details by hand. Fields set by hand are locked
// (items.locked_fields), so no lookup changes them again. Fields from a
// file (items.nfo_fields) win over online details too, unless the library
// prefers online details (libraries.metadata_mode).

// Metadata modes of a library: where a title's details come from first.
const (
	PreferLocal  = ""          // files next to the media, then online (the default)
	PreferOnline = "online"    // online details win over the files
	LocalOnly    = "localOnly" // only the files: nothing is looked up online
)

// ValidMetadataMode reports whether m is a metadata mode.
func ValidMetadataMode(m string) bool { return m == PreferLocal || m == PreferOnline || m == LocalOnly }

// SetMetadataMode sets where a library's details come from first.
func (s *Store) SetMetadataMode(ctx context.Context, libraryID int64, mode string) error {
	res, err := s.db.ExecContext(ctx, "UPDATE libraries SET metadata_mode = ? WHERE id = ?", mode, libraryID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// Editable fields: the ones an .nfo file or an admin can set.
const (
	FieldTitle    = "title"
	FieldYear     = "year"
	FieldOverview = "overview"
	FieldGenres   = "genres"
	FieldRating   = "rating"
	FieldPoster   = "poster"
	FieldBackdrop = "backdrop"
)

// fieldIn is SQL for "field f is in the comma list in column col".
func fieldIn(col, f string) string {
	return "(',' || COALESCE(" + col + ", '') || ',' LIKE '%," + f + ",%')"
}

// keptFromOnline is SQL for "an online lookup leaves items.f alone": an
// admin locked it, or it came from a file and the library doesn't prefer
// online details. It's used inside UPDATE items.
func keptFromOnline(f string) string {
	return "(" + fieldIn("items.locked_fields", f) + " OR (" + fieldIn("items.nfo_fields", f) +
		" AND COALESCE((SELECT l.metadata_mode FROM libraries l WHERE l.id = items.library_id), '') <> 'online'))"
}

// keptFromScan is SQL for "a scan of the file leaves items.f alone": an
// admin locked it or a file next to it set it, both of which say more
// than the file's name.
func keptFromScan(f string) string {
	return "(" + fieldIn("items.locked_fields", f) + " OR " + fieldIn("items.nfo_fields", f) + ")"
}

// Local is what a file next to the media says about a title.
type Local struct {
	Title    string
	Year     int
	Overview string
	Genres   []string
	Rating   float64
	Poster   string // a local path or a web address
	Backdrop string
	// TMDBID pins the match, when the file says which title it is.
	TMDBID int64
	// Hash identifies the file's contents; ApplyLocal does nothing when
	// it's what was applied last.
	Hash string
}

func (l Local) fields() []string {
	var f []string
	add := func(ok bool, name string) {
		if ok {
			f = append(f, name)
		}
	}
	add(l.Title != "", FieldTitle)
	add(l.Year != 0, FieldYear)
	add(l.Overview != "", FieldOverview)
	add(len(l.Genres) > 0, FieldGenres)
	add(l.Rating > 0, FieldRating)
	add(l.Poster != "", FieldPoster)
	add(l.Backdrop != "", FieldBackdrop)
	return f
}

// LocalHash returns the hash of the file last applied to an item, "" for none.
func (s *Store) LocalHash(ctx context.Context, id int64) (string, error) {
	var h string
	err := s.db.QueryRowContext(ctx, "SELECT COALESCE(nfo_hash, '') FROM items WHERE id = ?", id).Scan(&h)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return h, err
}

// ApplyLocal stores what a file next to the media says about an item, or,
// with l nil, forgets that the file said anything (it's gone). Fields an
// admin locked stay as they are. A TMDB id the file names becomes the
// item's match, looked up again, unless an admin picked the match.
func (s *Store) ApplyLocal(ctx context.Context, id int64, l *Local) error {
	if l == nil {
		_, err := s.db.ExecContext(ctx, "UPDATE items SET nfo_fields = '', nfo_hash = '' WHERE id = ? AND nfo_hash <> ''", id)
		return err
	}
	// Each field the file names, unless an admin locked it.
	var sets []string
	var args []any
	set := func(f, expr string, arg any) {
		sets = append(sets, f+" = CASE WHEN "+fieldIn("items.locked_fields", f)+" THEN "+f+" ELSE "+expr+" END")
		args = append(args, arg)
	}
	if l.Title != "" {
		set(FieldTitle, "?", l.Title)
		set("sort_title", "?", SortTitle(l.Title))
		// sort_title follows title's lock.
		sets[len(sets)-1] = strings.Replace(sets[len(sets)-1], fieldIn("items.locked_fields", "sort_title"), fieldIn("items.locked_fields", FieldTitle), 1)
	}
	if l.Year != 0 {
		set(FieldYear, "?", l.Year)
	}
	if l.Overview != "" {
		set(FieldOverview, "?", l.Overview)
	}
	if len(l.Genres) > 0 {
		set(FieldGenres, "?", encodeGenres(l.Genres))
	}
	if l.Rating > 0 {
		set(FieldRating, "?", l.Rating)
	}
	if l.Poster != "" {
		// A cover an admin picked wins over the file's.
		set(FieldPoster, "CASE WHEN poster LIKE 'chosen:%' THEN poster ELSE ? END", l.Poster)
	}
	if l.Backdrop != "" {
		set(FieldBackdrop, "?", l.Backdrop)
	}
	sets = append(sets, "nfo_fields = ?", "nfo_hash = ?")
	args = append(args, strings.Join(l.fields(), ","), l.Hash, id)
	if _, err := s.db.ExecContext(ctx, "UPDATE items SET "+strings.Join(sets, ", ")+" WHERE id = ?", args...); err != nil {
		return err
	}
	if l.TMDBID != 0 {
		_, err := s.db.ExecContext(ctx, `UPDATE items SET tmdb_id = ?, meta_at = NULL, details_at = NULL, translations_at = NULL
			WHERE id = ? AND match_locked = 0 AND (tmdb_id IS NULL OR tmdb_id <> ?)`, l.TMDBID, id, l.TMDBID)
		return err
	}
	return nil
}

// Edit is an admin's change to a title's details. Nil fields stay as
// they are.
type Edit struct {
	Title    *string   `json:"title,omitempty"`
	Year     *int      `json:"year,omitempty"`
	Overview *string   `json:"overview,omitempty"`
	Genres   *[]string `json:"genres,omitempty"`
	Rating   *float64  `json:"rating,omitempty"`
}

// EditDetails sets the fields of e on an item and locks them, so no
// lookup or scan changes them again.
func (s *Store) EditDetails(ctx context.Context, id int64, e Edit) error {
	var sets []string
	var args []any
	var locked []string
	if e.Title != nil {
		t := strings.TrimSpace(*e.Title)
		if t == "" {
			return errors.New("a title can't be empty")
		}
		sets = append(sets, "title = ?", "sort_title = ?")
		args = append(args, t, SortTitle(t))
		locked = append(locked, FieldTitle)
	}
	if e.Year != nil {
		sets = append(sets, "year = ?")
		args = append(args, nullInt(*e.Year))
		locked = append(locked, FieldYear)
	}
	if e.Overview != nil {
		sets = append(sets, "overview = ?")
		args = append(args, nullStr(strings.TrimSpace(*e.Overview)))
		locked = append(locked, FieldOverview)
	}
	if e.Genres != nil {
		var g []string
		for _, x := range *e.Genres {
			if x = strings.TrimSpace(x); x != "" && !slices.Contains(g, x) {
				g = append(g, x)
			}
		}
		sets = append(sets, "genres = ?")
		args = append(args, encodeGenres(append([]string{}, g...)))
		locked = append(locked, FieldGenres)
	}
	if e.Rating != nil {
		sets = append(sets, "rating = ?")
		args = append(args, nullFloat(min(max(*e.Rating, 0), 10)))
		locked = append(locked, FieldRating)
	}
	if len(sets) == 0 {
		return nil
	}
	have, err := s.LockedFields(ctx, id)
	if err != nil {
		return err
	}
	for _, f := range locked {
		if !slices.Contains(have, f) {
			have = append(have, f)
		}
	}
	sets = append(sets, "locked_fields = ?")
	args = append(args, strings.Join(have, ","), id)
	_, err = s.db.ExecContext(ctx, "UPDATE items SET "+strings.Join(sets, ", ")+" WHERE id = ?", args...)
	return err
}

// LockedFields lists the fields of an item an admin set by hand.
func (s *Store) LockedFields(ctx context.Context, id int64) ([]string, error) {
	var v string
	err := s.db.QueryRowContext(ctx, "SELECT COALESCE(locked_fields, '') FROM items WHERE id = ?", id).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if v == "" {
		return []string{}, err
	}
	return strings.Split(v, ","), err
}

// UnlockDetails lets lookups and scans set an item's details again, and
// has it looked up again (unless its match is locked) and its file
// details read again.
func (s *Store) UnlockDetails(ctx context.Context, id int64) error {
	// A file's own name gives its title and year back on the next scan.
	_, err := s.db.ExecContext(ctx, `UPDATE items SET locked_fields = '', nfo_hash = '',
		mtime = CASE WHEN path IS NOT NULL THEN 0 ELSE mtime END,
		meta_at = CASE WHEN match_locked = 1 THEN meta_at END, details_at = NULL, translations_at = NULL
		WHERE id = ?`, id)
	return err
}
