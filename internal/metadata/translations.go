package metadata

import (
	"context"
	"time"

	"github.com/yeixio/matinee/internal/store"
)

// Translator is a provider that also knows titles' text in other languages.
type Translator interface {
	// Translations looks up a movie's or show's title, description and
	// tagline in every language it has them in, keyed by i18n language.
	Translations(ctx context.Context, kind string, id int64) (map[string]store.Translation, error)
	// SeasonTranslation looks up a season's and its episodes' text in one
	// language; episodes are keyed by number.
	SeasonTranslation(ctx context.Context, showID int64, season int, lang string) (store.Translation, map[int]store.Translation, error)
}

func (f *Fetcher) translator() Translator {
	for _, p := range f.Providers {
		if t, ok := p.(Translator); ok {
			return t
		}
	}
	return nil
}

// FetchTranslations fills in a library's titles in other languages: every
// language for movies and shows, which takes one lookup each, and the
// languages people use for seasons and episodes, which take one a season.
func (f *Fetcher) FetchTranslations(ctx context.Context, lib store.Library) {
	tr := f.translator()
	if tr == nil {
		return
	}
	items, err := f.Store.NeedsTranslations(ctx, lib.ID, time.Now().Add(-detailsRefresh).Unix())
	if err != nil {
		f.Log.Error("list items needing translations", "err", err)
		return
	}
	for _, it := range items {
		if ctx.Err() != nil {
			return
		}
		t, err := tr.Translations(ctx, it.Kind, *it.TMDBID)
		if err != nil {
			f.Log.Warn("translations lookup", "title", it.Title, "err", err)
			return // likely down; the next scan tries again
		}
		if err := f.Store.SetTranslations(ctx, it.ID, t); err != nil {
			f.Log.Error("save translations", "err", err)
			return
		}
	}
	langs, err := f.Store.MetadataLanguages(ctx)
	if err != nil {
		f.Log.Error("list languages in use", "err", err)
		return
	}
	for _, lang := range langs {
		seasons, err := f.Store.NeedsSeasonTranslations(ctx, lib.ID, lang)
		if err != nil {
			f.Log.Error("list seasons needing translations", "err", err)
			return
		}
		for _, se := range seasons {
			if ctx.Err() != nil {
				return
			}
			if err := f.seasonTranslation(ctx, tr, se, lang); err != nil {
				f.Log.Warn("season translation lookup", "season", se.ID, "language", lang, "err", err)
				return
			}
		}
	}
}

func (f *Fetcher) seasonTranslation(ctx context.Context, tr Translator, se store.Item, lang string) error {
	show, err := f.Store.Item(ctx, 0, *se.ParentID)
	if err != nil || show.TMDBID == nil {
		return err
	}
	season, byNumber, err := tr.SeasonTranslation(ctx, *show.TMDBID, *se.IndexNumber, lang)
	if err != nil {
		return err
	}
	kids, err := f.Store.Children(ctx, 0, se.ID)
	if err != nil {
		return err
	}
	episodes := map[int64]store.Translation{}
	for _, ep := range kids {
		if ep.IndexNumber == nil {
			continue
		}
		// Episodes without a translation get an empty one, so they aren't
		// looked up again.
		episodes[ep.ID] = byNumber[*ep.IndexNumber]
	}
	return f.Store.SetSeasonTranslations(ctx, se.ID, lang, season, episodes)
}

// TranslateAll fills in translations for every library, such as when someone
// starts using a new language. A call while it runs makes it run once more.
func (f *Fetcher) TranslateAll(ctx context.Context) {
	f.translatePending.Store(true)
	for f.translatePending.Load() && f.translating.TryLock() {
		for f.translatePending.Swap(false) {
			libs, err := f.Store.Libraries(ctx)
			if err != nil {
				f.Log.Error("list libraries", "err", err)
				break
			}
			for _, lib := range libs {
				f.FetchTranslations(ctx, lib)
			}
		}
		f.translating.Unlock()
	}
}
