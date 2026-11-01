package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strings"
)

// Friends are people from outside the household an admin lets in with an
// invite link. A friend account works like any other account, except that
// it (and its profiles) sees only the libraries it was given. Libraries
// added later stay hidden from friends until an admin shares them.

// Invite is a link an admin made to let one friend in.
type Invite struct {
	Code      string  `json:"code"`
	Name      string  `json:"name,omitempty"` // who it's for, as the admin wrote it
	Libraries []int64 `json:"libraries"`
	CreatedBy string  `json:"createdBy,omitempty"`
	CreatedAt int64   `json:"createdAt"`
	ExpiresAt int64   `json:"expiresAt"`
	// UsedBy is the account made from it, once someone used it.
	UsedBy string `json:"usedBy,omitempty"`
	UsedAt int64  `json:"usedAt,omitempty"`
}

var (
	// ErrInviteUsed means someone already made an account with the invite.
	ErrInviteUsed = errors.New("this invite was already used")
	// ErrInviteExpired means the invite is too old.
	ErrInviteExpired = errors.New("this invite has expired")
	// ErrUsernameTaken means another account has the name.
	ErrUsernameTaken = errors.New("that username is taken")
)

// inviteAlphabet leaves out letters and digits people mix up.
const inviteAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// newInviteCode makes a code that can't be guessed: 16 characters, 80 bits.
func newInviteCode() string {
	b := make([]byte, 16)
	rand.Read(b)
	for i := range b {
		b[i] = inviteAlphabet[int(b[i])%len(inviteAlphabet)]
	}
	return string(b)
}

// NormalizeInviteCode takes a code as someone typed or pasted it.
func NormalizeInviteCode(code string) string {
	return strings.ToUpper(strings.NewReplacer("-", "", " ", "").Replace(strings.TrimSpace(code)))
}

// CreateInvite makes an invite that lasts until expiresAt (Unix seconds).
func (s *Store) CreateInvite(ctx context.Context, createdBy int64, name string, libraries []int64, expiresAt int64) (Invite, error) {
	libraries = cleanIDs(libraries)
	js, _ := json.Marshal(libraries)
	inv := Invite{Code: newInviteCode(), Name: name, Libraries: libraries, CreatedAt: now(), ExpiresAt: expiresAt}
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO invites (code, name, libraries, created_by, created_at, expires_at) VALUES (?, ?, ?, ?, ?, ?)",
		inv.Code, name, string(js), createdBy, inv.CreatedAt, expiresAt)
	return inv, err
}

const inviteCols = `v.code, v.name, v.libraries, COALESCE(c.username, ''), v.created_at, v.expires_at,
	COALESCE(f.username, ''), COALESCE(v.used_at, 0), v.used_by IS NOT NULL`

const inviteFrom = ` FROM invites v LEFT JOIN users c ON c.id = v.created_by LEFT JOIN users f ON f.id = v.used_by`

func scanInvite(r scanner) (Invite, bool, error) {
	var inv Invite
	var libs string
	var used bool
	err := r.Scan(&inv.Code, &inv.Name, &libs, &inv.CreatedBy, &inv.CreatedAt, &inv.ExpiresAt, &inv.UsedBy, &inv.UsedAt, &used)
	if errors.Is(err, sql.ErrNoRows) {
		return inv, false, ErrNotFound
	}
	if err != nil {
		return inv, false, err
	}
	if json.Unmarshal([]byte(libs), &inv.Libraries) != nil || inv.Libraries == nil {
		inv.Libraries = []int64{}
	}
	return inv, used, nil
}

// Invites lists invites, newest first. Used and expired ones stay listed for
// a while so admins can see what happened to them.
func (s *Store) Invites(ctx context.Context) ([]Invite, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+inviteCols+inviteFrom+" WHERE v.expires_at > ? OR v.used_at > ? ORDER BY v.created_at DESC",
		now()-30*86400, now()-30*86400)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Invite{}
	for rows.Next() {
		inv, _, err := scanInvite(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, inv)
	}
	return out, rows.Err()
}

// InviteByCode finds an invite that can still be used.
func (s *Store) InviteByCode(ctx context.Context, code string) (Invite, error) {
	inv, used, err := scanInvite(s.db.QueryRowContext(ctx, "SELECT "+inviteCols+inviteFrom+" WHERE v.code = ?", NormalizeInviteCode(code)))
	switch {
	case err != nil:
		return inv, err
	case used:
		return inv, ErrInviteUsed
	case inv.ExpiresAt <= now():
		return inv, ErrInviteExpired
	}
	return inv, nil
}

// DeleteInvite takes an invite back. Accounts already made from it stay.
func (s *Store) DeleteInvite(ctx context.Context, code string) error {
	res, err := s.db.ExecContext(ctx, "DELETE FROM invites WHERE code = ?", NormalizeInviteCode(code))
	return oneRow(res, err)
}

// AcceptInvite makes a friend account from an invite, with the invite's
// libraries. The invite can't be used again.
func (s *Store) AcceptInvite(ctx context.Context, code, username, passwordHash string) (User, error) {
	inv, err := s.InviteByCode(ctx, code)
	if err != nil {
		return User{}, err
	}
	if _, err := s.UserByName(ctx, username); err == nil {
		return User{}, ErrUsernameTaken
	} else if !errors.Is(err, ErrNotFound) {
		return User{}, err
	}
	t, err := s.db.BeginTx(ctx)
	if err != nil {
		return User{}, err
	}
	defer t.Rollback()
	var id int64
	if err := t.QueryRowContext(ctx,
		"INSERT INTO users (username, password_hash, is_admin, created_at, friend) VALUES (?, ?, 0, ?, 1) RETURNING id",
		username, passwordHash, now()).Scan(&id); err != nil {
		if strings.Contains(strings.ToUpper(err.Error()), "UNIQUE") {
			return User{}, ErrUsernameTaken
		}
		return User{}, err
	}
	// Only one account per invite, even when two people use it at once.
	res, err := t.ExecContext(ctx, "UPDATE invites SET used_by = ?, used_at = ? WHERE code = ? AND used_by IS NULL", id, now(), inv.Code)
	if err := oneRow(res, err); errors.Is(err, ErrNotFound) {
		return User{}, ErrInviteUsed
	} else if err != nil {
		return User{}, err
	}
	if err := setFriendLibraries(ctx, t, id, inv.Libraries); err != nil {
		return User{}, err
	}
	if err := t.Commit(); err != nil {
		return User{}, err
	}
	return User{ID: id, Username: username, Friend: true, hash: passwordHash}, nil
}

func setFriendLibraries(ctx context.Context, t *tx, userID int64, libraries []int64) error {
	if _, err := t.ExecContext(ctx, "DELETE FROM friend_libraries WHERE user_id = ?", userID); err != nil {
		return err
	}
	for _, lib := range cleanIDs(libraries) {
		// Libraries removed meanwhile are skipped.
		if _, err := t.ExecContext(ctx, `INSERT INTO friend_libraries (user_id, library_id)
			SELECT CAST(? AS BIGINT), id FROM libraries WHERE id = ?`, userID, lib); err != nil {
			return err
		}
	}
	return nil
}

// SetFriend makes an account a friend with the given libraries, or (with
// friend false) a household account that sees everything again.
func (s *Store) SetFriend(ctx context.Context, userID int64, friend bool, libraries []int64) error {
	u, err := s.UserByID(ctx, userID)
	if err != nil {
		return err
	}
	if friend && u.IsAdmin {
		return ErrAdminFriend
	}
	t, err := s.db.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer t.Rollback()
	if _, err := t.ExecContext(ctx, "UPDATE users SET friend = ? WHERE id = ?", b2i(friend), userID); err != nil {
		return err
	}
	if !friend {
		libraries = nil
	}
	if err := setFriendLibraries(ctx, t, userID, libraries); err != nil {
		return err
	}
	return t.Commit()
}

// ErrAdminFriend means an admin can't be limited to some libraries.
var ErrAdminFriend = errors.New("admins see every library; make them a regular account first")

// FriendLibraries maps each friend account to the libraries it sees.
func (s *Store) FriendLibraries(ctx context.Context) (map[int64][]int64, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT fl.user_id, fl.library_id FROM friend_libraries fl
		JOIN users u ON u.id = fl.user_id WHERE u.friend = 1 ORDER BY fl.library_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64][]int64{}
	for rows.Next() {
		var user, lib int64
		if err := rows.Scan(&user, &lib); err != nil {
			return nil, err
		}
		out[user] = append(out[user], lib)
	}
	return out, rows.Err()
}

// cleanIDs sorts ids and drops duplicates and non-positive ones.
func cleanIDs(ids []int64) []int64 {
	out := []int64{}
	for _, id := range ids {
		if id > 0 {
			out = append(out, id)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// friendHidden is true when the user in itemFrom's "me" is a friend (or a
// friend's profile) who wasn't given the library i.library_id.
const friendHidden = `EXISTS (SELECT 1 FROM users fu JOIN users fa ON fa.id = COALESCE(fu.parent_id, fu.id)
	WHERE fu.id = me.id AND fa.friend = 1 AND NOT EXISTS (
		SELECT 1 FROM friend_libraries fl WHERE fl.user_id = fa.id AND fl.library_id = i.library_id))`
