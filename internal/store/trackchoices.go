package store

import (
	"context"
	"database/sql"
	"errors"
)

// Track choices: the sound and subtitle tracks a person picked for a
// title, so it plays the same way next time. One row per person and item.
// An episode's pick is also kept on its show, and a version's on its
// title's main file, so the next episode or the other version starts in
// the same language (see trackpick.go in api).

// TrackChoice is what a person picked. Audio is -1 and Subtitle "" for
// what they didn't pick; Subtitle "off" is subtitles turned off.
type TrackChoice struct {
	Audio            int    `json:"audio"`
	AudioLanguage    string `json:"audioLanguage,omitempty"`
	AudioKind        string `json:"audioKind,omitempty"` // commentary, descriptive
	Subtitle         string `json:"subtitle,omitempty"`
	SubtitleLanguage string `json:"subtitleLanguage,omitempty"`
	SubtitleKind     string `json:"subtitleKind,omitempty"` // forced, sdh
	UpdatedAt        int64  `json:"updatedAt"`
}

// NoTrackChoice is a choice with nothing picked.
var NoTrackChoice = TrackChoice{Audio: -1}

// TrackChoiceFor returns a person's picks for an item, or ErrNotFound.
func (s *Store) TrackChoiceFor(ctx context.Context, userID, itemID int64) (TrackChoice, error) {
	c := NoTrackChoice
	err := s.db.QueryRowContext(ctx, `SELECT audio, audio_language, audio_kind, subtitle, subtitle_language, subtitle_kind, updated_at
		FROM track_choices WHERE user_id = ? AND item_id = ?`, userID, itemID).
		Scan(&c.Audio, &c.AudioLanguage, &c.AudioKind, &c.Subtitle, &c.SubtitleLanguage, &c.SubtitleKind, &c.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return NoTrackChoice, ErrNotFound
	}
	return c, err
}

// SetTrackChoice keeps a person's picks for an item. setAudio and
// setSubtitle say which of c's halves to change; the other stays.
func (s *Store) SetTrackChoice(ctx context.Context, userID, itemID int64, c TrackChoice, setAudio, setSubtitle bool) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO track_choices
		(user_id, item_id, audio, audio_language, audio_kind, subtitle, subtitle_language, subtitle_kind, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (user_id, item_id) DO UPDATE SET
			audio = CASE WHEN ? = 1 THEN excluded.audio ELSE track_choices.audio END,
			audio_language = CASE WHEN ? = 1 THEN excluded.audio_language ELSE track_choices.audio_language END,
			audio_kind = CASE WHEN ? = 1 THEN excluded.audio_kind ELSE track_choices.audio_kind END,
			subtitle = CASE WHEN ? = 1 THEN excluded.subtitle ELSE track_choices.subtitle END,
			subtitle_language = CASE WHEN ? = 1 THEN excluded.subtitle_language ELSE track_choices.subtitle_language END,
			subtitle_kind = CASE WHEN ? = 1 THEN excluded.subtitle_kind ELSE track_choices.subtitle_kind END,
			updated_at = excluded.updated_at`,
		userID, itemID, c.Audio, c.AudioLanguage, c.AudioKind, c.Subtitle, c.SubtitleLanguage, c.SubtitleKind, now(),
		b2i(setAudio), b2i(setAudio), b2i(setAudio), b2i(setSubtitle), b2i(setSubtitle), b2i(setSubtitle))
	return err
}

// ForgetTrackChoices forgets a person's picks for these items.
func (s *Store) ForgetTrackChoices(ctx context.Context, userID int64, itemIDs ...int64) error {
	for _, id := range itemIDs {
		if _, err := s.db.ExecContext(ctx, "DELETE FROM track_choices WHERE user_id = ? AND item_id = ?", userID, id); err != nil {
			return err
		}
	}
	return nil
}
