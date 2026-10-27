package store

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"

	"github.com/yeixio/matinee/i18n"
)

// Titles, descriptions and taglines in other languages come from the online
// match (TMDB) and are kept in item_translations, one row per item and
// language. A request's language (WithLanguage) picks which ones items are
// shown with; without a translation they keep the original text.

type languageKey struct{}

// WithLanguage makes items read with ctx come in a language, one of the
// i18n languages. "" or English shows them as they are.
func WithLanguage(ctx context.Context, language string) context.Context {
	return context.WithValue(ctx, languageKey{}, language)
}

// LanguageFrom returns the language WithLanguage set, or "".
func LanguageFrom(ctx context.Context) string {
	l, _ := ctx.Value(languageKey{}).(string)
	return l
}

// Translation is an item's text in one language. Empty fields have no
// translation.
type Translation struct {
	Title    string
	Overview string
	Tagline  string
}

func translating(ctx context.Context) string {
	if l := LanguageFrom(ctx); l != i18n.Source {
		return l
	}
	return ""
}

// localize shows items in the context's language where they have a
// translation, an episode's show title too.
func (s *Store) localize(ctx context.Context, items []Item) error {
	lang := translating(ctx)
	if lang == "" || len(items) == 0 {
		return nil
	}
	var ids []int64
	for _, it := range items {
		switch it.Kind {
		case "movie", "show", "season", "episode":
			ids = append(ids, it.ID)
			if it.showID != 0 {
				ids = append(ids, it.showID)
			}
		}
	}
	tr, err := s.translations(ctx, lang, ids)
	if err != nil || len(tr) == 0 {
		return err
	}
	for i := range items {
		it := &items[i]
		if t, ok := tr[it.ID]; ok {
			if t.Title != "" {
				it.Title = t.Title
			}
			if t.Overview != "" {
				it.Overview = t.Overview
			}
		}
		if t, ok := tr[it.showID]; ok && it.showID != 0 && t.Title != "" {
			it.ShowTitle = t.Title
		}
	}
	return nil
}

func (s *Store) translations(ctx context.Context, lang string, ids []int64) (map[int64]Translation, error) {
	out := map[int64]Translation{}
	for len(ids) > 0 {
		chunk := ids[:min(len(ids), 500)]
		ids = ids[len(chunk):]
		args := []any{lang}
		for _, id := range chunk {
			args = append(args, id)
		}
		rows, err := s.db.QueryContext(ctx, `SELECT item_id, title, overview, tagline FROM item_translations
			WHERE language = ? AND item_id IN (?`+strings.Repeat(", ?", len(chunk)-1)+`)`, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id int64
			var t Translation
			if err := rows.Scan(&id, &t.Title, &t.Overview, &t.Tagline); err != nil {
				rows.Close()
				return nil, err
			}
			out[id] = t
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// TranslatedTagline returns an item's tagline in the context's language, or
// "" when it has none.
func (s *Store) TranslatedTagline(ctx context.Context, id int64) (string, error) {
	lang := translating(ctx)
	if lang == "" {
		return "", nil
	}
	var tagline string
	err := s.db.QueryRowContext(ctx, "SELECT tagline FROM item_translations WHERE item_id = ? AND language = ?", id, lang).Scan(&tagline)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return tagline, err
}

// NeedsTranslations lists a library's matched movies and shows whose
// translations were never fetched or were fetched before olderThan (Unix
// seconds).
func (s *Store) NeedsTranslations(ctx context.Context, libraryID, olderThan int64) ([]Item, error) {
	return s.queryItems(ctx,
		"i.library_id = ? AND i.kind IN ('movie', 'show') AND i.tmdb_id IS NOT NULL AND COALESCE(i.translations_at, 0) < ?",
		"i.id", 0, libraryID, olderThan)
}

// SetTranslations replaces a movie's or show's translations, by language.
func (s *Store) SetTranslations(ctx context.Context, id int64, tr map[string]Translation) error {
	t, err := s.db.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer t.Rollback()
	if _, err := t.ExecContext(ctx, "DELETE FROM item_translations WHERE item_id = ?", id); err != nil {
		return err
	}
	for lang, tt := range tr {
		if err := putTranslation(ctx, t, id, lang, tt); err != nil {
			return err
		}
	}
	if _, err := t.ExecContext(ctx, "UPDATE items SET translations_at = ? WHERE id = ?", now(), id); err != nil {
		return err
	}
	return t.Commit()
}

func putTranslation(ctx context.Context, t *tx, id int64, lang string, tt Translation) error {
	_, err := t.ExecContext(ctx, `INSERT INTO item_translations (item_id, language, title, overview, tagline, search) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (item_id, language) DO UPDATE SET
			title = excluded.title, overview = excluded.overview, tagline = excluded.tagline, search = excluded.search`,
		id, lang, tt.Title, tt.Overview, tt.Tagline, strings.ToLower(tt.Title))
	return err
}

// NeedsSeasonTranslations lists a library's seasons of matched shows that,
// or one of whose episodes, have no row in a language yet. Seasons are looked
// up one language at a time, with their episodes, so only in the languages
// people use.
func (s *Store) NeedsSeasonTranslations(ctx context.Context, libraryID int64, lang string) ([]Item, error) {
	return s.queryItems(ctx, `i.library_id = ? AND i.kind = 'season' AND i.index_number IS NOT NULL
		AND EXISTS (SELECT 1 FROM items sh WHERE sh.id = i.parent_id AND sh.tmdb_id IS NOT NULL)
		AND (NOT EXISTS (SELECT 1 FROM item_translations t WHERE t.item_id = i.id AND t.language = ?)
			OR EXISTS (SELECT 1 FROM items e WHERE e.parent_id = i.id AND e.index_number IS NOT NULL
				AND NOT EXISTS (SELECT 1 FROM item_translations t WHERE t.item_id = e.id AND t.language = ?)))`,
		"i.parent_id, i.index_number", 0, libraryID, lang, lang)
}

// SetSeasonTranslations keeps a season's and its episodes' text in one
// language. Each gets a row even without a translation, so it isn't looked
// up again.
func (s *Store) SetSeasonTranslations(ctx context.Context, seasonID int64, lang string, season Translation, episodes map[int64]Translation) error {
	t, err := s.db.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer t.Rollback()
	if err := putTranslation(ctx, t, seasonID, lang, season); err != nil {
		return err
	}
	for id, tt := range episodes {
		if err := putTranslation(ctx, t, id, lang, tt); err != nil {
			return err
		}
	}
	return t.Commit()
}

// Settings that say which languages people use.
const (
	// userLanguagePrefix keeps a profile's own language: "language:user:<id>".
	userLanguagePrefix = "language:user:"
	// settingLanguagesSeen lists languages devices asked for without one
	// picked, comma-separated.
	settingLanguagesSeen = "languages:seen"
)

// UserLanguage returns the language a profile picked, or "" to follow the device.
func (s *Store) UserLanguage(ctx context.Context, userID int64) (string, error) {
	return s.Setting(ctx, userLanguagePrefix+strconv.FormatInt(userID, 10))
}

// SetUserLanguage keeps the language a profile picked; "" follows the device.
func (s *Store) SetUserLanguage(ctx context.Context, userID int64, lang string) error {
	return s.SetSetting(ctx, userLanguagePrefix+strconv.FormatInt(userID, 10), lang)
}

// NoteLanguageSeen records that a device asked for a language, and reports
// whether it was new.
func (s *Store) NoteLanguageSeen(ctx context.Context, lang string) (bool, error) {
	seen, err := s.Setting(ctx, settingLanguagesSeen)
	if err != nil {
		return false, err
	}
	list := strings.Split(seen, ",")
	for _, l := range list {
		if l == lang {
			return false, nil
		}
	}
	return true, s.SetSetting(ctx, settingLanguagesSeen, strings.Trim(seen+","+lang, ","))
}

// MetadataLanguages lists the languages besides English that people use:
// picked by a profile or asked for by a device.
func (s *Store) MetadataLanguages(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT value FROM settings WHERE key LIKE ? OR key = ?", userLanguagePrefix+"%", settingLanguagesSeen)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	set := map[string]bool{}
	var out []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		for _, l := range strings.Split(v, ",") {
			if l != "" && l != i18n.Source && i18n.Has(l) && !set[l] {
				set[l] = true
				out = append(out, l)
			}
		}
	}
	return out, rows.Err()
}
