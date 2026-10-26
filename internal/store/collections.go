package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Collections gather titles under a name. A smart one is a saved filter
// (ItemFilter), so it fills itself as the library changes and is evaluated
// for whoever is looking: "unwatched" means unwatched by them. A manual one
// is a list someone keeps by hand.
//
// A collection with no owner is shared: everyone sees it and admins look
// after it. Others belong to the person who made them. Box sets (TMDB's
// collections, like a trilogy) aren't stored here; see BoxSets.

const (
	CollectionSmart  = "smart"
	CollectionManual = "manual"
)

// CollectionSorts are the orders a collection can have besides its own:
// a smart collection's filter order or a manual one's hand-kept order.
// "shuffle" is a different order each day.
var CollectionSorts = map[string]bool{
	"title": true, "added": true, "released": true, "year": true, "rating": true,
	"runtime": true, "lastPlayed": true, "shuffle": true,
}

type SavedCollection struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"` // smart or manual
	// OwnerID is who made it; nil for a shared collection.
	OwnerID *int64 `json:"ownerId,omitempty"`
	Shared  bool   `json:"shared"`
	// Filter picks a smart collection's titles; nil for a manual one.
	Filter *ItemFilter `json:"filter,omitempty"`
	// Sort is one of CollectionSorts, or empty for the collection's own order.
	Sort  string `json:"sort,omitempty"`
	Order string `json:"order,omitempty"` // asc or desc; empty is the sort's natural one
	// PinHome shows the collection as a row on Home, PinLibrary as a row on
	// that library's page.
	PinHome    bool   `json:"pinHome"`
	PinLibrary *int64 `json:"pinLibrary,omitempty"`
	// Starter names a built-in collection, such as "recent".
	Starter   string `json:"starter,omitempty"`
	CreatedAt int64  `json:"createdAt"` // milliseconds
	UpdatedAt int64  `json:"updatedAt"` // milliseconds
}

const collectionCols = `id, name, kind, user_id, COALESCE(filter, ''), sort, sort_order, pin_home, pin_library, starter, created_at, updated_at`

func scanCollection(r scanner) (SavedCollection, error) {
	var c SavedCollection
	var owner, lib sql.NullInt64
	var filter string
	err := r.Scan(&c.ID, &c.Name, &c.Kind, &owner, &filter, &c.Sort, &c.Order, &c.PinHome, &lib, &c.Starter, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return c, err
	}
	if owner.Valid {
		c.OwnerID = &owner.Int64
	}
	c.Shared = !owner.Valid
	if lib.Valid {
		c.PinLibrary = &lib.Int64
	}
	if filter != "" {
		var f ItemFilter
		if err := json.Unmarshal([]byte(filter), &f); err != nil {
			return c, err
		}
		c.Filter = &f
	}
	return c, nil
}

func encodeFilter(c SavedCollection) (any, error) {
	if c.Kind != CollectionSmart || c.Filter == nil {
		return nil, nil
	}
	b, err := json.Marshal(c.Filter)
	return string(b), err
}

func nullID(p *int64) any {
	if p == nil {
		return nil
	}
	return *p
}

// CreateCollection saves a new collection; ID and times are filled in.
func (s *Store) CreateCollection(ctx context.Context, c SavedCollection) (SavedCollection, error) {
	filter, err := encodeFilter(c)
	if err != nil {
		return c, err
	}
	// Collections list in the order they were made, so created_at is in
	// milliseconds and always after the last one's.
	var last int64
	if err := s.db.QueryRowContext(ctx, "SELECT COALESCE(MAX(created_at), 0) FROM collections").Scan(&last); err != nil {
		return c, err
	}
	c.ID, c.CreatedAt, c.UpdatedAt = newID(), max(time.Now().UnixMilli(), last+1), time.Now().UnixMilli()
	c.Shared = c.OwnerID == nil
	_, err = s.db.ExecContext(ctx, `INSERT INTO collections
		(id, name, kind, user_id, filter, sort, sort_order, pin_home, pin_library, starter, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.ID, c.Name, c.Kind, nullID(c.OwnerID), filter, c.Sort, c.Order, b2i(c.PinHome), nullID(c.PinLibrary), c.Starter, c.CreatedAt, c.UpdatedAt)
	return c, err
}

// UpdateCollection saves a collection's name, filter, order and pins. Its
// kind and owner stay as they are.
func (s *Store) UpdateCollection(ctx context.Context, c SavedCollection) (SavedCollection, error) {
	filter, err := encodeFilter(c)
	if err != nil {
		return c, err
	}
	c.UpdatedAt = time.Now().UnixMilli()
	res, err := s.db.ExecContext(ctx, `UPDATE collections SET name = ?, filter = ?, sort = ?, sort_order = ?,
		pin_home = ?, pin_library = ?, updated_at = ? WHERE id = ?`,
		c.Name, filter, c.Sort, c.Order, b2i(c.PinHome), nullID(c.PinLibrary), c.UpdatedAt, c.ID)
	if err != nil {
		return c, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return c, ErrNotFound
	}
	return c, nil
}

func (s *Store) DeleteCollection(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM collections WHERE id = ?", id)
	return err
}

// Collection returns one collection, whoever it belongs to.
func (s *Store) Collection(ctx context.Context, id string) (SavedCollection, error) {
	c, err := scanCollection(s.db.QueryRowContext(ctx, "SELECT "+collectionCols+" FROM collections WHERE id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrNotFound
	}
	return c, err
}

// Collections lists the shared collections and the user's own, shared first.
func (s *Store) Collections(ctx context.Context, userID int64) ([]SavedCollection, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+collectionCols+` FROM collections
		WHERE user_id IS NULL OR user_id = ? ORDER BY CASE WHEN user_id IS NULL THEN 0 ELSE 1 END, created_at, id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SavedCollection{}
	for rows.Next() {
		c, err := scanCollection(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ManualItems lists a manual collection's titles in the order they were added.
func (s *Store) ManualItems(ctx context.Context, userID int64, collectionID string) ([]Item, error) {
	return s.queryItems(ctx, "i.id IN (SELECT item_id FROM collection_items WHERE collection_id = ?)",
		"(SELECT position FROM collection_items ci WHERE ci.collection_id = ? AND ci.item_id = i.id)",
		userID, collectionID, collectionID)
}

// AddToCollection puts an item at the end of a manual collection. Adding it
// again keeps its place.
func (s *Store) AddToCollection(ctx context.Context, collectionID string, itemID int64) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO collection_items (collection_id, item_id, position)
		SELECT ?, ?, COALESCE(MAX(position), 0) + 1 FROM collection_items WHERE collection_id = ?
		ON CONFLICT DO NOTHING`, collectionID, itemID, collectionID)
	return err
}

func (s *Store) RemoveFromCollection(ctx context.Context, collectionID string, itemID int64) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM collection_items WHERE collection_id = ? AND item_id = ?", collectionID, itemID)
	return err
}

// ItemCollections lists the IDs of the manual collections holding an item.
func (s *Store) ItemCollections(ctx context.Context, itemID int64) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT collection_id FROM collection_items WHERE item_id = ?", itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// starters are the shared collections a new server starts with. People can
// change or delete them like any other; they're only added once.
var starters = []struct {
	key, name, sort string
	pinHome         bool
	filter          url.Values
}{
	{"recent", "Recently added", "added", false, url.Values{"kind": {"movie", "show"}, "addedWithin": {"30"}}},
	{"unwatched", "Unwatched", "", false, url.Values{"kind": {"movie", "show"}, "watched": {"notStarted"}}},
	{"tonight", "Short enough for tonight", "shuffle", true, url.Values{"kind": {"movie"}, "runtimeMax": {"100"}, "watched": {"notStarted"}}},
}

const settingStartersAdded = "collections_starters_added"

// AddStarterCollections adds the built-in collections the first time it runs.
func (s *Store) AddStarterCollections(ctx context.Context) error {
	done, err := s.Setting(ctx, settingStartersAdded)
	if err != nil || done != "" {
		return err
	}
	for _, st := range starters {
		f, err := ParseItemFilter(st.filter)
		if err != nil {
			return err
		}
		if _, err := s.CreateCollection(ctx, SavedCollection{
			Name: st.name, Kind: CollectionSmart, Filter: &f, Sort: st.sort, PinHome: st.pinHome, Starter: st.key,
		}); err != nil {
			return err
		}
	}
	return s.SetSetting(ctx, settingStartersAdded, strconv.FormatInt(now(), 10))
}

// BoxSet is one of TMDB's collections, such as a trilogy, with at least two
// of its titles in the library. Box sets fill themselves from metadata.
type BoxSet struct {
	ID    int64  `json:"id"` // TMDB collection id
	Name  string `json:"name"`
	Count int    `json:"count"`
	// ItemIDs are its titles in the library, oldest first, for artwork.
	ItemIDs []int64 `json:"itemIds"`
}

// BoxSets lists the box sets with two or more titles in the library the
// user can see, by name.
func (s *Store) BoxSets(ctx context.Context, userID int64) ([]BoxSet, error) {
	// Only titles the user may see count (see visibleToMe).
	rows, err := s.db.QueryContext(ctx, `
		SELECT t.term, i.id, COALESCE(i.details, '')`+itemFrom+` JOIN item_terms t ON t.item_id = i.id
		WHERE t.kind = 'collection'`+visibleToMe+`
		ORDER BY t.term, COALESCE(i.year, 9999), i.sort_title`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byID := map[int64]*BoxSet{}
	var order []int64
	for rows.Next() {
		var term, details string
		var itemID int64
		if err := rows.Scan(&term, &itemID, &details); err != nil {
			return nil, err
		}
		id, err := strconv.ParseInt(term, 10, 64)
		if err != nil {
			continue
		}
		b := byID[id]
		if b == nil {
			b = &BoxSet{ID: id}
			byID[id] = b
			order = append(order, id)
		}
		b.Count++
		b.ItemIDs = append(b.ItemIDs, itemID)
		if b.Name == "" && details != "" {
			var d Details
			if json.Unmarshal([]byte(details), &d) == nil && d.Collection != nil {
				b.Name = d.Collection.Name
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := []BoxSet{}
	for _, id := range order {
		if b := byID[id]; b.Count > 1 {
			out = append(out, *b)
		}
	}
	slices.SortFunc(out, func(a, b BoxSet) int { return strings.Compare(SortTitle(a.Name), SortTitle(b.Name)) })
	return out, nil
}

// BoxSet returns one box set, or ErrNotFound when the library has none of it.
func (s *Store) BoxSet(ctx context.Context, userID, id int64) (BoxSet, error) {
	all, err := s.BoxSets(ctx, userID)
	if err != nil {
		return BoxSet{}, err
	}
	for _, b := range all {
		if b.ID == id {
			return b, nil
		}
	}
	return BoxSet{}, ErrNotFound
}
