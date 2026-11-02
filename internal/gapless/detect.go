// Package gapless finds albums whose songs run straight into each other,
// such as DJ mixes and live albums, so players leave no gap between them.
//
// Files sometimes say so: iTunes' gapless flag, a MusicBrainz release type
// of DJ-mix or live, a genre or title like "DJ Mix" or "Mixed by". When
// they don't, the detector listens to where songs meet: on an album meant
// to play without gaps, the end of one song and the start of the next are
// still loud, where songs on other albums fade out or stop, and start from
// quiet. Getting it wrong costs little either way: songs that already end
// in silence keep it when played without gaps, and anyone can turn it on
// or off for an album.
package gapless

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/yeixio/matinee/internal/probe"
	"github.com/yeixio/matinee/internal/store"
)

// Version goes up when the detector changes enough that albums it listened
// to should be listened to again.
const Version = 1

var (
	// How much of a song's end and the next one's start to listen to.
	edge = 400 * time.Millisecond
	// Louder than this on average counts as still playing, in dBFS. Songs
	// that end or start from quiet sit far below it.
	loud = -32.0
	// At most this many places where songs meet are listened to.
	maxJoins = 8
	// Of those, at least this share must run on for the album to count.
	share       = 0.66
	fileTimeout = time.Minute
	retryFailed = time.Hour
	batch       = 200
)

// Detector looks at albums and stores what it finds (see
// store.SetGaplessFound).
type Detector struct {
	Store  *store.Store
	FFmpeg string
	Prober probe.Prober
	Log    *slog.Logger
	// LowPriority is called with each FFmpeg's process id.
	LowPriority func(pid int)

	mu         sync.Mutex
	retryAfter map[int64]time.Time
}

// Pass looks at every album it hasn't looked at as its files are now.
// Before each one it calls next with the album, which returns false to
// stop the pass (paused, or shutting down). It returns how many albums it
// looked at.
func (d *Detector) Pass(ctx context.Context, next func(store.GaplessAlbum) bool) (int, error) {
	n := 0
	var after int64
	for {
		albums, err := d.Store.GaplessAlbums(ctx, after, batch)
		if err != nil || len(albums) == 0 {
			return n, err
		}
		for _, a := range albums {
			after = a.ID
			key := store.GaplessKey(Version, a.Tracks)
			if a.Key == key || d.retryLater(a.ID) {
				continue
			}
			if !next(a) || ctx.Err() != nil {
				return n, ctx.Err()
			}
			found, err := d.Album(ctx, a)
			if err != nil {
				if ctx.Err() != nil {
					return n, ctx.Err()
				}
				d.Log.Debug("gapless: couldn't listen to album", "album", a.ID, "err", err)
				d.later(a.ID)
				continue
			}
			if err := d.Store.SetGaplessFound(ctx, a.ID, found, key); err != nil {
				return n, err
			}
			if found {
				d.Log.Info("album plays without gaps", "album", a.ID, "title", a.Title)
			}
			n++
		}
	}
}

// Album says whether an album's songs run into each other.
func (d *Detector) Album(ctx context.Context, a store.GaplessAlbum) (bool, error) {
	if len(a.Tracks) < 2 {
		return false, nil
	}
	pr, err := d.Prober.Probe(ctx, a.Tracks[0].Path)
	if err != nil {
		return false, err
	}
	if Tagged(pr.Tags, a.Title) {
		return true, nil
	}
	var joins []bool
	for i := 0; i+1 < len(a.Tracks) && len(joins) < maxJoins; i++ {
		from, to := a.Tracks[i], a.Tracks[i+1]
		// The end of one disc and the start of the next aren't a join.
		if from.Disc != to.Disc {
			continue
		}
		end, err := d.level(ctx, from.Path, true)
		if err != nil {
			return false, err
		}
		start := end
		if end > loud {
			if start, err = d.level(ctx, to.Path, false); err != nil {
				return false, err
			}
		}
		joins = append(joins, end > loud && start > loud)
	}
	return Joined(joins), nil
}

// Joined says whether enough of the places songs meet run on.
func Joined(joins []bool) bool {
	if len(joins) == 0 {
		return false
	}
	n := 0
	for _, j := range joins {
		if j {
			n++
		}
	}
	return float64(n) >= share*float64(len(joins))
}

var (
	// Titles and genres of continuous mixes.
	mixRe = regexp.MustCompile(`(?i)\b(dj[ -]?mix(es|ed)?|continuous(ly)? mix(ed)?|mixed by|non[- ]?stop (mix|dj)|mega[- ]?mix|mix[- ]?tape|in the mix)\b`)
	// MusicBrainz release types (Picard writes them as RELEASETYPE, or
	// "MusicBrainz Album Type").
	typeRe = regexp.MustCompile(`(?i)\b(dj-mix|live)\b`)
)

// Tagged says whether a song's tags, or its album's title, say the album
// plays without gaps.
func Tagged(tags map[string]string, album string) bool {
	for k, v := range tags {
		k = strings.ToLower(k)
		v = strings.TrimSpace(v)
		switch k {
		case "gapless_playback", "itunesgapless", "itunpgap", "pgap", "gapless":
			if v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes") {
				return true
			}
		case "releasetype", "musicbrainz album type", "musicbrainz_albumtype", "release type":
			if typeRe.MatchString(v) {
				return true
			}
		case "genre":
			if mixRe.MatchString(v) {
				return true
			}
		}
	}
	return mixRe.MatchString(album) || mixRe.MatchString(tags["album"])
}

var meanRe = regexp.MustCompile(`mean_volume:\s*(-?[\d.]+|-inf) dB`)

// level is how loud a song is at its end, or its start, on average.
func (d *Detector) level(ctx context.Context, path string, end bool) (float64, error) {
	ctx, cancel := context.WithTimeout(ctx, fileTimeout)
	defer cancel()
	secs := strconv.FormatFloat(edge.Seconds(), 'f', 3, 64)
	args := []string{"-hide_banner", "-nostdin", "-nostats"}
	if end {
		args = append(args, "-sseof", "-"+secs, "-i", path)
	} else {
		args = append(args, "-i", path, "-t", secs)
	}
	args = append(args, "-map", "0:a:0", "-af", "volumedetect", "-f", "null", "-")
	cmd := exec.CommandContext(ctx, d.FFmpeg, args...)
	var out strings.Builder
	cmd.Stderr = &out
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	if d.LowPriority != nil {
		d.LowPriority(cmd.Process.Pid)
	}
	if err := cmd.Wait(); err != nil {
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		return 0, fmt.Errorf("ffmpeg %s: %w", path, err)
	}
	return parseMean(out.String())
}

func parseMean(out string) (float64, error) {
	m := meanRe.FindStringSubmatch(out)
	if m == nil {
		return 0, errors.New("no level in ffmpeg's output")
	}
	if m[1] == "-inf" {
		return -200, nil
	}
	return strconv.ParseFloat(m[1], 64)
}

func (d *Detector) retryLater(id int64) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	t, ok := d.retryAfter[id]
	if ok && time.Now().After(t) {
		delete(d.retryAfter, id)
		return false
	}
	return ok
}

func (d *Detector) later(id int64) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.retryAfter == nil {
		d.retryAfter = map[int64]time.Time{}
	}
	d.retryAfter[id] = time.Now().Add(retryFailed)
}
