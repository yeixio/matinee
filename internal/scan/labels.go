package scan

import (
	"context"
	"strings"

	"github.com/yeixio/matinee/internal/store"
)

// albumLabel is the record label a music file's tags name. Taggers write
// LABEL or ORGANIZATION in Vorbis comments and APE tags, and ID3's TPUB,
// which FFmpeg reads as publisher. A tag naming no label, as MusicBrainz's
// "[no label]", counts as none.
func albumLabel(tags map[string]string) string {
	for _, k := range []string{"label", "publisher", "organization", "recordlabel", "record label"} {
		v := strings.TrimSpace(tags[k])
		if v == "" {
			continue
		}
		// Several labels are joined with a semicolon or a slash; the first
		// is the one the release is filed under.
		if i := strings.IndexAny(v, ";\x00"); i > 0 {
			v = strings.TrimSpace(v[:i])
		}
		switch strings.ToLower(strings.Trim(v, "[]() ")) {
		case "", "no label", "not on label", "self-released", "none", "unknown":
			return ""
		}
		return v
	}
	return ""
}

// readAlbumLabels reads, once, one file of each album scanned before
// Matinee kept record labels, since scans skip unchanged files.
func (s *Scanner) readAlbumLabels(ctx context.Context, lib store.Library) error {
	albums, err := s.Store.AlbumsToReadLabel(ctx, lib.ID)
	if err != nil {
		return err
	}
	for _, a := range albums {
		pr, err := s.Prober.Probe(ctx, a.Path)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			// Leave it unread; a file that can't be read now may be later.
			continue
		}
		if err := s.Store.SetAlbumLabel(ctx, a.ID, albumLabel(pr.Tags)); err != nil {
			return err
		}
	}
	return nil
}
