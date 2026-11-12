package api

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/yeixio/matinee/internal/scan"
	"github.com/yeixio/matinee/internal/store"
)

// Admins can edit a movie's, show's or episode's details by hand; edited
// fields are locked so lookups and scans leave them alone (see
// store.EditDetails). Each library says whether details from .nfo files
// next to the media or from online come first, or only the files.

func (s *Server) editable(w http.ResponseWriter, r *http.Request) (store.Item, bool) {
	it, ok := s.loadItem(w, r)
	if !ok {
		return it, false
	}
	switch it.Kind {
	case "movie", "show", "season", "episode":
		return it, true
	}
	writeError(w, http.StatusBadRequest, "invalid request")
	return it, false
}

// PUT /items/{id}/details
func (s *Server) editItemDetails(w http.ResponseWriter, r *http.Request) {
	it, ok := s.editable(w, r)
	if !ok {
		return
	}
	var e store.Edit
	if err := readJSON(r, &e); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if (it.Kind == "show" || it.Kind == "season") && (e.Title != nil || e.Year != nil) {
		// Episodes find their show and season by those, from the folders,
		// so they're changed by renaming the folder.
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if e.Year != nil && *e.Year != 0 && (*e.Year < 1870 || *e.Year > 2200) {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if e.Title != nil && strings.TrimSpace(*e.Title) == "" {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if err := s.Store.EditDetails(r.Context(), it.ID, e); err != nil {
		s.serverError(w, err)
		return
	}
	s.writeItem(w, r, it.ID)
}

// DELETE /items/{id}/details: undo hand edits; the details come from the
// files and online again.
func (s *Server) unlockItemDetails(w http.ResponseWriter, r *http.Request) {
	it, ok := s.editable(w, r)
	if !ok {
		return
	}
	if err := s.Store.UnlockDetails(r.Context(), it.ID); err != nil {
		s.serverError(w, err)
		return
	}
	if lib, err := s.Store.Library(r.Context(), it.LibraryID); err == nil {
		ctx := s.BackgroundCtx
		if ctx == nil {
			ctx = context.Background()
		}
		go func() {
			if err := scan.ReadNFOs(ctx, s.Store, lib); err != nil {
				s.Log.Warn("read .nfo files", "library", lib.Name, "err", err)
			}
			if s.Refetch != nil {
				s.Refetch(ctx, lib)
			}
		}()
	}
	s.writeItem(w, r, it.ID)
}

// PUT /libraries/{id}/metadata-mode
func (s *Server) setLibraryMetadataMode(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	var in struct {
		Mode string `json:"mode"`
	}
	if err := readJSON(r, &in); err != nil || !store.ValidMetadataMode(in.Mode) {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	err := s.Store.SetMetadataMode(r.Context(), id, in.Mode)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "library not found")
		return
	}
	if err != nil {
		s.serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// addEpisodeOrder says how a show's episodes named by absolute number are
// put in seasons, and an episode's absolute number.
func (s *Server) addEpisodeOrder(ctx context.Context, v *itemView) error {
	var err error
	switch v.Kind {
	case "show":
		order, absolute, e := s.Store.EpisodeOrder(ctx, v.ID)
		if absolute {
			v.EpisodeOrder = order
		}
		err = e
	case "episode":
		v.Absolute, err = s.Store.AbsoluteNumber(ctx, v.ID)
	}
	return err
}

// PUT /items/{id}/episode-order {"order": "aired" | "dvd" | "absolute"}:
// how a show's episodes named by their number from the first are put in
// seasons. They move in the background.
func (s *Server) setEpisodeOrder(w http.ResponseWriter, r *http.Request) {
	it, ok := s.loadItem(w, r)
	if !ok {
		return
	}
	var in struct {
		Order string `json:"order"`
	}
	if err := readJSON(r, &in); err != nil || it.Kind != "show" || !store.ValidEpisodeOrder(in.Order) {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if err := s.Store.SetEpisodeOrder(r.Context(), it.ID, in.Order); err != nil {
		s.serverError(w, err)
		return
	}
	s.refetch(it)
	w.WriteHeader(http.StatusNoContent)
}
