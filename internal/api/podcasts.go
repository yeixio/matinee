package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/yeixio/matinee/internal/podcast"
	"github.com/yeixio/matinee/internal/store"
)

// Podcasts (see internal/podcast): a podcast library holds shows followed
// by their feeds. Anyone signed in (but not on a kids profile) can add a
// show to one, which also follows it for them; admins remove shows. Each
// person follows the shows they want, and Home's New episodes row has
// what's new in those.
//
// Shows are found by pasting a feed's address, by importing an OPML file
// from another podcast app, or in Podcast Index, once an admin enters a
// free Podcast Index key under Settings → Integrations.

const settingPodcastIndex = "podcastindex"

// rowNewEpisodes is Home's row of new episodes of the shows someone
// follows, from the last newEpisodeDays.
const (
	rowNewEpisodes = "newEpisodes"
	newEpisodeDays = 14
)

// podcastFinishedMs is how close to an episode's end counts as finishing
// it: most end on a minute of music or a closing ad.
const podcastFinishedMs = 60_000

func (s *Server) podcastIndex(ctx context.Context) (*podcast.Index, error) {
	var k podcast.IndexKey
	v, err := s.Store.Setting(ctx, settingPodcastIndex)
	if err != nil || v == "" {
		return nil, err
	}
	if err := json.Unmarshal([]byte(v), &k); err != nil || k.Key == "" {
		return nil, err
	}
	return &podcast.Index{Key: k, Base: s.PodcastIndexBase}, nil
}

// ---- the Podcast Index key (admins) ----

// podcastIndexView is GET /integrations/podcastindex. The key and secret
// never leave the server; Hint is the key's last four characters.
type podcastIndexView struct {
	Connected bool   `json:"connected"`
	Hint      string `json:"hint,omitempty"`
}

func (s *Server) getPodcastIndex(w http.ResponseWriter, r *http.Request) {
	ix, err := s.podcastIndex(r.Context())
	if err != nil {
		s.serverError(w, err)
		return
	}
	v := podcastIndexView{}
	if ix != nil {
		v.Connected = true
		if k := ix.Key.Key; len(k) >= 8 {
			v.Hint = k[len(k)-4:]
		}
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) putPodcastIndex(w http.ResponseWriter, r *http.Request) {
	var k podcast.IndexKey
	if err := readJSON(r, &k); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	k.Key, k.Secret = strings.TrimSpace(k.Key), strings.TrimSpace(k.Secret)
	if k.Key == "" || k.Secret == "" {
		writeError(w, http.StatusBadRequest, "enter both the API key and the API secret")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	switch err := (&podcast.Index{Key: k, Base: s.PodcastIndexBase}).Check(ctx); {
	case errors.Is(err, podcast.ErrBadKey):
		writeError(w, http.StatusBadRequest, "Podcast Index didn't accept that key")
		return
	case err != nil:
		s.Log.Warn("podcast index check", "err", err)
		writeError(w, http.StatusBadGateway, "Podcast Index couldn't be reached to check the key")
		return
	}
	b, _ := json.Marshal(k)
	if err := s.Store.SetSetting(r.Context(), settingPodcastIndex, string(b)); err != nil {
		s.serverError(w, err)
		return
	}
	s.podcastCache.clear()
	r.Body = http.NoBody
	s.getPodcastIndex(w, r)
}

func (s *Server) deletePodcastIndex(w http.ResponseWriter, r *http.Request) {
	if err := s.Store.SetSetting(r.Context(), settingPodcastIndex, ""); err != nil {
		s.serverError(w, err)
		return
	}
	s.podcastCache.clear()
	w.WriteHeader(http.StatusNoContent)
}

// ---- finding shows ----

// directoryShow is a show in Podcast Index, with the show's id when a
// library has it already.
type directoryShow struct {
	podcast.Show
	ItemID int64 `json:"itemId,omitempty"`
}

// podcastDirectory searches Podcast Index (?q=), or lists what's trending
// in a language (?lang=), in all shows or in one category (?category=, an
// id from Categories). Connected is false when no admin entered a Podcast
// Index key, so the apps offer pasting a feed's address instead.
func (s *Server) podcastDirectory(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ix, err := s.podcastIndex(ctx)
	if err != nil {
		s.serverError(w, err)
		return
	}
	out := struct {
		Connected  bool               `json:"connected"`
		Shows      []directoryShow    `json:"shows"`
		Categories []podcast.Category `json:"categories"`
	}{Connected: ix != nil, Shows: []directoryShow{}, Categories: podcast.Categories}
	if ix == nil {
		writeJSON(w, http.StatusOK, out)
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	lang := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("lang")))
	if i := strings.IndexAny(lang, "-_"); i > 0 {
		lang = lang[:i] // Podcast Index knows languages, not regions
	}
	category := r.URL.Query().Get("category")
	if _, ok := podcast.CategoryByID(category); !ok {
		category = ""
	}
	key := "trending:" + lang + ":" + category
	if q != "" {
		key = "search:" + strings.ToLower(q)
	}
	shows, err := cached(&s.podcastCache, key, 30*time.Minute, func() ([]podcast.Show, error) {
		if q != "" {
			return ix.Search(ctx, q, 40)
		}
		return ix.Trending(ctx, lang, category, 40)
	})
	if err != nil {
		if errors.Is(err, podcast.ErrBadKey) {
			writeError(w, http.StatusBadGateway, "Podcast Index didn't accept the server's key")
			return
		}
		s.Log.Warn("podcast index", "err", err)
		writeError(w, http.StatusBadGateway, "Podcast Index isn't answering right now. Try again later.")
		return
	}
	var urls []string
	for _, sh := range shows {
		urls = append(urls, sh.FeedURL)
	}
	have, err := s.Store.PodcastsByFeed(ctx, urls)
	if err != nil {
		s.serverError(w, err)
		return
	}
	for _, sh := range shows {
		out.Shows = append(out.Shows, directoryShow{Show: sh, ItemID: have[sh.FeedURL]})
	}
	writeJSON(w, http.StatusOK, out)
}

// ---- adding and removing shows ----

// podcastLibrary loads a podcast library the person can see.
func (s *Server) podcastLibrary(w http.ResponseWriter, r *http.Request, id int64) (store.Library, bool) {
	if s.libraryHidden(w, r, id) {
		return store.Library{}, false
	}
	lib, err := s.Store.Library(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "library not found")
		return lib, false
	}
	if err != nil {
		s.serverError(w, err)
		return lib, false
	}
	if lib.Kind != "podcasts" {
		writeError(w, http.StatusBadRequest, "not a podcast library")
		return lib, false
	}
	return lib, true
}

// subscribeError answers for a show that couldn't be added.
func (s *Server) subscribeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, podcast.ErrBadURL), errors.Is(err, podcast.ErrNotFeed), errors.Is(err, podcast.ErrNoEpisodes),
		errors.Is(err, store.ErrPodcastTitleTaken):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		s.Log.Info("adding a podcast", "err", err)
		writeError(w, http.StatusBadGateway, "the feed couldn't be read. Check the address and try again.")
	}
}

// addPodcast adds a show to a podcast library by its feed's address, and
// follows it for whoever added it.
func (s *Server) addPodcast(w http.ResponseWriter, r *http.Request) {
	var req struct {
		LibraryID int64  `json:"libraryId"`
		FeedURL   string `json:"feedUrl"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	lib, ok := s.podcastLibrary(w, r, req.LibraryID)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	id, err := s.Podcasts.Subscribe(ctx, lib, req.FeedURL)
	if err != nil {
		s.subscribeError(w, err)
		return
	}
	if err := s.Store.Follow(r.Context(), currentUser(r).ID, id); err != nil {
		s.serverError(w, err)
		return
	}
	it, err := s.Store.Item(r.Context(), currentUser(r).ID, id)
	if err != nil {
		s.serverError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, it)
}

// loadPodcast loads a show from the path's id.
func (s *Server) loadPodcast(w http.ResponseWriter, r *http.Request) (store.Item, bool) {
	it, ok := s.loadItem(w, r)
	if ok && it.Kind != "podcast" {
		writeError(w, http.StatusBadRequest, "not a podcast")
		return it, false
	}
	return it, ok
}

func (s *Server) deletePodcast(w http.ResponseWriter, r *http.Request) {
	it, ok := s.loadPodcast(w, r)
	if !ok {
		return
	}
	if err := s.Store.DeleteItem(r.Context(), it.ID); err != nil {
		s.serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// refreshPodcast checks a show's feed for new episodes now.
func (s *Server) refreshPodcast(w http.ResponseWriter, r *http.Request) {
	it, ok := s.loadPodcast(w, r)
	if !ok {
		return
	}
	f, err := s.Store.PodcastFeedOf(r.Context(), it.ID)
	if err != nil {
		s.serverError(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	added, err := s.Podcasts.Refresh(ctx, f)
	if err != nil {
		s.Log.Info("refreshing a podcast", "show", it.Title, "err", err)
		writeError(w, http.StatusBadGateway, "the feed couldn't be read right now")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"added": added})
}

// ---- following ----

func (s *Server) followPodcast(w http.ResponseWriter, r *http.Request) {
	s.setFollow(w, r, true)
}

func (s *Server) unfollowPodcast(w http.ResponseWriter, r *http.Request) {
	s.setFollow(w, r, false)
}

func (s *Server) setFollow(w http.ResponseWriter, r *http.Request, follow bool) {
	it, ok := s.loadPodcast(w, r)
	if !ok {
		return
	}
	var err error
	if follow {
		err = s.Store.Follow(r.Context(), currentUser(r).ID, it.ID)
	} else {
		err = s.Store.Unfollow(r.Context(), currentUser(r).ID, it.ID)
	}
	if err != nil {
		s.serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// followedShow is a show someone follows, with how many of its episodes
// they haven't played.
type followedShow struct {
	store.Item
	Unplayed int `json:"unplayed"`
}

// followedPodcasts lists the shows the person follows, the one with the
// newest episode first.
func (s *Server) followedPodcasts(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), currentUser(r).ID
	shows, err := s.Store.FollowedPodcasts(ctx, uid)
	if err != nil {
		s.serverError(w, err)
		return
	}
	ids := make([]int64, len(shows))
	for i, sh := range shows {
		ids[i] = sh.ID
	}
	unplayed, err := s.Store.UnplayedEpisodes(ctx, uid, ids)
	if err != nil {
		s.serverError(w, err)
		return
	}
	out := []followedShow{}
	for _, sh := range shows {
		out = append(out, followedShow{Item: sh, Unplayed: unplayed[sh.ID]})
	}
	writeJSON(w, http.StatusOK, out)
}

// markPodcastPlayed marks every episode of a show played, or unplayed.
func (s *Server) markPodcastPlayed(w http.ResponseWriter, r *http.Request) {
	it, ok := s.loadPodcast(w, r)
	if !ok {
		return
	}
	var req struct {
		Played bool `json:"played"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if err := s.Store.MarkPodcastPlayed(r.Context(), currentUser(r).ID, it.ID, req.Played); err != nil {
		s.serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// podcastView is what a show's page has besides the item: its feed and
// whether the person follows it. Only admins see a failed refresh's reason.
type podcastView struct {
	FeedURL     string `json:"feedUrl"`
	Link        string `json:"link,omitempty"`
	RefreshedAt int64  `json:"refreshedAt"`
	Error       string `json:"error,omitempty"`
	Following   bool   `json:"following"`
	Unplayed    int    `json:"unplayed"`
}

// addPodcastDetails fills in a show's or an episode's extras on its page.
func (s *Server) addPodcastDetails(ctx context.Context, u store.User, v *itemView) error {
	switch v.Kind {
	case "podcast":
		f, err := s.Store.PodcastFeedOf(ctx, v.ID)
		if err != nil {
			return err
		}
		pv := &podcastView{FeedURL: f.URL, Link: f.Link, RefreshedAt: f.RefreshedAt}
		if u.IsAdmin {
			pv.Error = f.Error
		}
		if pv.Following, err = s.Store.Follows(ctx, u.ID, v.ID); err != nil {
			return err
		}
		unplayed, err := s.Store.UnplayedEpisodes(ctx, u.ID, []int64{v.ID})
		if err != nil {
			return err
		}
		pv.Unplayed = unplayed[v.ID]
		v.Podcast = pv
	case "podcastEpisode":
		e, err := s.Store.EpisodeInfoOf(ctx, v.ID)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return err
		}
		v.Episode = &e
		// Where the episode is on the internet isn't for everyone to see.
		if u.IsAdmin {
			v.FilePath = store.EpisodeURL(v.FilePath)
		}
	}
	return nil
}

// ---- moving shows between apps (OPML) ----

// exportPodcasts sends a podcast library's shows as an OPML file, for
// another podcast app or another server.
func (s *Server) exportPodcasts(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	lib, ok := s.podcastLibrary(w, r, id)
	if !ok {
		return
	}
	feeds, err := s.Store.PodcastFeeds(r.Context(), lib.ID)
	if err != nil {
		s.serverError(w, err)
		return
	}
	var list []podcast.OPMLFeed
	for _, f := range feeds {
		list = append(list, podcast.OPMLFeed{Title: f.Title, URL: f.URL})
	}
	w.Header().Set("Content-Type", "text/x-opml; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="podcasts.opml"`)
	w.Write(podcast.WriteOPML(lib.Name, list, time.Now()))
}

// importResult is one show of an imported OPML file that couldn't be added.
type importProblem struct {
	Title string `json:"title"`
	URL   string `json:"feedUrl"`
	Error string `json:"error"`
}

// importPodcasts adds every show in an OPML file (the request's body) to
// a podcast library, and follows them for whoever imported them.
func (s *Server) importPodcasts(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	lib, ok := s.podcastLibrary(w, r, id)
	if !ok {
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "the file is too big")
		return
	}
	feeds, err := podcast.ParseOPML(body)
	if err != nil || len(feeds) == 0 {
		writeError(w, http.StatusBadRequest, "that isn't an OPML file")
		return
	}
	if len(feeds) > 500 {
		feeds = feeds[:500]
	}
	ctx, uid := s.BackgroundCtx, currentUser(r).ID
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	added, problems := 0, []importProblem{}
	for _, f := range feeds {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer func() { <-sem; wg.Done() }()
			fctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			pid, err := s.Podcasts.Subscribe(fctx, lib, f.URL)
			if err == nil {
				err = s.Store.Follow(ctx, uid, pid)
			}
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				msg := "the feed couldn't be read"
				if errors.Is(err, podcast.ErrNotFeed) || errors.Is(err, podcast.ErrNoEpisodes) || errors.Is(err, podcast.ErrBadURL) ||
					errors.Is(err, store.ErrPodcastTitleTaken) {
					msg = err.Error()
				}
				problems = append(problems, importProblem{Title: f.Title, URL: f.URL, Error: msg})
				return
			}
			added++
		}()
	}
	wg.Wait()
	writeJSON(w, http.StatusOK, map[string]any{"added": added, "problems": problems})
}
