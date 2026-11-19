package api

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/yeixio/matinee/internal/arr"
	"github.com/yeixio/matinee/internal/store"
)

// Box Office's clean-up rules list watched titles as leaving soon before
// anything deletes them. When Radarr or Sonarr goes to a Box Office with
// that list ("leaving"), Home gets a Leaving soon row of those titles, so
// people can watch them first: playing one takes it off Box Office's list.
// It's a row among Home's others, so every app shows it. Without such a
// Box Office there's no row.

const (
	rowLeaving = "leavingSoon"
	// leavingTTL is how long Box Office's list is kept before asking again.
	leavingTTL = 10 * time.Minute
)

type leavingTitle struct {
	kind string // movie or show
	arr.BoxOfficeLeaving
}

// leavingSoon is the titles in the library that are leaving soon, soonest
// first, that this person may see. A Box Office that can't be reached
// means no row, not an error.
func (s *Server) leavingSoon(ctx context.Context, u store.User, limit int) ([]store.Item, error) {
	var all []leavingTitle
	for _, app := range arr.Apps {
		cl, ok := s.boxOfficeFor(ctx, app, "leaving")
		if !ok {
			continue
		}
		kind := "movie"
		if app == arr.Sonarr {
			kind = "show"
		}
		list, err := cached(&s.arrCache, "boxoffice-leaving:"+string(app)+":"+s.boxOffices.id(app), leavingTTL, func(ctx context.Context) ([]arr.BoxOfficeLeaving, error) {
			callCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			return cl.BoxOfficeLeavingSoon(callCtx)
		})
		if err != nil {
			s.Log.Warn("Box Office's leaving soon list", "app", app, "err", err)
			continue
		}
		for _, t := range list {
			all = append(all, leavingTitle{kind, t})
		}
	}
	slices.SortStableFunc(all, func(a, b leavingTitle) int { return a.LeavesAt.Compare(b.LeavesAt) })
	out := []store.Item{}
	seen := map[int64]bool{}
	for _, t := range all {
		if len(out) >= limit {
			break
		}
		id, err := s.Store.LibraryMatch(ctx, t.kind, t.TMDBID, t.Title, t.Year)
		if err != nil {
			return nil, err
		}
		if id == 0 || seen[id] {
			continue
		}
		seen[id] = true
		it, err := s.Store.Item(ctx, u.ID, id)
		if errors.Is(err, store.ErrNotFound) {
			continue // not one this person may see
		}
		if err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, nil
}
