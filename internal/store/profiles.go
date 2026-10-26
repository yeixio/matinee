package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"strconv"
)

// A profile is a user without a password that belongs to an account, so
// people who share a sign-in (or the home network's guest account) each
// keep their own progress, watch list and theme. Kids profiles only see
// the libraries they're given, and in libraries filtered by rating only
// titles rated up to their level.

// Library modes for kids profiles. A library without one is hidden.
const (
	LibraryAll   = "all"   // everything in it, rated or not
	LibraryRated = "rated" // titles rated up to the profile's level
)

// Profile is a profile as admins manage it.
type Profile struct {
	User
	// Libraries maps library ids to LibraryAll or LibraryRated; only
	// meaningful for kids profiles.
	Libraries map[int64]string `json:"libraries"`
}

// ProfileChanges are what an admin sets on a profile. Nil fields stay as they are.
type ProfileChanges struct {
	Name      *string
	Avatar    *string
	KidsLevel *int
	// PINHash replaces the PIN; "" removes it.
	PINHash   *string
	Libraries map[int64]string
}

// Profiles lists an account's profiles, in the order they were made.
func (s *Store) Profiles(ctx context.Context, accountID int64) ([]Profile, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+userCols+" FROM users u WHERE u.parent_id = ? ORDER BY u.id", accountID)
	if err != nil {
		return nil, err
	}
	var out []Profile
	for rows.Next() {
		u, err := scanUserRow(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, Profile{User: u})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		if out[i].Libraries, err = s.profileLibraries(ctx, out[i].ID); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *Store) profileLibraries(ctx context.Context, userID int64) (map[int64]string, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT library_id, mode FROM profile_libraries WHERE user_id = ?", userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]string{}
	for rows.Next() {
		var id int64
		var mode string
		if err := rows.Scan(&id, &mode); err != nil {
			return nil, err
		}
		out[id] = mode
	}
	return out, rows.Err()
}

// Profile returns one profile.
func (s *Store) Profile(ctx context.Context, id int64) (Profile, error) {
	u, err := s.Viewer(ctx, id)
	if err != nil {
		return Profile{}, err
	}
	if u.AccountID == 0 {
		return Profile{}, ErrNotFound
	}
	libs, err := s.profileLibraries(ctx, id)
	return Profile{User: u, Libraries: libs}, err
}

// CreateProfile adds a profile to an account.
func (s *Store) CreateProfile(ctx context.Context, accountID int64, c ProfileChanges) (Profile, error) {
	// The stored username only has to be unique; people see the display name.
	b := make([]byte, 6)
	rand.Read(b)
	var id int64
	err := s.db.QueryRowContext(ctx,
		"INSERT INTO users (username, password_hash, is_admin, created_at, parent_id) VALUES (?, '', 0, ?, ?) RETURNING id",
		"profile-"+hex.EncodeToString(b), now(), accountID).Scan(&id)
	if err != nil {
		return Profile{}, err
	}
	if err := s.UpdateProfile(ctx, id, c); err != nil {
		return Profile{}, err
	}
	return s.Profile(ctx, id)
}

// UpdateProfile changes a profile, or the PIN and avatar of an account.
func (s *Store) UpdateProfile(ctx context.Context, id int64, c ProfileChanges) error {
	t, err := s.db.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer t.Rollback()
	set := func(col string, v any) error {
		_, err := t.ExecContext(ctx, "UPDATE users SET "+col+" = ? WHERE id = ?", v, id)
		return err
	}
	if c.Name != nil {
		if err := set("display_name", *c.Name); err != nil {
			return err
		}
	}
	if c.Avatar != nil {
		if err := set("avatar", *c.Avatar); err != nil {
			return err
		}
	}
	if c.KidsLevel != nil {
		if err := set("kids_level", *c.KidsLevel); err != nil {
			return err
		}
	}
	if c.PINHash != nil {
		if err := set("pin_hash", *c.PINHash); err != nil {
			return err
		}
	}
	if c.Libraries != nil {
		if _, err := t.ExecContext(ctx, "DELETE FROM profile_libraries WHERE user_id = ?", id); err != nil {
			return err
		}
		for lib, mode := range c.Libraries {
			if mode != LibraryAll && mode != LibraryRated {
				continue
			}
			if _, err := t.ExecContext(ctx, "INSERT INTO profile_libraries (user_id, library_id, mode) VALUES (?, ?, ?)", id, lib, mode); err != nil {
				return err
			}
		}
	}
	return t.Commit()
}

// DeleteProfile removes a profile with its progress and watch list.
func (s *Store) DeleteProfile(ctx context.Context, id int64) error {
	// Per-user settings are keys like "theme:user:<id>".
	if _, err := s.db.ExecContext(ctx, "DELETE FROM settings WHERE key LIKE ?", "%:user:"+strconv.FormatInt(id, 10)); err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx, "DELETE FROM users WHERE id = ? AND parent_id IS NOT NULL", id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// LibraryVisible reports whether a user may see a library at all.
func (s *Store) LibraryVisible(ctx context.Context, u User, libraryID int64) (bool, error) {
	if u.KidsLevel == 0 {
		return true, nil
	}
	var n int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM profile_libraries WHERE user_id = ? AND library_id = ?", u.ID, libraryID).Scan(&n)
	return n > 0, err
}

// itemRatingLevel is an item's rating level, or its show's for seasons and episodes.
const itemRatingLevel = `COALESCE(i.rating_level,
	(SELECT x.rating_level FROM items x WHERE x.id = i.parent_id),
	(SELECT g.rating_level FROM items x JOIN items g ON g.id = x.parent_id WHERE x.id = i.parent_id))`

// visibleToMe keeps the items the user in itemFrom's "me" may see: all of
// them, unless they're a kids profile.
const visibleToMe = ` AND (
	NOT EXISTS (SELECT 1 FROM users ku WHERE ku.id = me.id AND ku.kids_level > 0) OR
	EXISTS (SELECT 1 FROM profile_libraries pl JOIN users ku ON ku.id = pl.user_id
		WHERE pl.user_id = me.id AND pl.library_id = i.library_id AND (pl.mode = 'all' OR ` + itemRatingLevel + ` <= ku.kids_level)))`

// BackfillRatingLevels fills in rating levels for titles whose details were
// looked up before levels were kept. level turns a certification into one.
func (s *Store) BackfillRatingLevels(ctx context.Context, level func(cert string) int) (int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, details FROM items
		WHERE rating_level IS NULL AND kind IN ('movie', 'show') AND details LIKE '%"certification"%'`)
	if err != nil {
		return 0, err
	}
	levels := map[int64]int{}
	for rows.Next() {
		var id int64
		var js string
		if err := rows.Scan(&id, &js); err != nil {
			rows.Close()
			return 0, err
		}
		var d Details
		if json.Unmarshal([]byte(js), &d) == nil {
			if l := level(d.Certification); l > 0 {
				levels[id] = l
			}
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	for id, l := range levels {
		if _, err := s.db.ExecContext(ctx, "UPDATE items SET rating_level = ? WHERE id = ?", l, id); err != nil {
			return 0, err
		}
	}
	return len(levels), nil
}
