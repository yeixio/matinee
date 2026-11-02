package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/yeixio/matinee/internal/radio"
	"github.com/yeixio/matinee/internal/store"
	"github.com/yeixio/matinee/internal/stream"
)

// Radio stations live in music libraries (see store/stations.go and
// internal/radio). Anyone in the household who isn't on a kids profile can
// add, rename and remove them; everyone who can see the library can play
// them.

// musicLibrary loads a music library the person can see, from an id.
func (s *Server) musicLibrary(w http.ResponseWriter, r *http.Request, id int64) (store.Library, bool) {
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
	if lib.Kind != "music" {
		writeError(w, http.StatusBadRequest, "not a music library")
		return lib, false
	}
	return lib, true
}

// libraryStations lists a music library's radio stations.
func (s *Server) libraryStations(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	if _, ok := s.musicLibrary(w, r, id); !ok {
		return
	}
	items, err := s.Store.Stations(r.Context(), currentUser(r).ID, id)
	if err != nil {
		s.serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

// stationDirectory finds stations in Radio Browser, by name (?q=) or genre (?tag=).
func (s *Server) stationDirectory(w http.ResponseWriter, r *http.Request) {
	q, tag := strings.TrimSpace(r.URL.Query().Get("q")), strings.TrimSpace(r.URL.Query().Get("tag"))
	if q == "" && tag == "" {
		writeError(w, http.StatusBadRequest, "q or tag is required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	found, err := radio.Search(ctx, stream.Remote, s.RadioBase, q+tag, q == "")
	if err != nil {
		s.Log.Info("searching for radio stations", "err", err)
		writeError(w, http.StatusBadGateway, "the station directory isn't answering right now")
		return
	}
	writeJSON(w, http.StatusOK, found)
}

// addStation adds a radio station to a music library by its stream's
// address: {"libraryId": 1, "url": "…", "name": "…", "image": "…"}. The
// stream is checked first; a playlist file is followed to its stream.
func (s *Server) addStation(w http.ResponseWriter, r *http.Request) {
	if currentUser(r).Friend {
		writeError(w, http.StatusForbidden, "only the household can change this")
		return
	}
	var req struct {
		LibraryID int64    `json:"libraryId"`
		URL       string   `json:"url"`
		Name      string   `json:"name"`
		Image     string   `json:"image"`
		Homepage  string   `json:"homepage"`
		Tags      []string `json:"tags"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	lib, ok := s.musicLibrary(w, r, req.LibraryID)
	if !ok {
		return
	}
	// Finish even if the app stops waiting, as for podcasts.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 20*time.Second)
	defer cancel()
	info, err := radio.Probe(ctx, stream.Remote, req.URL)
	switch {
	case errors.Is(err, radio.ErrBadURL), errors.Is(err, radio.ErrNotAudio), errors.Is(err, radio.ErrHLS):
		writeError(w, http.StatusBadRequest, err.Error())
		return
	case err != nil:
		s.Log.Info("adding a radio station", "err", err)
		if currentUser(r).IsAdmin {
			writeErrorWith(w, http.StatusBadGateway, "the station couldn't be reached: {{reason}}", map[string]string{"reason": err.Error()})
			return
		}
		writeError(w, http.StatusBadGateway, "the station couldn't be reached. Check the address and try again.")
		return
	}
	name := firstNonEmpty(req.Name, info.Name, hostOf(info.URL))
	if len([]rune(name)) > 120 {
		name = string([]rune(name)[:120])
	}
	image := ""
	if strings.HasPrefix(req.Image, "https://") || strings.HasPrefix(req.Image, "http://") {
		image = req.Image
	}
	tags := req.Tags
	if len(tags) == 0 && info.Genre != "" {
		tags = []string{info.Genre}
	}
	id, err := s.Store.AddStation(r.Context(), lib.ID, store.Station{
		Name: name, URL: info.URL, Homepage: firstNonEmpty(req.Homepage, info.Homepage), Image: image, Genres: tags,
	})
	if errors.Is(err, store.ErrStationTaken) {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
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

// loadStation loads a station from the path's id, for the household.
func (s *Server) loadStation(w http.ResponseWriter, r *http.Request) (store.Item, bool) {
	if currentUser(r).Friend {
		writeError(w, http.StatusForbidden, "only the household can change this")
		return store.Item{}, false
	}
	it, ok := s.loadItem(w, r)
	if ok && it.Kind != "station" {
		writeError(w, http.StatusBadRequest, "not a radio station")
		return it, false
	}
	return it, ok
}

// renameStation renames a station: {"name": "…"}.
func (s *Server) renameStation(w http.ResponseWriter, r *http.Request) {
	it, ok := s.loadStation(w, r)
	if !ok {
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := readJSON(r, &req); err != nil || strings.TrimSpace(req.Name) == "" || len([]rune(req.Name)) > 120 {
		writeError(w, http.StatusBadRequest, "name: give one of up to 120 characters")
		return
	}
	if err := s.Store.RenameStation(r.Context(), it.ID, req.Name); err != nil {
		s.serverError(w, err)
		return
	}
	it, err := s.Store.Item(r.Context(), currentUser(r).ID, it.ID)
	if err != nil {
		s.serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, it)
}

// deleteStation removes a station.
func (s *Server) deleteStation(w http.ResponseWriter, r *http.Request) {
	it, ok := s.loadStation(w, r)
	if !ok {
		return
	}
	if err := s.Store.DeleteStation(r.Context(), it.ID); err != nil {
		s.serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// playStation relays a station's stream to a player (GET /items/{id}/file),
// keeping the song titles it sends for /items/{id}/now.
func (s *Server) playStation(w http.ResponseWriter, r *http.Request, it store.Item) {
	addr, err := s.Store.StationURL(r.Context(), it.ID)
	if err != nil {
		writeError(w, http.StatusNotFound, "item not found")
		return
	}
	radio.Relay(w, r, stream.Remote, addr, func(title string) { s.stationTitles.Set(it.ID, title) })
}

// stationNow says what a station is playing, from the titles it sends
// while someone listens: {"title": "Artist - Song"}, or {} when it hasn't
// said.
func (s *Server) stationNow(w http.ResponseWriter, r *http.Request) {
	it, ok := s.loadItem(w, r)
	if !ok {
		return
	}
	if it.Kind != "station" {
		writeError(w, http.StatusBadRequest, "not a radio station")
		return
	}
	if t, ok := s.stationTitles.Get(it.ID); ok {
		writeJSON(w, http.StatusOK, t)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{})
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	return ""
}

// hostOf names a station after its address when nothing else names it.
func hostOf(addr string) string {
	h := strings.TrimPrefix(strings.TrimPrefix(addr, "https://"), "http://")
	if i := strings.IndexAny(h, "/:"); i > 0 {
		h = h[:i]
	}
	return h
}
