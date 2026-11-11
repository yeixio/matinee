package store

import (
	"context"

	"github.com/yeixio/matinee/internal/namefix"
)

// namesRepair is the version of repairNames a database has had; raising
// it runs it again.
const namesRepair = "1"

// repairNames writes titles read from oddly written file names the way
// they were meant (see internal/namefix): "Amélie", with its accent
// as a separate mark as a Mac writes it, as "Amélie", and garbled
// "Ã\u0086on Flux" as "Æon Flux". Scans did this from Matinee 2026-11 on,
// but only for files they read again. A title an admin set is left
// alone. Movies, shows, albums, artists and books that were looked up
// and not matched are looked up again, since the online databases didn't
// know the old spelling but do know the new one; a movie that then
// matches one already in the library becomes another version of it.
func (s *Store) repairNames(ctx context.Context) error {
	if v, err := s.Setting(ctx, "backfill:names"); err != nil || v == namesRepair {
		return err
	}
	rows, err := s.db.QueryContext(ctx, "SELECT id, title FROM items WHERE NOT "+fieldIn("locked_fields", FieldTitle))
	if err != nil {
		return err
	}
	type fix struct {
		id    int64
		title string
	}
	var fixes []fix
	for rows.Next() {
		var f fix
		if err := rows.Scan(&f.id, &f.title); err != nil {
			rows.Close()
			return err
		}
		if !ascii(f.title) {
			if t := namefix.Repair(f.title); t != f.title {
				fixes = append(fixes, fix{f.id, t})
			}
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	t, err := s.db.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer t.Rollback()
	for _, f := range fixes {
		if _, err := t.ExecContext(ctx, `UPDATE items SET title = ?, sort_title = ?,
			meta_at = CASE WHEN tmdb_id IS NULL AND mbid IS NULL AND match_locked = 0 THEN NULL ELSE meta_at END
			WHERE id = ?`, f.title, SortTitle(f.title), f.id); err != nil {
			return err
		}
	}
	if _, err := t.ExecContext(ctx, `INSERT INTO settings (key, value) VALUES ('backfill:names', ?)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value`, namesRepair); err != nil {
		return err
	}
	return t.Commit()
}

func ascii(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}
