package api

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"sync"
	"time"

	"github.com/yeixio/matinee/internal/arr"
	"github.com/yeixio/matinee/internal/store"
)

// Box Office can clear out titles that have been watched (its clean-up
// rules), and only Matinee knows what has been. So when Radarr or Sonarr
// goes to a Box Office that says it takes the report ("watched"), Matinee
// tells it, for each movie or show, how many people have played it, how
// many have finished it, and when, and how far into a show anyone has got
// (so Box Office can fetch the next episodes as they're watched): counts,
// never who. Matinee deletes
// nothing itself, and real Radarr and Sonarr are told nothing.

const (
	// watchedLook is how often Matinee works out what has been watched.
	watchedLook = 15 * time.Minute
	// watchedResend is how often it reports even when nothing has changed,
	// so Box Office knows the report is current.
	watchedResend = 6 * time.Hour
)

// watchReports is what Matinee last told each Box Office.
type watchReports struct {
	mu     sync.Mutex
	looked time.Time
	sent   map[string]watchSent // by app and Box Office id
}

type watchSent struct {
	sum [sha256.Size]byte
	at  time.Time
}

// reportWatched tells each Box Office what has been watched, when it has
// changed or it's been a while. force skips the wait between looks.
func (s *Server) reportWatched(ctx context.Context, force bool) {
	w := &s.watchReports
	w.mu.Lock()
	defer w.mu.Unlock()
	if !force && time.Since(w.looked) < watchedLook {
		return
	}
	looked := false
	for _, app := range arr.Apps {
		if !s.boxOffices.has(app, "watched") {
			continue
		}
		cl, _, err := s.arrClient(ctx, app)
		if err != nil {
			continue
		}
		looked = true
		kind := "movie"
		if app == arr.Sonarr {
			kind = "show"
		}
		people, titles, err := s.Store.WatchReport(ctx, kind)
		if err != nil {
			s.Log.Warn("couldn't work out what has been watched", "kind", kind, "err", err)
			continue
		}
		report := watchedTitles(titles)
		b, _ := json.Marshal(struct {
			People int
			Titles []arr.BoxOfficeWatched
		}{people, report})
		key, sum := string(app)+":"+s.boxOffices.id(app), sha256.Sum256(b)
		if last, ok := w.sent[key]; ok && last.sum == sum && time.Since(last.at) < watchedResend {
			continue
		}
		callCtx, cancel := context.WithTimeout(ctx, time.Minute)
		err = cl.BoxOfficeReportWatched(callCtx, people, report)
		cancel()
		if err != nil {
			s.Log.Warn("couldn't tell Box Office what has been watched", "app", app, "err", err)
			continue
		}
		if w.sent == nil {
			w.sent = map[string]watchSent{}
		}
		w.sent[key] = watchSent{sum: sum, at: time.Now()}
	}
	if looked {
		w.looked = time.Now()
	}
}

func watchedTitles(titles []store.WatchedTitle) []arr.BoxOfficeWatched {
	at := func(unix int64) *time.Time {
		if unix <= 0 {
			return nil
		}
		t := time.Unix(unix, 0).UTC()
		return &t
	}
	out := make([]arr.BoxOfficeWatched, len(titles))
	for i, t := range titles {
		out[i] = arr.BoxOfficeWatched{TMDBID: t.TMDBID, LastPlayed: at(t.LastPlayed), Viewers: t.Viewers, FinishedBy: t.FinishedBy, FinishedAt: at(t.FinishedAt), FurthestSeason: t.FurthestSeason, FurthestEpisode: t.FurthestEpisode}
	}
	return out
}
