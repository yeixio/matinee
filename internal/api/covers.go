package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/yeixio/matinee/internal/metadata"
	"github.com/yeixio/matinee/internal/store"
)

// Admins can pick an album's or book's cover by hand: from covers found
// online (see metadata.Covers), from an image's web address, or by
// uploading one. The pick is kept in the database and wins over the
// folder, the files' tags and online lookups until it's reset.

// CoverFinder lists covers to pick from.
type CoverFinder interface {
	Find(ctx context.Context, q metadata.Query) []metadata.CoverOption
}

// maxCoverBytes is the largest cover Matinee takes.
const maxCoverBytes = 20 << 20

// pickableCover loads the item and checks it's an album or book.
func (s *Server) pickableCover(w http.ResponseWriter, r *http.Request) (store.Item, bool) {
	it, ok := s.loadItem(w, r)
	if !ok {
		return it, false
	}
	if it.Kind != "album" && it.Kind != "book" {
		writeError(w, http.StatusBadRequest, "only albums and books have covers to pick")
		return it, false
	}
	return it, true
}

// coverOptions lists covers found online for an album or book.
func (s *Server) coverOptions(w http.ResponseWriter, r *http.Request) {
	it, ok := s.pickableCover(w, r)
	if !ok {
		return
	}
	options := []metadata.CoverOption{}
	if s.Covers != nil {
		q := metadata.Query{Kind: it.Kind, Title: it.Title}
		if it.ParentID != nil {
			if p, err := s.Store.Item(r.Context(), 0, *it.ParentID); err == nil && !strings.HasPrefix(p.Title, "Unknown ") {
				q.Artist = p.Title
			}
		}
		if it.Kind == "album" {
			q.ReleaseID, q.ReleaseGroupID, _ = s.Store.AlbumTags(r.Context(), it.ID)
			if q.ReleaseGroupID == "" {
				q.ReleaseGroupID, _ = s.Store.MBID(r.Context(), it.ID)
			}
		}
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		if found := s.Covers.Find(ctx, q); found != nil {
			options = found
		}
	}
	source := ""
	if it.PosterTag != "" {
		if img, err := s.Store.ChosenImage(r.Context(), it.ID); err == nil {
			source = img.Source
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"options": options, "chosen": it.PosterTag != "", "chosenSource": source})
}

// setCover makes an image the cover: one at a web address, sent as JSON
// {"url": …}, or the image itself as the request body.
func (s *Server) setCover(w http.ResponseWriter, r *http.Request) {
	it, ok := s.pickableCover(w, r)
	if !ok {
		return
	}
	var data []byte
	source := "upload"
	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		var body struct {
			URL string `json:"url"`
		}
		if err := readJSON(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, "bad request")
			return
		}
		u, err := url.Parse(body.URL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			writeError(w, http.StatusBadRequest, "the cover's address must start with http:// or https://")
			return
		}
		if data, err = s.downloadCover(r.Context(), u.String()); err != nil {
			s.Log.Warn("download a picked cover", "url", u.String(), "err", err)
			writeError(w, http.StatusBadGateway, "couldn't get an image from that address")
			return
		}
		source = u.String()
	} else {
		var err error
		data, err = io.ReadAll(http.MaxBytesReader(w, r.Body, maxCoverBytes))
		if err != nil {
			writeError(w, http.StatusRequestEntityTooLarge, "the image is too big (20 MB at most)")
			return
		}
	}
	ct := http.DetectContentType(data)
	switch ct {
	case "image/jpeg", "image/png", "image/webp", "image/gif":
	default:
		writeError(w, http.StatusBadRequest, "that isn't a JPEG, PNG, WebP or GIF image")
		return
	}
	if err := s.Store.SetChosenImage(r.Context(), it.ID, store.ChosenImage{Data: data, ContentType: ct, Source: source}); err != nil {
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

// resetCover forgets a picked cover; the folder, the files and online
// lookups fill it in again, with a scan of its library.
func (s *Server) resetCover(w http.ResponseWriter, r *http.Request) {
	it, ok := s.pickableCover(w, r)
	if !ok {
		return
	}
	if err := s.Store.ClearChosenImage(r.Context(), it.ID); err != nil {
		s.serverError(w, err)
		return
	}
	if lib, err := s.Store.Library(r.Context(), it.LibraryID); err == nil && s.Scanner != nil {
		go s.Scanner.Scan(s.BackgroundCtx, lib)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) downloadCover(ctx context.Context, u string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Matinee (https://github.com/yeixio/matinee)")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", res.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, maxCoverBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxCoverBytes {
		return nil, errors.New("too big (20 MB at most)")
	}
	return data, nil
}

// chosenCover loads a picked cover for the image cache.
func (s *Server) chosenCover(ctx context.Context, src string) ([]byte, error) {
	id, ok := store.ChosenID(src)
	if !ok {
		return nil, store.ErrNotFound
	}
	img, err := s.Store.ChosenImage(ctx, id)
	return img.Data, err
}
