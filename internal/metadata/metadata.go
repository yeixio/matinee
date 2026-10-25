// Package metadata looks up descriptions and artwork from online providers.
// Providers are an extension point: TMDB is built in, and metadata plugins
// will implement the same interface.
package metadata

import (
	"context"
	"log/slog"

	"github.com/yeixio/matinee/internal/store"
)

// Query describes the item to identify.
type Query struct {
	Kind    string // movie, show or episode
	Title   string
	Year    int
	ShowID  int64 // provider id of the episode's show
	Season  int
	Episode int
}

// Provider identifies items with an online database. Lookup returns nil, nil
// when nothing matches.
type Provider interface {
	Name() string
	Lookup(ctx context.Context, q Query) (*store.Metadata, error)
}

// Fetcher fills in metadata for newly scanned items.
type Fetcher struct {
	Store    *store.Store
	Provider Provider
	Log      *slog.Logger
}

// FetchLibrary looks up every item in the library that hasn't been looked up yet.
func (f *Fetcher) FetchLibrary(ctx context.Context, lib store.Library) {
	if f.Provider == nil {
		return
	}
	items, err := f.Store.NeedsMetadata(ctx, lib.ID)
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
		m, err := f.Provider.Lookup(ctx, q)
		if err != nil {
			// Leave it for the next scan; the provider may be down.
			f.Log.Warn("metadata lookup", "provider", f.Provider.Name(), "title", it.Title, "err", err)
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
}

type queryState int

const (
	ready      queryState = iota
	noMatch               // can never match, e.g. an episode of an unmatched show
	retryLater            // depends on a lookup that hasn't succeeded yet
)

func (f *Fetcher) query(ctx context.Context, it store.Item) (Query, queryState) {
	q := Query{Kind: it.Kind, Title: it.Title}
	if it.Year != nil {
		q.Year = *it.Year
	}
	if it.Kind != "episode" {
		return q, ready
	}
	if it.ParentID == nil || it.IndexNumber == nil {
		return q, noMatch
	}
	season, err := f.Store.Item(ctx, 0, *it.ParentID)
	if err != nil || season.ParentID == nil || season.IndexNumber == nil {
		return q, noMatch
	}
	show, err := f.Store.Item(ctx, 0, *season.ParentID)
	switch {
	case err != nil:
		return q, noMatch
	case show.MetaAt == 0:
		return q, retryLater
	case show.TMDBID == nil:
		return q, noMatch
	}
	q.ShowID, q.Season, q.Episode = *show.TMDBID, *season.IndexNumber, *it.IndexNumber
	return q, ready
}
