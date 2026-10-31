package api

import (
	"errors"
	"net/http"
	"slices"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"

	"github.com/yeixio/matinee/internal/store"
)

// Playlists (see store.Playlist) are each person's own, so a profile's are
// its own and the Home guest's are shared by the household, like its watch
// list. Anyone who can listen can keep them.

const (
	playlistNameLimit = 100
	// playlistLimit caps how many songs a playlist holds.
	playlistLimit = 5000
)

type playlistView struct {
	store.Playlist
	Entries []store.PlaylistEntry `json:"entries"`
}

// listPlaylists lists someone's playlists. ?q= finds them by name, as
// someone would say it: the closest first, leaving out the rest.
func (s *Server) listPlaylists(w http.ResponseWriter, r *http.Request) {
	all, err := s.Store.Playlists(r.Context(), currentUser(r).ID)
	if err != nil {
		s.serverError(w, err)
		return
	}
	if q := r.URL.Query().Get("q"); q != "" {
		all = matchPlaylists(all, q)
	}
	writeJSON(w, http.StatusOK, all)
}

// matchPlaylists keeps the playlists whose name matches q: the same name
// first, then names that start with it, then names that hold it. Case,
// accents and punctuation don't count.
func matchPlaylists(all []store.Playlist, q string) []store.Playlist {
	want := foldName(q)
	if want == "" {
		return []store.Playlist{}
	}
	rank := func(p store.Playlist) int {
		name := foldName(p.Name)
		switch {
		case name == want:
			return 0
		case strings.HasPrefix(name, want):
			return 1
		case strings.Contains(name, want):
			return 2
		}
		return -1
	}
	out := []store.Playlist{}
	for _, p := range all {
		if rank(p) >= 0 {
			out = append(out, p)
		}
	}
	slices.SortStableFunc(out, func(a, b store.Playlist) int { return rank(a) - rank(b) })
	return out
}

// foldName lowers a name and drops accents and punctuation, keeping single
// spaces between words, so "Café Mix!" matches "cafe mix".
func foldName(s string) string {
	var b strings.Builder
	space := false
	for _, r := range norm.NFD.String(strings.ToLower(s)) {
		switch {
		case unicode.Is(unicode.Mn, r):
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			if space && b.Len() > 0 {
				b.WriteByte(' ')
			}
			space = false
			b.WriteRune(r)
		default:
			space = true
		}
	}
	return b.String()
}

// playlistName tidies a name, or says what's wrong with it.
func playlistName(w http.ResponseWriter, name string) (string, bool) {
	name = strings.TrimSpace(name)
	switch {
	case name == "":
		writeError(w, http.StatusBadRequest, "a playlist needs a name")
		return "", false
	case len(name) > playlistNameLimit:
		writeError(w, http.StatusBadRequest, "that name is too long")
		return "", false
	}
	return name, true
}

// loadPlaylist finds the playlist in the path, if it's the user's.
func (s *Server) loadPlaylist(w http.ResponseWriter, r *http.Request) (store.Playlist, bool) {
	p, err := s.Store.Playlist(r.Context(), currentUser(r).ID, r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "playlist not found")
		return p, false
	}
	if err != nil {
		s.serverError(w, err)
		return p, false
	}
	return p, true
}

func (s *Server) writePlaylist(w http.ResponseWriter, r *http.Request, status int, id string) {
	ctx, u := r.Context(), currentUser(r)
	p, err := s.Store.Playlist(ctx, u.ID, id)
	if err != nil {
		s.serverError(w, err)
		return
	}
	entries, err := s.Store.PlaylistEntries(ctx, u.ID, id)
	if err != nil {
		s.serverError(w, err)
		return
	}
	if status == http.StatusOK {
		writeLargeJSON(w, r, playlistView{p, entries})
		return
	}
	writeJSON(w, status, playlistView{p, entries})
}

func (s *Server) getPlaylist(w http.ResponseWriter, r *http.Request) {
	if p, ok := s.loadPlaylist(w, r); ok {
		s.writePlaylist(w, r, http.StatusOK, p.ID)
	}
}

// songs turns what someone picked into songs to add: a song is itself, and
// an album or artist is all of their songs, in album order.
func (s *Server) songs(w http.ResponseWriter, r *http.Request, ids []int64) ([]int64, bool) {
	ctx, u := r.Context(), currentUser(r)
	var out []int64
	for _, id := range ids {
		it, err := s.Store.Item(ctx, u.ID, id)
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "item not found")
			return nil, false
		}
		if err != nil {
			s.serverError(w, err)
			return nil, false
		}
		switch it.Kind {
		case "track":
			out = append(out, it.ID)
		case "album", "artist":
			tracks, err := s.Store.Tracks(ctx, u.ID, it.ID)
			if err != nil {
				s.serverError(w, err)
				return nil, false
			}
			for _, t := range tracks {
				out = append(out, t.ID)
			}
		default:
			writeError(w, http.StatusBadRequest, "only music goes on a playlist")
			return nil, false
		}
	}
	return out, true
}

func (s *Server) createPlaylist(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name  string  `json:"name"`
		Items []int64 `json:"items"`
	}
	if err := readJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "bad request")
		return
	}
	name, ok := playlistName(w, in.Name)
	if !ok {
		return
	}
	songs, ok := s.songs(w, r, in.Items)
	if !ok {
		return
	}
	if len(songs) > playlistLimit {
		writeError(w, http.StatusBadRequest, "a playlist holds up to 5000 songs")
		return
	}
	p, err := s.Store.CreatePlaylist(r.Context(), currentUser(r).ID, name)
	if err == nil && len(songs) > 0 {
		_, err = s.Store.AddToPlaylist(r.Context(), p.ID, songs)
	}
	if err != nil {
		s.serverError(w, err)
		return
	}
	s.writePlaylist(w, r, http.StatusCreated, p.ID)
}

func (s *Server) renamePlaylist(w http.ResponseWriter, r *http.Request) {
	p, ok := s.loadPlaylist(w, r)
	if !ok {
		return
	}
	var in struct {
		Name string `json:"name"`
	}
	if err := readJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "bad request")
		return
	}
	name, ok := playlistName(w, in.Name)
	if !ok {
		return
	}
	if err := s.Store.RenamePlaylist(r.Context(), currentUser(r).ID, p.ID, name); err != nil {
		s.serverError(w, err)
		return
	}
	s.writePlaylist(w, r, http.StatusOK, p.ID)
}

func (s *Server) deletePlaylist(w http.ResponseWriter, r *http.Request) {
	p, ok := s.loadPlaylist(w, r)
	if !ok {
		return
	}
	if err := s.Store.DeletePlaylist(r.Context(), currentUser(r).ID, p.ID); err != nil {
		s.serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// addToPlaylist puts songs, albums or artists' songs at the end.
func (s *Server) addToPlaylist(w http.ResponseWriter, r *http.Request) {
	p, ok := s.loadPlaylist(w, r)
	if !ok {
		return
	}
	var in struct {
		Items []int64 `json:"items"`
	}
	if err := readJSON(r, &in); err != nil || len(in.Items) == 0 {
		writeError(w, http.StatusBadRequest, "items is required")
		return
	}
	songs, ok := s.songs(w, r, in.Items)
	if !ok {
		return
	}
	if p.Count+len(songs) > playlistLimit {
		writeError(w, http.StatusBadRequest, "a playlist holds up to 5000 songs")
		return
	}
	if len(songs) > 0 {
		if _, err := s.Store.AddToPlaylist(r.Context(), p.ID, songs); err != nil {
			s.serverError(w, err)
			return
		}
	}
	s.writePlaylist(w, r, http.StatusOK, p.ID)
}

func (s *Server) removeFromPlaylist(w http.ResponseWriter, r *http.Request) {
	p, ok := s.loadPlaylist(w, r)
	if !ok {
		return
	}
	if err := s.Store.RemoveFromPlaylist(r.Context(), p.ID, []string{r.PathValue("entry")}); err != nil {
		s.serverError(w, err)
		return
	}
	s.writePlaylist(w, r, http.StatusOK, p.ID)
}

// orderPlaylist puts the playlist's songs in a new order, given as entry ids.
func (s *Server) orderPlaylist(w http.ResponseWriter, r *http.Request) {
	p, ok := s.loadPlaylist(w, r)
	if !ok {
		return
	}
	var in struct {
		Entries []string `json:"entries"`
	}
	if err := readJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "bad request")
		return
	}
	if err := s.Store.OrderPlaylist(r.Context(), p.ID, in.Entries); err != nil {
		s.serverError(w, err)
		return
	}
	s.writePlaylist(w, r, http.StatusOK, p.ID)
}
