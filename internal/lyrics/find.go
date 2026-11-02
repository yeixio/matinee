package lyrics

import (
	"context"
	"errors"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/yeixio/matinee/internal/probe"
)

// Song is what the finder needs to know about a song.
type Song struct {
	Path       string
	Artist     string
	Title      string
	Album      string
	DurationMs int64
}

// Finder finds songs' words.
type Finder struct {
	Prober probe.Prober
	// Online looks songs up on LRCLIB; nil never does.
	Online *LRCLIB
}

// maxFile is the biggest lyrics file worth reading.
const maxFile = 512 << 10

// Find returns the song's words: timed ones from next to the song or its
// tags first, then (with online) from LRCLIB, then untimed ones. It returns
// empty Lyrics when there are none anywhere.
func (f *Finder) Find(ctx context.Context, song Song, online bool) (Lyrics, error) {
	local := sidecar(song.Path)
	var tags map[string]string
	if !local.Synced {
		// The tags say who and what for LRCLIB too, more exactly than the
		// library's names, which may have been tidied.
		if res, err := f.Prober.Probe(ctx, song.Path); err == nil {
			tags = res.Tags
			if res.DurationMs > 0 {
				song.DurationMs = res.DurationMs
			}
			if e := Embedded(res.Tags); !e.Empty() && (local.Empty() || e.Synced) {
				local = e
			}
		} else if ctx.Err() != nil {
			return Lyrics{}, ctx.Err()
		}
	}
	if local.Synced || !online || f.Online == nil {
		return local, nil
	}
	if v := tags["artist"]; v != "" {
		song.Artist = v
	} else if v := tags["album_artist"]; v != "" && song.Artist == "" {
		song.Artist = v
	}
	if v := tags["title"]; v != "" {
		song.Title = v
	}
	if v := tags["album"]; v != "" {
		song.Album = v
	}
	found, err := f.Online.Find(ctx, song)
	switch {
	case err != nil && local.Empty():
		return Lyrics{}, err
	case err != nil:
		return local, nil
	case found.Synced || local.Empty():
		return found, nil
	}
	return local, nil
}

// sidecar reads song.lrc (or song.txt) next to the song.
func sidecar(path string) Lyrics {
	base := strings.TrimSuffix(path, filepath.Ext(path))
	for _, ext := range []string{".lrc", ".LRC", ".Lrc", ".txt", ".TXT"} {
		text, err := readSmall(base + ext)
		if err != nil {
			continue
		}
		l := Parse(text)
		if l.Empty() {
			continue
		}
		l.Source = "file"
		return l
	}
	return Lyrics{Lines: []Line{}}
}

func readSmall(path string) (string, error) {
	fh, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer fh.Close()
	if st, err := fh.Stat(); err != nil || !st.Mode().IsRegular() {
		return "", errors.New("not a file")
	}
	b, err := io.ReadAll(io.LimitReader(fh, maxFile))
	return string(b), err
}

// Embedded reads the words in a song's tags (keys lower-cased, as probe
// gives them): ID3's USLT shows up as lyrics-eng and the like, MP4's as
// lyrics, Vorbis comments as lyrics or unsyncedlyrics. Many taggers put
// timed LRC text in them; timed words win over untimed ones.
func Embedded(tags map[string]string) Lyrics {
	best := Lyrics{Lines: []Line{}}
	// In order, so the same tags always give the same words.
	for _, k := range slices.Sorted(maps.Keys(tags)) {
		v := tags[k]
		if !isLyricsTag(k) || strings.TrimSpace(v) == "" {
			continue
		}
		l := Parse(v)
		if l.Empty() {
			continue
		}
		if best.Empty() || (l.Synced && !best.Synced) || (l.Synced == best.Synced && k == "syncedlyrics") {
			best = l
		}
	}
	if !best.Empty() {
		best.Source = "embedded"
	}
	return best
}

func isLyricsTag(k string) bool {
	switch k {
	case "lyrics", "syncedlyrics", "synced lyrics", "unsyncedlyrics", "unsynced lyrics", "uslt", "©lyr":
		return true
	}
	return strings.HasPrefix(k, "lyrics-") || strings.HasPrefix(k, "lyrics_")
}
