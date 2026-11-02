package api

import (
	"context"
	"net/http"
	"strconv"

	"github.com/yeixio/matinee/internal/lyrics"
	"github.com/yeixio/matinee/internal/store"
)

// Lyrics: a song's words, timed when they can be, so players can show
// each line as it's sung. They come from a .lrc (or .txt) file next to the
// song, or the song's own tags; and, only when an admin turns on online
// lyrics in Settings, from LRCLIB, which the server asks directly.

// settingOnlineLyrics is "true" when an admin turned online lyrics on.
const settingOnlineLyrics = "online_lyrics"

func (s *Server) onlineLyricsOn(ctx context.Context) (bool, error) {
	v, err := s.Store.Setting(ctx, settingOnlineLyrics)
	return v == "true", err
}

func (s *Server) setOnlineLyrics(ctx context.Context, on bool) error {
	return s.Store.SetSetting(ctx, settingOnlineLyrics, strconv.FormatBool(on))
}

// itemLyrics is GET /items/{id}/lyrics. A song without words gets empty
// lines, not an error.
func (s *Server) itemLyrics(w http.ResponseWriter, r *http.Request) {
	it, ok := s.loadItem(w, r)
	if !ok {
		return
	}
	if it.Kind != "track" {
		writeError(w, http.StatusBadRequest, "not a song")
		return
	}
	out := lyrics.Lyrics{Lines: []lyrics.Line{}}
	if s.Lyrics != nil {
		online, err := s.onlineLyricsOn(r.Context())
		if err != nil {
			s.serverError(w, err)
			return
		}
		song := lyrics.Song{Path: it.Path, Artist: it.Artist, Title: it.Title, Album: it.Album}
		if it.DurationMs != nil {
			song.DurationMs = *it.DurationMs
		}
		if song.Artist == "" {
			song.Artist = s.albumArtist(r, it)
		}
		found, err := s.Lyrics.Find(r.Context(), song, online)
		switch {
		case err != nil && r.Context().Err() != nil:
			return
		case err != nil:
			// LRCLIB couldn't be reached; there may be words next time.
			s.Log.Warn("looking up a song's lyrics", "item", it.ID, "err", err)
			w.Header().Set("Cache-Control", "no-store")
			writeJSON(w, http.StatusOK, out)
			return
		}
		out = found
	}
	// A .lrc added next to the song shows up within a few minutes.
	w.Header().Set("Cache-Control", "private, max-age=300")
	writeJSON(w, http.StatusOK, out)
}

// albumArtist is who made the album a song is on, for songs that don't
// name their own artist.
func (s *Server) albumArtist(r *http.Request, it store.Item) string {
	if it.ParentID == nil {
		return ""
	}
	album, err := s.Store.Item(r.Context(), currentUser(r).ID, *it.ParentID)
	if err != nil {
		return ""
	}
	return album.Artist
}
