package store

import (
	"context"
	"errors"
	"sort"
	"strconv"
)

// How much each thing two titles share counts toward them being related.
// Genres alone don't make titles related; a shared director or a TMDB
// recommendation nearly does.
const (
	relatedThreshold = 6
	relatedLimit     = 20
)

// Related lists titles in the library like the given movie or show, best
// first: ones TMDB recommends, ones by the same director or creator, with
// the same leads, keywords and genres. Members of its collection are left
// out; see CollectionItems.
func (s *Store) Related(ctx context.Context, userID int64, it Item) ([]Item, error) {
	scores := map[int64]int{}
	add := func(q string) error {
		rows, err := s.db.QueryContext(ctx, q, it.ID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id int64
			var score int
			if err := rows.Scan(&id, &score); err != nil {
				return err
			}
			scores[id] += score
		}
		return rows.Err()
	}
	err := add(`
		SELECT o.item_id, SUM(CASE m.kind WHEN 'rec' THEN 6 WHEN 'keyword' THEN 2 WHEN 'collection' THEN -1000 ELSE 1 END)
		FROM item_terms m JOIN item_terms o ON o.kind = m.kind AND o.term = m.term AND o.item_id <> m.item_id
		WHERE m.item_id = ?
		GROUP BY o.item_id`)
	if err != nil {
		return nil, err
	}
	err = add(`
		SELECT o.item_id, SUM(CASE m.kind WHEN 'crew' THEN 6 ELSE 2 END)
		FROM credits m JOIN credits o ON o.person_id = m.person_id AND o.kind = m.kind AND o.item_id <> m.item_id
		WHERE m.item_id = ? AND (
			(m.kind = 'cast' AND m.ord < 10 AND o.ord < 10) OR
			(m.kind = 'crew' AND m.role IN ('Director', 'Creator') AND o.role = m.role))
		GROUP BY o.item_id`)
	if err != nil {
		return nil, err
	}
	type cand struct {
		id    int64
		score int
	}
	var cands []cand
	for id, score := range scores {
		if score >= relatedThreshold {
			cands = append(cands, cand{id, score})
		}
	}
	sort.Slice(cands, func(i, j int) bool {
		if cands[i].score != cands[j].score {
			return cands[i].score > cands[j].score
		}
		return cands[i].id < cands[j].id
	})
	out := []Item{}
	for _, c := range cands {
		if len(out) == relatedLimit {
			break
		}
		r, err := s.Item(ctx, userID, c.id)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if r.Kind == it.Kind {
			out = append(out, r)
		}
	}
	return out, nil
}

// CollectionItems lists the library's titles in a collection, oldest first.
func (s *Store) CollectionItems(ctx context.Context, userID, collectionID int64) ([]Item, error) {
	return s.queryItems(ctx,
		"i.id IN (SELECT item_id FROM item_terms WHERE kind = 'collection' AND term = ?)",
		"COALESCE(i.year, 9999), i.sort_title", userID, strconv.FormatInt(collectionID, 10))
}
