package metadata

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/yeixio/matinee/internal/store"
)

// Daily shows, such as talk shows and the news, are named by air date.
// The scanner files their episodes under a season named for the year,
// with the month and day as the episode number (2024 and 315 for March
// 15, 2024). TMDB numbers them its own way, so they're found by air date.

// dailySeason reports whether a season number is a daily show's year.
func dailySeason(n int) bool { return n >= 1900 }

type dailyEpisode struct {
	ID          int64   `json:"id"`
	Name        string  `json:"name"`
	Overview    string  `json:"overview"`
	VoteAverage float64 `json:"vote_average"`
	StillPath   string  `json:"still_path"`
	AirDate     string  `json:"air_date"`
}

type dailySeasonInfo struct {
	ID         int64          `json:"id"`
	Number     int            `json:"season_number"`
	Overview   string         `json:"overview"`
	PosterPath string         `json:"poster_path"`
	AirDate    string         `json:"air_date"`
	Episodes   []dailyEpisode `json:"episodes"`
}

// dailyCache keeps the seasons TMDB lists for daily shows a while, since a
// scan looks up hundreds of episodes of one show in a row.
type dailyCache struct {
	mu   sync.Mutex
	at   map[string]time.Time
	data map[string]any
}

const dailyCacheFor = 6 * time.Hour

func (c *dailyCache) get(key string) (any, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if at, ok := c.at[key]; ok && time.Since(at) < dailyCacheFor {
		return c.data[key], true
	}
	return nil, false
}

func (c *dailyCache) put(key string, v any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.at == nil {
		c.at, c.data = map[string]time.Time{}, map[string]any{}
	}
	c.at[key], c.data[key] = time.Now(), v
}

// yearSeasons lists a show's TMDB seasons that aired in a year, and the
// one before them, since a season can run from one year into the next.
func (t *TMDB) yearSeasons(ctx context.Context, showID int64, year int) ([]int, error) {
	key := fmt.Sprintf("show/%d", showID)
	var seasons []dailySeasonInfo
	if v, ok := t.daily.get(key); ok {
		seasons = v.([]dailySeasonInfo)
	} else {
		var res struct {
			Seasons []dailySeasonInfo `json:"seasons"`
		}
		if err := t.get(ctx, fmt.Sprintf("/tv/%d", showID), nil, &res); err != nil {
			if err == errNotFound {
				return nil, nil
			}
			return nil, err
		}
		seasons = res.Seasons
		t.daily.put(key, seasons)
	}
	var out []int
	prev := -1
	for _, s := range seasons {
		y, _ := strconv.Atoi(strings.SplitN(s.AirDate, "-", 2)[0])
		switch {
		case y == year:
			if len(out) == 0 && prev >= 0 {
				out = append(out, prev)
			}
			out = append(out, s.Number)
		case y > 0 && y < year:
			prev = s.Number
		}
	}
	if len(out) == 0 && prev >= 0 {
		out = append(out, prev) // a season that started earlier and runs on
	}
	return out, nil
}

func (t *TMDB) dailySeasonDetails(ctx context.Context, showID int64, season int) (*dailySeasonInfo, error) {
	key := fmt.Sprintf("season/%d/%d", showID, season)
	if v, ok := t.daily.get(key); ok {
		return v.(*dailySeasonInfo), nil
	}
	var res dailySeasonInfo
	if err := t.get(ctx, fmt.Sprintf("/tv/%d/season/%d", showID, season), nil, &res); err != nil {
		if err == errNotFound {
			return nil, nil
		}
		return nil, err
	}
	t.daily.put(key, &res)
	return &res, nil
}

// dailyEpisodeLookup finds a daily show's episode by the day it aired.
func (t *TMDB) dailyEpisodeLookup(ctx context.Context, q Query) (*store.Metadata, error) {
	date := fmt.Sprintf("%04d-%02d-%02d", q.Season, q.Episode/100, q.Episode%100)
	seasons, err := t.yearSeasons(ctx, q.ShowID, q.Season)
	if err != nil {
		return nil, err
	}
	for _, n := range seasons {
		s, err := t.dailySeasonDetails(ctx, q.ShowID, n)
		if err != nil {
			return nil, err
		}
		if s == nil {
			continue
		}
		for _, e := range s.Episodes {
			if e.AirDate == date {
				return &store.Metadata{TMDBID: e.ID, Title: e.Name, Overview: e.Overview, Rating: e.VoteAverage,
					Poster: t.image("w780", e.StillPath)}, nil
			}
		}
	}
	return nil, nil
}

// dailySeasonLookup finds the TMDB season of a daily show's year: the
// first that aired in it.
func (t *TMDB) dailySeasonLookup(ctx context.Context, q Query) (*store.Metadata, error) {
	seasons, err := t.yearSeasons(ctx, q.ShowID, q.Season)
	if err != nil {
		return nil, err
	}
	for _, n := range seasons {
		s, err := t.dailySeasonDetails(ctx, q.ShowID, n)
		if err != nil || s == nil {
			return nil, err
		}
		if strings.HasPrefix(s.AirDate, strconv.Itoa(q.Season)) {
			return &store.Metadata{TMDBID: s.ID, Overview: s.Overview, Year: q.Season, Poster: t.image("w500", s.PosterPath)}, nil
		}
	}
	return nil, nil
}
