package store

import "context"

// SetAddedAt backdates an item, for tests of what was added when.
func (s *Store) SetAddedAt(ctx context.Context, id, at int64) error {
	_, err := s.db.ExecContext(ctx, "UPDATE items SET added_at = ? WHERE id = ?", at, id)
	return err
}

// ForgetFilterBackfill has the next Open fill in filter terms again.
func (s *Store) ForgetFilterBackfill(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM item_terms WHERE kind IN ('studio', 'country', 'language', 'certification')")
	if err == nil {
		_, err = s.db.ExecContext(ctx, "UPDATE items SET released = NULL")
	}
	if err == nil {
		_, err = s.db.ExecContext(ctx, "DELETE FROM settings WHERE key = 'backfill:filters'")
	}
	return err
}

func (s *Store) BackfillFilters(ctx context.Context) error { return s.backfillFilters(ctx) }

// SetMetaAt backdates an item's online lookup.
func (s *Store) SetMetaAt(ctx context.Context, id, at int64) error {
	_, err := s.db.ExecContext(ctx, "UPDATE items SET meta_at = ? WHERE id = ?", at, id)
	return err
}
