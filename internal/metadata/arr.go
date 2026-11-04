package metadata

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/yeixio/matinee/internal/arr"
	"github.com/yeixio/matinee/internal/store"
)

// Arr looks movies up through Radarr and shows through Sonarr (or Box
// Office, which answers as both), while TMDB has no key. They already know
// each title's poster, backdrop and description, from their own metadata
// service, so an admin who has connected them needn't get a TMDB key for
// artwork. Seasons and episodes wait for TMDB.
type Arr struct {
	Store *store.Store
	// TMDB, when it has a key, looks everything up instead.
	TMDB *TMDB

	mu      sync.Mutex
	checked time.Time
	apps    map[arr.App]arr.Config
	libs    map[arr.App]arrLibrary
}

type arrLibrary struct {
	at     time.Time
	titles []arr.Title
}

const (
	arrConfigLife  = time.Minute
	arrLibraryLife = 10 * time.Minute
)

func (a *Arr) Name() string { return "arr" }

func arrApp(kind string) arr.App {
	if kind == "movie" {
		return arr.Radarr
	}
	return arr.Sonarr
}

// Kinds lists movies while Radarr is connected and shows while Sonarr is,
// unless TMDB has a key.
func (a *Arr) Kinds() []string {
	if a.TMDB != nil && a.TMDB.Ready() {
		return nil
	}
	apps := a.connected()
	var kinds []string
	if _, ok := apps[arr.Radarr]; ok {
		kinds = append(kinds, "movie")
	}
	if _, ok := apps[arr.Sonarr]; ok {
		kinds = append(kinds, "show")
	}
	return kinds
}

// Forget drops what Arr knows of the connections and libraries, after an
// admin changes a connection.
func (a *Arr) Forget() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.checked, a.apps, a.libs = time.Time{}, nil, nil
}

func (a *Arr) connected() map[arr.App]arr.Config {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.apps != nil && time.Since(a.checked) < arrConfigLife {
		return a.apps
	}
	apps := map[arr.App]arr.Config{}
	for _, app := range arr.Apps {
		v, err := a.Store.Setting(context.Background(), arr.SettingKey(app))
		if err != nil || v == "" {
			continue
		}
		var c arr.Config
		if json.Unmarshal([]byte(v), &c) == nil && c.URL != "" {
			apps[app] = c
		}
	}
	a.apps, a.checked = apps, time.Now()
	return apps
}

func (a *Arr) library(ctx context.Context, cl *arr.Client) ([]arr.Title, error) {
	a.mu.Lock()
	lib, ok := a.libs[cl.App]
	a.mu.Unlock()
	if ok && time.Since(lib.at) < arrLibraryLife {
		return lib.titles, nil
	}
	titles, err := cl.Library(ctx)
	if err != nil {
		return nil, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.libs == nil {
		a.libs = map[arr.App]arrLibrary{}
	}
	a.libs[cl.App] = arrLibrary{at: time.Now(), titles: titles}
	return titles, nil
}

// Lookup finds the title in the app's library, and failing that, the way
// the app's own Add New page does.
func (a *Arr) Lookup(ctx context.Context, q Query) (*store.Metadata, error) {
	if q.Kind != "movie" && q.Kind != "show" {
		return nil, nil
	}
	c, ok := a.connected()[arrApp(q.Kind)]
	if !ok {
		return nil, nil
	}
	cl, err := arr.New(arrApp(q.Kind), c)
	if err != nil {
		return nil, err
	}
	lib, err := a.library(ctx, cl)
	if err != nil {
		return nil, err
	}
	t, ok := arr.Match(lib, q.Title, q.Year)
	if !ok {
		found, err := cl.Search(ctx, q.Title)
		if err != nil {
			return nil, err
		}
		if t, ok = bestFound(found, q.Title, q.Year); !ok {
			return nil, nil
		}
	}
	m := &store.Metadata{
		TMDBID: int64(t.TMDBID), Overview: t.Overview, Year: t.Year, Rating: t.Rating,
		Poster: t.Poster, Backdrop: t.Backdrop,
	}
	if len(t.Genres) > 0 {
		m.Genres = t.Genres
	}
	return m, nil
}

// bestFound picks the search result that is the title: one with its name
// and year, else the first from that year, else (with no year to go by)
// the first.
func bestFound(found []arr.Title, title string, year int) (arr.Title, bool) {
	if t, ok := arr.Match(found, title, year); ok {
		return t, true
	}
	for _, t := range found {
		if year > 0 && t.Year > 0 && t.Year >= year-1 && t.Year <= year+1 {
			return t, true
		}
	}
	if year == 0 && len(found) > 0 {
		return found[0], true
	}
	return arr.Title{}, false
}
