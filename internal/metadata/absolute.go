package metadata

import (
	"context"
	"fmt"
	"sort"

	"github.com/yeixio/matinee/internal/store"
)

// Episodes named by their number from the show's first ("Show - 0123")
// are put in seasons once their show is matched: by the seasons as they
// aired, the DVD sets' seasons, or one long season (see store/absolute.go).

// EpisodeList is a show's episodes in one of its orders.
type EpisodeList struct {
	// Episodes are the show's episodes from the first, specials left
	// out, each where it's shown in the order and where it's found online.
	Episodes []store.Place
	// Aired counts the episodes of each season as it aired, specials left
	// out, to tell whether 104 can be season 1, episode 4.
	Aired map[int]int
	// Japanese says the show is from Japan, where a number like 104 is
	// the 104th episode much more often than season 1, episode 4.
	Japanese bool
}

// EpisodeLister lists a show's episodes in an order; TMDB is one. It
// returns nil, nil when the show isn't there.
type EpisodeLister interface {
	EpisodeList(ctx context.Context, showID int64, order string) (*EpisodeList, error)
}

// place finds where an episode named by number n goes in list. compact
// says n might be a season and an episode run together, as 104.
func place(n int, compact bool, order string, list *EpisodeList) (store.Place, bool) {
	s, e := n/100, n%100
	asSeason := compact && order != store.OrderAbsolute && e >= 1 && list.Aired[s] >= e
	if asSeason && (n > len(list.Episodes) || !list.Japanese) {
		return store.Place{Season: s, Episode: e, TMDBSeason: s, TMDBEpisode: e}, true
	}
	if n >= 1 && n <= len(list.Episodes) {
		return list.Episodes[n-1], true
	}
	if asSeason {
		return store.Place{Season: s, Episode: e, TMDBSeason: s, TMDBEpisode: e}, true
	}
	return store.Place{}, false
}

// PlaceEpisodes puts the episodes named by absolute number of a library's
// matched shows in their seasons. It returns how many moved.
func (f *Fetcher) PlaceEpisodes(ctx context.Context, lib store.Library) int {
	var lister EpisodeLister
	for _, p := range f.active() {
		if l, ok := p.(EpisodeLister); ok {
			lister = l
			break
		}
	}
	if lister == nil {
		return 0
	}
	shows, err := f.Store.ShowsToPlace(ctx, lib.ID)
	if err != nil {
		f.Log.Error("list shows to place", "err", err)
		return 0
	}
	moved := 0
	for _, sh := range shows {
		if ctx.Err() != nil {
			break
		}
		list, err := lister.EpisodeList(ctx, sh.TMDBID, sh.Order)
		if err != nil {
			// Try again with the next lookup; the provider may be down.
			f.Log.Warn("episode list", "show", sh.TMDBID, "err", err)
			continue
		}
		if list == nil {
			list = &EpisodeList{}
		}
		eps, err := f.Store.AbsoluteEpisodes(ctx, sh.ID)
		if err != nil {
			f.Log.Error("list episodes to place", "err", err)
			continue
		}
		for _, e := range eps {
			p, ok := place(e.Number, e.Compact, sh.Order, list)
			if !ok {
				// Not online (yet): it stays as the scan filed it.
				err = f.Store.MarkPlaced(ctx, e.ID, sh.Order)
			} else {
				var m bool
				if m, err = f.Store.PlaceEpisode(ctx, lib.ID, sh.ID, e.ID, p, sh.Order); m {
					moved++
				}
			}
			if err != nil {
				f.Log.Error("place episode", "err", err)
				break
			}
		}
	}
	if moved > 0 {
		if err := f.Store.PruneEmpty(ctx, lib.ID); err != nil {
			f.Log.Error("prune empty seasons", "err", err)
		}
	}
	return moved
}

// EpisodeList lists a show's episodes in an order: the seasons as they
// aired, or a DVD or absolute order from TMDB's episode groups. A show
// without the order asked for gets the aired one, in one long season
// for the absolute order.
func (t *TMDB) EpisodeList(ctx context.Context, showID int64, order string) (*EpisodeList, error) {
	key := fmt.Sprintf("show-info/%d", showID)
	var show *tmdbShowSeasons
	if v, ok := t.daily.get(key); ok {
		show = v.(*tmdbShowSeasons)
	} else {
		var res tmdbShowSeasons
		if err := t.get(ctx, fmt.Sprintf("/tv/%d", showID), nil, &res); err != nil {
			if err == errNotFound {
				return nil, nil
			}
			return nil, err
		}
		show = &res
		t.daily.put(key, show)
	}
	list := &EpisodeList{Aired: map[int]int{}, Japanese: show.OriginalLanguage == "ja"}
	for _, c := range show.OriginCountry {
		list.Japanese = list.Japanese || c == "JP"
	}
	seasons := append([]tmdbSeasonCount(nil), show.Seasons...)
	sort.Slice(seasons, func(i, j int) bool { return seasons[i].Number < seasons[j].Number })
	var aired []store.Place
	for _, s := range seasons {
		if s.Number <= 0 {
			continue
		}
		list.Aired[s.Number] = s.EpisodeCount
		for e := 1; e <= s.EpisodeCount; e++ {
			aired = append(aired, store.Place{Season: s.Number, Episode: e, TMDBSeason: s.Number, TMDBEpisode: e})
		}
	}
	list.Episodes = aired
	groupType := map[string]int{store.OrderDVD: 3, store.OrderAbsolute: 2}[order]
	if groupType != 0 {
		group, err := t.episodeGroup(ctx, showID, groupType)
		if err != nil {
			return nil, err
		}
		if len(group) > 0 {
			list.Episodes = group
		}
	}
	if order == store.OrderAbsolute {
		// One long season, numbered from the first.
		flat := make([]store.Place, len(list.Episodes))
		for i, p := range list.Episodes {
			flat[i] = store.Place{Season: 1, Episode: i + 1, TMDBSeason: p.TMDBSeason, TMDBEpisode: p.TMDBEpisode}
		}
		list.Episodes = flat
	}
	return list, nil
}

type tmdbSeasonCount struct {
	Number       int `json:"season_number"`
	EpisodeCount int `json:"episode_count"`
}

type tmdbShowSeasons struct {
	OriginalLanguage string            `json:"original_language"`
	OriginCountry    []string          `json:"origin_country"`
	Seasons          []tmdbSeasonCount `json:"seasons"`
}

// episodeGroup lists a show's episodes in its largest TMDB episode group
// of a type (2 absolute, 3 DVD), each group a season, specials left out;
// nil when it has none.
func (t *TMDB) episodeGroup(ctx context.Context, showID int64, groupType int) ([]store.Place, error) {
	key := fmt.Sprintf("episode-group/%d/%d", showID, groupType)
	if v, ok := t.daily.get(key); ok {
		return v.([]store.Place), nil
	}
	var groups struct {
		Results []struct {
			ID           string `json:"id"`
			Type         int    `json:"type"`
			EpisodeCount int    `json:"episode_count"`
		} `json:"results"`
	}
	if err := t.get(ctx, fmt.Sprintf("/tv/%d/episode_groups", showID), nil, &groups); err != nil && err != errNotFound {
		return nil, err
	}
	best, most := "", 0
	for _, g := range groups.Results {
		if g.Type == groupType && g.EpisodeCount > most {
			best, most = g.ID, g.EpisodeCount
		}
	}
	var out []store.Place
	if best != "" {
		var res struct {
			Groups []struct {
				Order    int `json:"order"`
				Episodes []struct {
					Season  int `json:"season_number"`
					Episode int `json:"episode_number"`
					Order   int `json:"order"`
				} `json:"episodes"`
			} `json:"groups"`
		}
		if err := t.get(ctx, "/tv/episode_group/"+best, nil, &res); err != nil && err != errNotFound {
			return nil, err
		}
		sort.SliceStable(res.Groups, func(i, j int) bool { return res.Groups[i].Order < res.Groups[j].Order })
		season := 0
		for _, g := range res.Groups {
			specials := true
			for _, e := range g.Episodes {
				specials = specials && e.Season == 0
			}
			if specials {
				continue
			}
			season++
			eps := g.Episodes
			sort.SliceStable(eps, func(i, j int) bool { return eps[i].Order < eps[j].Order })
			for i, e := range eps {
				out = append(out, store.Place{Season: season, Episode: i + 1, TMDBSeason: e.Season, TMDBEpisode: e.Episode})
			}
		}
	}
	t.daily.put(key, out)
	return out, nil
}
