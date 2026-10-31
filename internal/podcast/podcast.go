package podcast

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/yeixio/matinee/internal/store"
	"github.com/yeixio/matinee/internal/stream"
)

// RefreshEvery is how often each show's feed is checked for new episodes.
const RefreshEvery = time.Hour

// Manager keeps a server's shows in line with their feeds.
type Manager struct {
	Store *store.Store
	HTTP  *http.Client
	Log   *slog.Logger
	// Added runs for each new episode, after the first refresh of a show.
	Added func(ctx context.Context, libraryID, id int64)

	mu      sync.Mutex
	running map[int64]bool // shows being refreshed
}

func (m *Manager) client() *http.Client {
	if m.HTTP != nil {
		return m.HTTP
	}
	// The same guard as for playing episodes: feeds come from the internet.
	return &http.Client{Transport: stream.Remote.Transport, Timeout: 60 * time.Second}
}

var (
	// ErrBadURL means a feed address isn't a web address.
	ErrBadURL = errors.New("enter the feed's web address, starting with https://")
	// ErrNoEpisodes means a feed has no episodes with sound to play.
	ErrNoEpisodes = errors.New("that feed has no episodes to listen to")
)

// NormalizeURL tidies a feed address someone typed or pasted. Addresses
// that podcast apps register for feeds (feed:// and podcast://) work too.
func NormalizeURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	for _, p := range []string{"feed://", "podcast://", "pcast://", "itpc://"} {
		if rest, ok := strings.CutPrefix(strings.ToLower(raw), p); ok {
			raw = "https://" + raw[len(raw)-len(rest):]
		}
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", ErrBadURL
	}
	u.Fragment = ""
	return u.String(), nil
}

// Subscribe adds the show with this feed to a library, with its episodes,
// or finds it if the library has it already.
func (m *Manager) Subscribe(ctx context.Context, lib store.Library, feedURL string) (int64, error) {
	feedURL, err := NormalizeURL(feedURL)
	if err != nil {
		return 0, err
	}
	if id, err := m.Store.PodcastByFeed(ctx, lib.ID, feedURL); err == nil {
		return id, nil
	} else if !errors.Is(err, store.ErrNotFound) {
		return 0, err
	}
	got, err := Fetch(ctx, m.client(), feedURL, "", "")
	if err != nil {
		return 0, err
	}
	if got.Feed == nil {
		return 0, ErrNotFeed
	}
	if len(got.Feed.Episodes) == 0 {
		return 0, ErrNoEpisodes
	}
	// The feed may have moved for good; that's the address to keep.
	if got.URL != feedURL {
		if id, err := m.Store.PodcastByFeed(ctx, lib.ID, got.URL); err == nil {
			return id, nil
		}
	}
	id, err := m.Store.CreatePodcast(ctx, lib.ID, got.URL, info(got.Feed))
	if err != nil {
		return 0, err
	}
	if err := m.putEpisodes(ctx, lib.ID, id, got.Feed, false); err != nil {
		return id, err
	}
	return id, m.Store.PodcastRefreshed(ctx, id, got.URL, got.ETag, got.LastModified, "")
}

func info(f *Feed) store.PodcastInfo {
	return store.PodcastInfo{Title: f.Title, Description: f.Description, Author: f.Author, Link: f.Link,
		Image: f.Image, Language: f.Language, Categories: f.Categories}
}

func (m *Manager) putEpisodes(ctx context.Context, libraryID, podcastID int64, f *Feed, announce bool) error {
	for _, e := range f.Episodes {
		id, added, err := m.Store.PutEpisode(ctx, libraryID, podcastID, store.PodcastEpisode{
			GUID: e.GUID, Title: e.Title, Description: e.Description, URL: e.URL, MimeType: e.Type, Size: e.Length,
			Published: e.Published, DurationMs: e.DurationMs, Season: e.Season, Number: e.Number, Image: e.Image,
			Link: e.Link, Kind: e.Kind,
		})
		if err != nil {
			return err
		}
		if added && announce && m.Added != nil {
			m.Added(ctx, libraryID, id)
		}
	}
	return nil
}

// Refresh checks one show's feed for new and changed episodes.
func (m *Manager) Refresh(ctx context.Context, f store.PodcastFeed) (added int, err error) {
	m.mu.Lock()
	if m.running == nil {
		m.running = map[int64]bool{}
	}
	if m.running[f.ID] {
		m.mu.Unlock()
		return 0, nil
	}
	m.running[f.ID] = true
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		delete(m.running, f.ID)
		m.mu.Unlock()
	}()

	got, err := Fetch(ctx, m.client(), f.URL, f.ETag, f.LastModified)
	if err != nil {
		if ctx.Err() == nil {
			m.Store.PodcastRefreshed(ctx, f.ID, f.URL, f.ETag, f.LastModified, err.Error())
		}
		return 0, err
	}
	if got.Feed != nil {
		if err := m.Store.UpdatePodcast(ctx, f.ID, info(got.Feed)); err != nil {
			return 0, err
		}
		before, err := m.Store.Children(ctx, 0, f.ID)
		if err != nil {
			return 0, err
		}
		if err := m.putEpisodes(ctx, f.LibraryID, f.ID, got.Feed, true); err != nil {
			return 0, err
		}
		after, err := m.Store.Children(ctx, 0, f.ID)
		if err != nil {
			return 0, err
		}
		added = len(after) - len(before)
	}
	return added, m.Store.PodcastRefreshed(ctx, f.ID, got.URL, got.ETag, got.LastModified, "")
}

// RefreshLibrary checks every show in a library, a few at a time, and
// says how many new episodes turned up and how many feeds failed.
func (m *Manager) RefreshLibrary(ctx context.Context, lib store.Library) (added, failed int, err error) {
	return m.refreshLibrary(ctx, lib, 0)
}

// RefreshStale is RefreshLibrary for the shows not checked for
// RefreshEvery, for the server's regular scans of every library.
func (m *Manager) RefreshStale(ctx context.Context, lib store.Library) (added, failed int, err error) {
	return m.refreshLibrary(ctx, lib, time.Now().Add(-RefreshEvery).Unix())
}

func (m *Manager) refreshLibrary(ctx context.Context, lib store.Library, before int64) (added, failed int, err error) {
	feeds, err := m.Store.PodcastFeeds(ctx, lib.ID)
	if err != nil {
		return 0, 0, err
	}
	if before > 0 {
		feeds = slices.DeleteFunc(feeds, func(f store.PodcastFeed) bool { return f.RefreshedAt >= before })
	}
	return m.refreshAll(ctx, feeds)
}

func (m *Manager) refreshAll(ctx context.Context, feeds []store.PodcastFeed) (added, failed int, err error) {
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	for _, f := range feeds {
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer func() { <-sem; wg.Done() }()
			n, err := m.Refresh(ctx, f)
			mu.Lock()
			defer mu.Unlock()
			added += n
			if err != nil {
				failed++
				m.Log.Warn("podcast refresh failed", "show", f.Title, "err", err)
			}
		}()
	}
	wg.Wait()
	return added, failed, ctx.Err()
}

// Run refreshes shows whose feeds weren't checked for RefreshEvery, until
// ctx ends.
func (m *Manager) Run(ctx context.Context) {
	t := time.NewTicker(5 * time.Minute)
	defer t.Stop()
	for {
		feeds, err := m.Store.StalePodcasts(ctx, time.Now().Add(-RefreshEvery).Unix())
		if err != nil && ctx.Err() == nil {
			m.Log.Error("podcasts to refresh", "err", err)
		}
		if len(feeds) > 0 {
			added, failed, _ := m.refreshAll(ctx, feeds)
			m.Log.Info("podcasts refreshed", "shows", len(feeds), "new episodes", added, "failed", failed)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
