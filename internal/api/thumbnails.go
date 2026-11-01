package api

import (
	"net/http"
	"os"
	"strconv"

	"github.com/yeixio/matinee/internal/thumbs"
)

// thumbnailsView says whether a video has seek bar pictures and how
// they're laid out (see store.Thumbnails).
type thumbnailsView struct {
	// State is ready when there are pictures; making while they're being
	// made for someone who's watching, so a player asks again in a while;
	// none when there are none yet (or the file can't be read); off when
	// the admin turned them off.
	State      string `json:"state"`
	IntervalMs int64  `json:"intervalMs,omitempty"`
	Width      int    `json:"width,omitempty"`
	Height     int    `json:"height,omitempty"`
	Columns    int    `json:"columns,omitempty"`
	Rows       int    `json:"rows,omitempty"`
	Count      int    `json:"count,omitempty"`
	Sheets     int    `json:"sheets,omitempty"`
	// Tag changes when the pictures are made again; sheet URLs carry it
	// as ?v= so players don't show old ones from their cache.
	Tag string `json:"tag,omitempty"`
}

// itemThumbnails is what players ask for when a video starts. A video
// without pictures gets them made now, ahead of the background pass.
func (s *Server) itemThumbnails(w http.ResponseWriter, r *http.Request) {
	it, ok := s.loadItem(w, r)
	if !ok {
		return
	}
	if it.Kind != "movie" && it.Kind != "episode" {
		writeError(w, http.StatusBadRequest, "not a video")
		return
	}
	v := thumbnailsView{State: "off"}
	if s.Thumbs != nil {
		mode, err := s.Thumbs.Mode(r.Context())
		if err != nil {
			s.serverError(w, err)
			return
		}
		if mode != thumbs.Off {
			t, ok, err := s.Store.ItemThumbnails(r.Context(), it.ID, thumbs.Version)
			if err != nil {
				s.serverError(w, err)
				return
			}
			if _, statErr := os.Stat(s.Thumbs.SheetPath(it.ID, 0)); ok && statErr == nil {
				v = thumbnailsView{State: "ready", IntervalMs: t.IntervalMs, Width: t.Width, Height: t.Height,
					Columns: t.Columns, Rows: t.Rows, Count: t.Count, Sheets: t.Sheets(), Tag: strconv.FormatInt(t.MadeAt, 36)}
			} else if s.Thumbs.Request(r.Context(), it.ID) {
				v.State = "making"
			} else {
				v.State = "none"
			}
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, v)
}

// thumbnailSheet is one sheet of a video's pictures, a JPEG.
func (s *Server) thumbnailSheet(w http.ResponseWriter, r *http.Request) {
	it, ok := s.loadItem(w, r)
	if !ok {
		return
	}
	n, err := strconv.Atoi(r.PathValue("sheet"))
	if err != nil || n < 0 || s.Thumbs == nil {
		writeError(w, http.StatusNotFound, "no such sheet")
		return
	}
	f, err := os.Open(s.Thumbs.SheetPath(it.ID, n))
	if err != nil {
		writeError(w, http.StatusNotFound, "no such sheet")
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		s.serverError(w, err)
		return
	}
	// The URL carries the pictures' tag, so a new set is a new URL.
	w.Header().Set("Cache-Control", "private, max-age=604800")
	w.Header().Set("Content-Type", "image/jpeg")
	http.ServeContent(w, r, "", info.ModTime(), f)
}
