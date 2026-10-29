package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/yeixio/matinee/internal/store"
	"github.com/yeixio/matinee/internal/stream"
)

// Audiobooks (see store/books.go): a book plays as one long listen across
// its parts, and where someone is in it is kept on the book.

// bookFinishedMs is how close to a book's end counts as finishing it.
// Books end on credits and silence, but not on 8% of a 20-hour listen.
const bookFinishedMs = 60_000

type bookPartView struct {
	store.BookPart
	MimeType string `json:"mimeType"`
}

type bookView struct {
	store.Item
	Parts    []bookPartView      `json:"parts"`
	Chapters []store.BookChapter `json:"chapters"`
}

// getBook answers a book, or a part's book, with its parts in order, its
// chapters on the book's clock and the person's place in it.
func (s *Server) getBook(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	b, err := s.Store.Book(r.Context(), currentUser(r).ID, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "book not found")
		return
	}
	if err != nil {
		s.serverError(w, err)
		return
	}
	v := bookView{Item: b.Item, Parts: []bookPartView{}, Chapters: b.Chapters}
	for _, p := range b.Parts {
		v.Parts = append(v.Parts, bookPartView{BookPart: p, MimeType: stream.MimeType(p.Path)})
	}
	writeJSON(w, http.StatusOK, v)
}

// setBookProgress saves a place in a whole book. Reaching the last minute
// finishes it.
func (s *Server) setBookProgress(w http.ResponseWriter, r *http.Request, it store.Item, positionMs int64, played bool) {
	if it.DurationMs != nil && *it.DurationMs > 0 && positionMs >= *it.DurationMs-bookFinishedMs {
		played = true
	}
	if played {
		positionMs = 0
	}
	if err := s.Store.SetBookProgress(r.Context(), currentUser(r).ID, it.ID, positionMs, played); err != nil {
		s.serverError(w, err)
		return
	}
	s.Plugins.Played(currentUser(r), it, positionMs, played)
	w.WriteHeader(http.StatusNoContent)
}

// librarySeries lists an audiobook library's series.
func (s *Server) librarySeries(w http.ResponseWriter, r *http.Request) {
	s.bookGroups(w, r, s.Store.Series)
}

// libraryNarrators lists who narrates an audiobook library's books.
func (s *Server) libraryNarrators(w http.ResponseWriter, r *http.Request) {
	s.bookGroups(w, r, s.Store.Narrators)
}

func (s *Server) bookGroups(w http.ResponseWriter, r *http.Request, list func(ctx context.Context, userID, libraryID int64) ([]store.BookGroup, error)) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	if s.libraryHidden(w, r, id) {
		return
	}
	groups, err := list(r.Context(), currentUser(r).ID, id)
	if err != nil {
		s.serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, groups)
}
