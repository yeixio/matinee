package store

import (
	"context"
)

// NextEpisode is the episode after the given one: the next in its season,
// else the first of the following season. Specials (season 0) only lead to
// other specials, and regular seasons never lead into specials. It returns
// ErrNotFound after a show's last episode, or for anything that isn't an
// episode. Only episodes the user may see count (see visibleToMe).
func (s *Store) NextEpisode(ctx context.Context, userID int64, ep Item) (Item, error) {
	if ep.Kind != "episode" || ep.ParentID == nil {
		return Item{}, ErrNotFound
	}
	season, err := s.Item(ctx, userID, *ep.ParentID)
	if err != nil {
		return Item{}, err
	}
	episodes, err := s.Children(ctx, userID, season.ID)
	if err != nil {
		return Item{}, err
	}
	for i, e := range episodes {
		if e.ID == ep.ID {
			for _, n := range episodes[i+1:] {
				if n.Playable {
					return n, nil
				}
			}
			break
		}
	}
	if season.ParentID == nil || isSpecials(season) {
		return Item{}, ErrNotFound
	}
	seasons, err := s.Children(ctx, userID, *season.ParentID)
	if err != nil {
		return Item{}, err
	}
	after := false
	for _, se := range seasons {
		if se.ID == season.ID {
			after = true
			continue
		}
		if !after || se.Kind != "season" || isSpecials(se) {
			continue
		}
		episodes, err := s.Children(ctx, userID, se.ID)
		if err != nil {
			return Item{}, err
		}
		for _, n := range episodes {
			if n.Playable {
				return n, nil
			}
		}
	}
	return Item{}, ErrNotFound
}

func isSpecials(season Item) bool { return season.IndexNumber != nil && *season.IndexNumber == 0 }

// Similar picks a title to suggest after a show's last episode: the most
// related show the user hasn't finished, else the most related title.
func (s *Store) Similar(ctx context.Context, userID int64, show Item) (Item, error) {
	related, err := s.Related(ctx, userID, show)
	if err != nil {
		return Item{}, err
	}
	for _, r := range related {
		if r.Kind == "show" && !r.Played {
			return r, nil
		}
	}
	for _, r := range related {
		if !r.Played {
			return r, nil
		}
	}
	return Item{}, ErrNotFound
}
