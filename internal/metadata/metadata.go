// Package metadata looks up descriptions and artwork from online providers.
// Providers are an extension point: TMDB (movies and TV) and MusicBrainz
// (music) are built in, and metadata plugins will implement the same interface.
package metadata

import (
	"context"
	"encoding/json"
	"log/slog"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/yeixio/matinee/internal/store"
)

// Query describes the item to identify.
type Query struct {
	Kind    string // movie, show, season, episode, artist or album
	Title   string
	Year    int
	ShowID  int64 // provider id of the season's or episode's show
	Season  int
	Episode int
	Artist  string // the album's artist
	// For a plugin's kinds: the item's file and what the plugin keeps with it.
	Path string
	Data json.RawMessage
}

// Provider identifies items with an online database. Lookup returns nil, nil
// when nothing matches.
type Provider interface {
	Name() string
	// Kinds lists the item kinds the provider looks up.
	Kinds() []string
	Lookup(ctx context.Context, q Query) (*store.Metadata, error)
}

// GenreProvider is a Provider that can look up the genres of a movie or show
// it matched earlier, by its id.
type GenreProvider interface {
	Genres(ctx context.Context, kind string, id int64) ([]string, error)
}

// Fetcher fills in metadata for newly scanned items.
type Fetcher struct {
	Store     *store.Store
	Providers []Provider
	// Plugins lists providers that plugins add for their own item kinds, as
	// they are now; nil for none.
	Plugins func() []Provider
	// Awards looks up titles' awards, if set.
	Awards AwardSource
	Log    *slog.Logger

	translating      sync.Mutex  // one TranslateAll at a time
	translatePending atomic.Bool // TranslateAll was asked to run (again)
}

func (f *Fetcher) all() []Provider {
	if f.Plugins == nil {
		return f.Providers
	}
	return append(slices.Clip(f.Providers), f.Plugins()...)
}

// Kinds lists every item kind some provider looks up.
func (f *Fetcher) Kinds() []string {
	var kinds []string
	for _, p := range f.all() {
		kinds = append(kinds, p.Kinds()...)
	}
	return kinds
}

// active lists the providers that look something up now: TMDB without a
// key doesn't.
func (f *Fetcher) active() []Provider {
	var ps []Provider
	for _, p := range f.all() {
		if len(p.Kinds()) > 0 {
			ps = append(ps, p)
		}
	}
	return ps
}

func (f *Fetcher) provider(kind string) Provider {
	for _, p := range f.all() {
		if slices.Contains(p.Kinds(), kind) {
			return p
		}
	}
	return nil
}

// FetchLibrary looks up every item in the library that hasn't been looked up yet.
func (f *Fetcher) FetchLibrary(ctx context.Context, lib store.Library) {
	items, err := f.Store.NeedsMetadata(ctx, lib.ID, f.Kinds())
	if err != nil {
		f.Log.Error("list items needing metadata", "err", err)
		return
	}
	matched := 0
	for _, it := range items {
		if ctx.Err() != nil {
			return
		}
		q, state := f.query(ctx, it)
		switch state {
		case retryLater:
			continue
		case noMatch:
			f.Store.ApplyMetadata(ctx, it.ID, nil)
			continue
		}
		p := f.provider(it.Kind)
		m, err := p.Lookup(ctx, q)
		if err != nil {
			// Leave it for the next scan; the provider may be down.
			f.Log.Warn("metadata lookup", "provider", p.Name(), "title", it.Title, "err", err)
			continue
		}
		if m != nil {
			matched++
		}
		if err := f.Store.ApplyMetadata(ctx, it.ID, m); err != nil {
			f.Log.Error("save metadata", "err", err)
		}
	}
	if len(items) > 0 {
		f.Log.Info("metadata done", "library", lib.Name, "looked_up", len(items), "matched", matched)
	}
	f.fetchGenres(ctx, lib)
	f.FetchDetails(ctx, lib)
	f.FetchTranslations(ctx, lib)
}

// fetchGenres fills in genres for movies and shows matched before Matinee
// kept them, so the home screen's genre rows include the whole library.
func (f *Fetcher) fetchGenres(ctx context.Context, lib store.Library) {
	items, err := f.Store.NeedsGenres(ctx, lib.ID)
	if err != nil {
		f.Log.Error("list items needing genres", "err", err)
		return
	}
	for _, it := range items {
		if ctx.Err() != nil {
			return
		}
		p, ok := f.provider(it.Kind).(GenreProvider)
		if !ok {
			continue
		}
		genres, err := p.Genres(ctx, it.Kind, *it.TMDBID)
		if err != nil {
			f.Log.Warn("genre lookup", "title", it.Title, "err", err)
			return // likely down; the next scan tries again
		}
		if err := f.Store.SetGenres(ctx, it.ID, genres); err != nil {
			f.Log.Error("save genres", "err", err)
		}
	}
}

type queryState int

const (
	ready      queryState = iota
	noMatch               // can never match, e.g. an episode of an unmatched show
	retryLater            // depends on a lookup that hasn't succeeded yet
)

func (f *Fetcher) query(ctx context.Context, it store.Item) (Query, queryState) {
	q := Query{Kind: it.Kind, Title: it.Title, Path: it.Path, Data: it.PluginData}
	if it.Year != nil {
		q.Year = *it.Year
	}
	switch it.Kind {
	case "artist":
		if it.Title == "Unknown Artist" {
			return q, noMatch
		}
		return q, ready
	case "album":
		if it.Title == "Unknown Album" || it.ParentID == nil {
			return q, noMatch
		}
		artist, err := f.Store.Item(ctx, 0, *it.ParentID)
		if err != nil {
			return q, noMatch
		}
		if artist.Title != "Unknown Artist" {
			q.Artist = artist.Title
		}
		return q, ready
	case "season":
		if it.ParentID == nil || it.IndexNumber == nil {
			return q, noMatch
		}
		return f.showQuery(ctx, q, *it.ParentID, *it.IndexNumber, 0)
	case "episode":
		if it.ParentID == nil || it.IndexNumber == nil {
			return q, noMatch
		}
		season, err := f.Store.Item(ctx, 0, *it.ParentID)
		if err != nil || season.ParentID == nil || season.IndexNumber == nil {
			return q, noMatch
		}
		return f.showQuery(ctx, q, *season.ParentID, *season.IndexNumber, *it.IndexNumber)
	}
	return q, ready
}

// showQuery fills in the show's provider id for a season or episode lookup.
func (f *Fetcher) showQuery(ctx context.Context, q Query, showID int64, season, episode int) (Query, queryState) {
	show, err := f.Store.Item(ctx, 0, showID)
	switch {
	case err != nil:
		return q, noMatch
	case show.MetaAt == 0:
		return q, retryLater
	case show.TMDBID == nil:
		return q, noMatch
	}
	q.ShowID, q.Season, q.Episode = *show.TMDBID, season, episode
	return q, ready
}
