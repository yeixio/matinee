package api

import (
	"net/http"

	"github.com/yeixio/matinee/internal/stream"
)

// versionView is one file of a title with several (see store.GroupVersions):
// what tells it apart, for the version picker on the title page and in the
// player. Its id plays it like any item's.
type versionView struct {
	ID         int64    `json:"id"`
	Edition    string   `json:"edition,omitempty"`
	Width      *int     `json:"width,omitempty"`
	Height     *int     `json:"height,omitempty"`
	VideoCodec string   `json:"videoCodec,omitempty"`
	AudioCodec string   `json:"audioCodec,omitempty"`
	Container  string   `json:"container,omitempty"`
	MimeType   string   `json:"mimeType,omitempty"`
	MediaTags  []string `json:"mediaTags,omitempty"`
	DurationMs *int64   `json:"durationMs,omitempty"`
	Size       int64    `json:"size,omitempty"`
}

// addVersions lists the versions of a movie or episode with more than one.
func (s *Server) addVersions(r *http.Request, v *itemView) error {
	if !v.Playable || v.Kind != "movie" && v.Kind != "episode" {
		return nil
	}
	vs, err := s.Store.Versions(r.Context(), currentUser(r).ID, v.ID)
	if err != nil || len(vs) < 2 {
		return err
	}
	for _, it := range vs {
		mime := stream.MimeType(it.Path)
		if len(it.Parts) > 1 {
			mime = "" // played as one through the server
		}
		v.Versions = append(v.Versions, versionView{
			ID: it.ID, Edition: it.Edition, Width: it.Width, Height: it.Height,
			VideoCodec: it.VideoCodec, AudioCodec: it.AudioCodec, Container: it.Container,
			MimeType: mime, MediaTags: it.MediaTags, DurationMs: it.DurationMs, Size: it.Size,
		})
	}
	return nil
}
