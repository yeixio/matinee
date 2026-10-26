package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/yeixio/matinee/internal/store"
)

// libraryItems lists a library's top level, narrowed and sorted by any
// store.ItemFilter parameters, such as ?genre=Comedy&watched=unwatched&sort=year.
func (s *Server) libraryItems(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	f, err := store.ParseItemFilter(r.URL.Query())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	f.Library = []int64{id}
	s.writeFiltered(w, r, f)
}

// filterItems is libraryItems across libraries (?library= picks some).
func (s *Server) filterItems(w http.ResponseWriter, r *http.Request) {
	f, err := store.ParseItemFilter(r.URL.Query())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.writeFiltered(w, r, f)
}

func (s *Server) writeFiltered(w http.ResponseWriter, r *http.Request, f store.ItemFilter) {
	items, err := s.Store.FilterItems(r.Context(), currentUser(r).ID, f)
	if err != nil {
		s.serverError(w, err)
		return
	}
	writeLargeJSON(w, r, items)
}

// libraryFilters says what a library can be filtered by.
func (s *Server) libraryFilters(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	if _, err := s.Store.Library(r.Context(), id); errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "library not found")
		return
	} else if err != nil {
		s.serverError(w, err)
		return
	}
	if s.libraryHidden(w, r, id) {
		return
	}
	o, err := s.Store.FilterOptions(r.Context(), id)
	if err != nil {
		s.serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, o)
}

// libraryPeople finds people credited in a library by name, for the
// person and director filters (?q=wea&role=cast).
func (s *Server) libraryPeople(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	role := r.URL.Query().Get("role")
	if role != "" && role != "cast" && role != "director" {
		writeError(w, http.StatusBadRequest, "role: use cast or director")
		return
	}
	if s.libraryHidden(w, r, id) {
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	people, err := s.Store.FilterPeople(r.Context(), id, role, r.URL.Query().Get("q"), limit)
	if err != nil {
		s.serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, people)
}

// libraryHidden answers 404 for a library a kids profile can't see, so its
// filter options don't list what's in it.
func (s *Server) libraryHidden(w http.ResponseWriter, r *http.Request, id int64) bool {
	ok, err := s.Store.LibraryVisible(r.Context(), currentUser(r), id)
	if err != nil {
		s.serverError(w, err)
		return true
	}
	if !ok {
		writeError(w, http.StatusNotFound, "library not found")
	}
	return !ok
}
