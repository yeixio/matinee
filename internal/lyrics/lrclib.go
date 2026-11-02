package lyrics

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// LRCLIB looks up songs' words on LRCLIB (lrclib.net), a free, open
// database of lyrics that needs no account or key. The server asks it
// directly; nothing goes through anyone else. Answers, including "not
// there", are kept in Dir so each song is asked about once in a while.
type LRCLIB struct {
	// Base is the API; empty is https://lrclib.net/api.
	Base      string
	Client    *http.Client
	UserAgent string
	Dir       string
}

const (
	// keepFound is how long found words are kept; keepMissing how long
	// before asking again about a song LRCLIB didn't have.
	keepFound   = 90 * 24 * time.Hour
	keepMissing = 7 * 24 * time.Hour
	// closeEnough is how far a search result's length may be from the
	// song's and still be the same recording.
	closeEnough = 3.0
)

type lrclibTrack struct {
	TrackName    string  `json:"trackName"`
	ArtistName   string  `json:"artistName"`
	AlbumName    string  `json:"albumName"`
	Duration     float64 `json:"duration"`
	Instrumental bool    `json:"instrumental"`
	PlainLyrics  string  `json:"plainLyrics"`
	SyncedLyrics string  `json:"syncedLyrics"`
}

type cached struct {
	At     time.Time `json:"at"`
	Lyrics Lyrics    `json:"lyrics"`
}

// Find looks the song up: first exactly (artist, title, album and length),
// then by searching for the artist and title and taking a result about as
// long as the song.
func (c *LRCLIB) Find(ctx context.Context, song Song) (Lyrics, error) {
	if strings.TrimSpace(song.Title) == "" || strings.TrimSpace(song.Artist) == "" {
		return Lyrics{Lines: []Line{}}, nil
	}
	key := c.cacheKey(song)
	if l, ok := c.load(key); ok {
		return l, nil
	}
	l, err := c.lookup(ctx, song)
	if err != nil {
		return Lyrics{}, err
	}
	c.save(key, l)
	return l, nil
}

func (c *LRCLIB) lookup(ctx context.Context, song Song) (Lyrics, error) {
	secs := float64(song.DurationMs) / 1000
	if song.Album != "" && secs > 0 {
		q := url.Values{"artist_name": {song.Artist}, "track_name": {song.Title}, "album_name": {song.Album}, "duration": {fmt.Sprintf("%.0f", secs)}}
		var t lrclibTrack
		switch err := c.get(ctx, "/get", q, &t); {
		case err == nil:
			return fromTrack(t), nil
		case !errors.Is(err, errMissing):
			return Lyrics{}, err
		}
	}
	var found []lrclibTrack
	if err := c.get(ctx, "/search", url.Values{"artist_name": {song.Artist}, "track_name": {song.Title}}, &found); err != nil && !errors.Is(err, errMissing) {
		return Lyrics{}, err
	}
	best := -1
	for i, t := range found {
		if secs > 0 && math.Abs(t.Duration-secs) > closeEnough {
			continue
		}
		if t.SyncedLyrics == "" && t.PlainLyrics == "" && !t.Instrumental {
			continue
		}
		if best < 0 || (t.SyncedLyrics != "" && found[best].SyncedLyrics == "") {
			best = i
		}
	}
	if best < 0 {
		return Lyrics{Lines: []Line{}}, nil
	}
	return fromTrack(found[best]), nil
}

func fromTrack(t lrclibTrack) Lyrics {
	l := Lyrics{Lines: []Line{}}
	switch {
	case strings.TrimSpace(t.SyncedLyrics) != "":
		l = Parse(t.SyncedLyrics)
	case strings.TrimSpace(t.PlainLyrics) != "":
		l = Parse(t.PlainLyrics)
	}
	l.Instrumental = t.Instrumental
	if !l.Empty() || l.Instrumental {
		l.Source = "online"
	}
	return l
}

var errMissing = errors.New("not on LRCLIB")

func (c *LRCLIB) get(ctx context.Context, path string, q url.Values, out any) error {
	base := c.Base
	if base == "" {
		base = "https://lrclib.net/api"
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(base, "/")+path+"?"+q.Encode(), nil)
	if err != nil {
		return err
	}
	if c.UserAgent != "" {
		req.Header.Set("User-Agent", c.UserAgent)
	}
	client := c.Client
	if client == nil {
		client = http.DefaultClient
	}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	switch {
	case res.StatusCode == http.StatusNotFound:
		return errMissing
	case res.StatusCode != http.StatusOK:
		return fmt.Errorf("LRCLIB: %s", res.Status)
	}
	return json.NewDecoder(http.MaxBytesReader(nil, res.Body, 4<<20)).Decode(out)
}

func (c *LRCLIB) cacheKey(song Song) string {
	h := sha1.Sum([]byte(strings.ToLower(strings.Join([]string{song.Artist, song.Title, song.Album, fmt.Sprint(song.DurationMs / 1000)}, "\x00"))))
	return hex.EncodeToString(h[:])
}

func (c *LRCLIB) load(key string) (Lyrics, bool) {
	if c.Dir == "" {
		return Lyrics{}, false
	}
	b, err := os.ReadFile(filepath.Join(c.Dir, key+".json"))
	if err != nil {
		return Lyrics{}, false
	}
	var v cached
	if json.Unmarshal(b, &v) != nil {
		return Lyrics{}, false
	}
	keep := keepFound
	if v.Lyrics.Empty() && !v.Lyrics.Instrumental {
		keep = keepMissing
	}
	if time.Since(v.At) > keep {
		return Lyrics{}, false
	}
	if v.Lyrics.Lines == nil {
		v.Lyrics.Lines = []Line{}
	}
	return v.Lyrics, true
}

func (c *LRCLIB) save(key string, l Lyrics) {
	if c.Dir == "" || os.MkdirAll(c.Dir, 0o755) != nil {
		return
	}
	b, err := json.Marshal(cached{At: time.Now(), Lyrics: l})
	if err != nil {
		return
	}
	tmp := filepath.Join(c.Dir, key+".tmp")
	if os.WriteFile(tmp, b, 0o644) == nil {
		_ = os.Rename(tmp, filepath.Join(c.Dir, key+".json"))
	}
}
