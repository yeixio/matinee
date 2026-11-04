package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/yeixio/matinee/internal/metadata"
	"github.com/yeixio/matinee/internal/scan"
	"github.com/yeixio/matinee/internal/store"
)

// Admins fix a movie's or show's online match from its page: search for
// the right title (or paste a TMDB or IMDb id) and pick it, refresh its
// details, or clear the match so it goes by its file name. A match picked
// or cleared by hand is locked (see store.SetMatch).

// Matcher searches the online database movies and shows are matched with.
type Matcher interface {
	Ready() bool
	Search(ctx context.Context, kind, title string, year int) ([]metadata.Candidate, error)
	Find(ctx context.Context, kind, ref string) (*store.Metadata, *metadata.Candidate, error)
}

// matchable loads the item and checks it's a movie or show.
func (s *Server) matchable(w http.ResponseWriter, r *http.Request) (store.Item, bool) {
	it, ok := s.loadItem(w, r)
	if !ok {
		return it, false
	}
	if it.Kind != "movie" && it.Kind != "show" {
		writeError(w, http.StatusBadRequest, "only movies and shows have a match to fix")
		return it, false
	}
	return it, true
}

func (s *Server) matcherReady(w http.ResponseWriter) bool {
	if s.Matcher == nil || !s.Matcher.Ready() {
		writeError(w, http.StatusServiceUnavailable, "add a TMDB key in Settings → Posters and details to look titles up")
		return false
	}
	return true
}

// matchError answers for a failed lookup.
func (s *Server) matchError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, metadata.ErrUnknownRef):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, metadata.ErrBadKey), errors.Is(err, metadata.ErrNoKey):
		writeError(w, http.StatusServiceUnavailable, "TMDB didn't accept the key in Settings → Posters and details")
	case s.noInternet(w, err):
	default:
		s.Log.Warn("match lookup", "err", err)
		writeError(w, http.StatusBadGateway, "couldn't reach TMDB; try again in a moment")
	}
}

// itemMatch says what a movie or show is matched with, and lists titles
// to pick from: those found for ?q= (and ?year=), or for the item's own
// title and year. A q that is a TMDB id, a themoviedb.org address or an
// IMDb id lists that one title.
func (s *Server) itemMatch(w http.ResponseWriter, r *http.Request) {
	it, ok := s.matchable(w, r)
	if !ok {
		return
	}
	m, err := s.Store.ItemMatch(r.Context(), it.ID)
	if err != nil {
		s.serverError(w, err)
		return
	}
	out := map[string]any{"tmdbId": m.TMDBID, "locked": m.Locked, "results": []metadata.Candidate{}}
	if s.Matcher == nil || !s.Matcher.Ready() {
		out["ready"] = false
		writeJSON(w, http.StatusOK, out)
		return
	}
	out["ready"] = true
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	year, _ := strconv.Atoi(r.URL.Query().Get("year"))
	if q == "" {
		q = it.Title
		if it.Year != nil && r.URL.Query().Get("year") == "" {
			year = *it.Year
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	if looksLikeRef(q) {
		_, c, err := s.Matcher.Find(ctx, it.Kind, q)
		if err != nil {
			s.matchError(w, err)
			return
		}
		if c != nil {
			out["results"] = []metadata.Candidate{*c}
		}
		writeJSON(w, http.StatusOK, out)
		return
	}
	results, err := s.Matcher.Search(ctx, it.Kind, q, year)
	if err != nil {
		s.matchError(w, err)
		return
	}
	out["results"] = results
	writeJSON(w, http.StatusOK, out)
}

// looksLikeRef reports whether a search is really a TMDB id, a
// themoviedb.org address or an IMDb id.
func looksLikeRef(q string) bool {
	if _, err := strconv.ParseInt(q, 10, 64); err == nil && len(q) >= 2 {
		// A number alone is a TMDB id, unless it's a title like "1917";
		// pasting an id is far rarer than searching for a year-like title,
		// so only long numbers count.
		return len(q) >= 5
	}
	return strings.HasPrefix(strings.ToLower(q), "tmdb:") || strings.Contains(q, "themoviedb.org/") || strings.HasPrefix(q, "tt") && len(q) > 6 && isDigits(q[2:]) || strings.Contains(q, "imdb.com/title/tt")
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}

// setItemMatch matches a movie or show with the title sent as JSON
// {"ref": …}: a TMDB id, a themoviedb.org address or an IMDb id.
func (s *Server) setItemMatch(w http.ResponseWriter, r *http.Request) {
	it, ok := s.matchable(w, r)
	if !ok || !s.matcherReady(w) {
		return
	}
	var body struct {
		Ref string `json:"ref"`
	}
	if err := readJSON(r, &body); err != nil || strings.TrimSpace(body.Ref) == "" {
		writeError(w, http.StatusBadRequest, "send the TMDB id, TMDB address or IMDb id to match with")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	m, _, err := s.Matcher.Find(ctx, it.Kind, body.Ref)
	if err != nil {
		s.matchError(w, err)
		return
	}
	if m == nil {
		writeError(w, http.StatusNotFound, "TMDB has no such title")
		return
	}
	s.rematch(w, r, it, m, true)
}

// clearItemMatch takes a movie's or show's match away, so it goes by its
// file name, and keeps lookups from matching it again.
func (s *Server) clearItemMatch(w http.ResponseWriter, r *http.Request) {
	it, ok := s.matchable(w, r)
	if !ok {
		return
	}
	s.rematch(w, r, it, nil, true)
}

// refreshItem fetches a movie's or show's details, artwork, cast and
// translations again. One that isn't matched, and wasn't cleared by hand,
// is looked up again from the start.
func (s *Server) refreshItem(w http.ResponseWriter, r *http.Request) {
	it, ok := s.matchable(w, r)
	if !ok {
		return
	}
	cur, err := s.Store.ItemMatch(r.Context(), it.ID)
	if err != nil {
		s.serverError(w, err)
		return
	}
	if cur.TMDBID == 0 {
		for _, id := range s.versionIDs(r, it) {
			if err := s.Store.LookUpAgain(r.Context(), id); err != nil {
				s.serverError(w, err)
				return
			}
		}
		s.refetch(it)
		s.writeItem(w, r, it.ID)
		return
	}
	if !s.matcherReady(w) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	m, _, err := s.Matcher.Find(ctx, it.Kind, strconv.FormatInt(cur.TMDBID, 10))
	if err != nil {
		s.matchError(w, err)
		return
	}
	if m == nil {
		writeError(w, http.StatusNotFound, "TMDB no longer has this title; fix the match instead")
		return
	}
	s.rematch(w, r, it, m, cur.Locked)
}

// rematch matches every version of the title alike, so they stay one.
func (s *Server) rematch(w http.ResponseWriter, r *http.Request, it store.Item, m *store.Metadata, locked bool) {
	for _, id := range s.versionIDs(r, it) {
		if err := s.Store.SetMatch(r.Context(), id, m, locked); err != nil {
			s.serverError(w, err)
			return
		}
	}
	if _, err := s.Store.GroupVersions(r.Context(), it.LibraryID, scan.Edition); err != nil {
		s.serverError(w, err)
		return
	}
	s.refetch(it)
	s.writeItem(w, r, it.ID)
}

// versionIDs lists the item and the other versions of its title.
func (s *Server) versionIDs(r *http.Request, it store.Item) []int64 {
	ids := []int64{it.ID}
	if it.Kind != "movie" && it.Kind != "episode" {
		return ids
	}
	vs, err := s.Store.Versions(r.Context(), 0, it.ID)
	if err != nil {
		return ids
	}
	for _, v := range vs {
		if v.ID != it.ID {
			ids = append(ids, v.ID)
		}
	}
	return ids
}

// refetch looks up the rest (cast, seasons and episodes, translations) in
// the background.
func (s *Server) refetch(it store.Item) {
	if s.Refetch == nil {
		return
	}
	lib, err := s.Store.Library(context.Background(), it.LibraryID)
	if err != nil {
		s.Log.Warn("refetch", "err", err)
		return
	}
	ctx := s.BackgroundCtx
	if ctx == nil {
		ctx = context.Background()
	}
	go s.Refetch(ctx, lib)
}

func (s *Server) writeItem(w http.ResponseWriter, r *http.Request, id int64) {
	it, err := s.Store.Item(r.Context(), currentUser(r).ID, id)
	if err != nil {
		s.serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, it)
}
