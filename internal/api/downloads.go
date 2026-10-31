package api

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/yeixio/matinee/internal/auth"
	"github.com/yeixio/matinee/internal/download"
	"github.com/yeixio/matinee/internal/store"
	"github.com/yeixio/matinee/internal/subtitle"
)

// ---- downloads for offline playback ----

type downloadView struct {
	store.Download
	URL string `json:"url"` // where the file is fetched from, once ready
}

func viewDownload(d store.Download) downloadView {
	return downloadView{d, "/api/v1/downloads/" + d.ID + "/file"}
}

func currentDevice(r *http.Request) string { return store.SessionID(auth.Token(r)) }

func (s *Server) downloadQualities(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, download.Qualities)
}

// BurnStream finds the embedded stream of a picture subtitle track, for the
// download converter.
func (s *Server) BurnStream(ctx context.Context, it store.Item, track string) (int, bool) {
	t, ok := subtitle.Find(s.subtitleTracks(ctx, it), track)
	if !ok || !t.BurnIn {
		return 0, false
	}
	return t.Stream, true
}

func (s *Server) createDownload(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ItemID   int64  `json:"itemId"`
		Quality  string `json:"quality"`
		Subtitle string `json:"subtitle"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if req.Quality == "" {
		req.Quality = store.QualityOriginal
	}
	q, ok := download.QualityByName(req.Quality)
	if !ok {
		writeError(w, http.StatusBadRequest, "unknown quality")
		return
	}
	u := currentUser(r)
	it, err := s.Store.Item(r.Context(), u.ID, req.ItemID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "item not found")
		return
	} else if err != nil {
		s.serverError(w, err)
		return
	}
	if !it.Playable {
		writeError(w, http.StatusBadRequest, "not a playable item")
		return
	}
	if req.Subtitle != "" {
		// Text subtitles are fetched separately as WebVTT and drawn by the
		// player; only picture subtitles are drawn into a converted video.
		if _, burn := s.BurnStream(r.Context(), it, req.Subtitle); !burn {
			req.Subtitle = ""
		} else if q.Name == store.QualityOriginal {
			writeError(w, http.StatusBadRequest, "picture subtitles need a converted quality, not the original file")
			return
		}
	}
	d := store.Download{UserID: u.ID, DeviceID: currentDevice(r), ItemID: it.ID, Quality: q.Name, Subtitle: req.Subtitle}
	s.Downloads.Plan(&d, it, q)
	d, err = s.Store.PutDownload(r.Context(), d)
	if err != nil {
		s.serverError(w, err)
		return
	}
	if d.State == store.DownloadQueued {
		s.Downloads.Wake()
	}
	writeJSON(w, http.StatusOK, viewDownload(d))
}

// listDownloads lists this device's downloads; ?device=all lists all of the
// user's devices, and ?user=all (admins) everyone's.
func (s *Server) listDownloads(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	var list []store.Download
	var err error
	switch {
	case r.URL.Query().Get("user") == "all":
		if !u.IsAdmin {
			writeError(w, http.StatusForbidden, "admins only")
			return
		}
		list, err = s.Store.AllDownloads(r.Context())
	case r.URL.Query().Get("device") == "all":
		list, err = s.Store.Downloads(r.Context(), u.ID, "")
	default:
		list, err = s.Store.Downloads(r.Context(), u.ID, currentDevice(r))
	}
	if err != nil {
		s.serverError(w, err)
		return
	}
	out := make([]downloadView, len(list))
	for i, d := range list {
		out[i] = viewDownload(d)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) loadDownload(w http.ResponseWriter, r *http.Request) (store.Download, bool) {
	d, err := s.Store.UserDownload(r.Context(), currentUser(r).ID, r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "download not found")
		return d, false
	}
	if err != nil {
		s.serverError(w, err)
		return d, false
	}
	return d, true
}

func (s *Server) getDownload(w http.ResponseWriter, r *http.Request) {
	if d, ok := s.loadDownload(w, r); ok {
		writeJSON(w, http.StatusOK, viewDownload(d))
	}
}

// deleteDownload removes a download. Admins can remove anyone's, to revoke it.
func (s *Server) deleteDownload(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	owner := u.ID
	if u.IsAdmin {
		owner = 0
	}
	d, err := s.Store.DeleteDownload(r.Context(), owner, r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "download not found")
		return
	}
	if err != nil {
		s.serverError(w, err)
		return
	}
	s.Downloads.Forget(r.Context(), d)
	w.WriteHeader(http.StatusNoContent)
}

// finishDownload is the device saying it has the whole file and its checksum
// matched, so the server may clean up its converted copy later.
func (s *Server) finishDownload(w http.ResponseWriter, r *http.Request) {
	err := s.Store.FinishDownload(r.Context(), currentUser(r).ID, r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusConflict, "download isn't ready")
		return
	}
	if err != nil {
		s.serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// downloadFile serves a prepared file to the device it belongs to, with
// range requests for resuming and a strong ETag (the SHA-256) so a resumed
// transfer never stitches together two different files.
func (s *Server) downloadFile(w http.ResponseWriter, r *http.Request) {
	d, ok := s.loadDownload(w, r)
	if !ok {
		return
	}
	if d.DeviceID != currentDevice(r) {
		writeError(w, http.StatusForbidden, "this download belongs to another device")
		return
	}
	if d.State != store.DownloadReady {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "download isn't ready", "state": d.State})
		return
	}
	f, err := os.Open(d.File)
	var info os.FileInfo
	if err == nil {
		info, err = f.Stat()
	}
	changed := err != nil || info.Size() != d.Size
	if !changed && !s.Downloads.Converted(d) {
		changed = info.ModTime().Unix() != d.SourceMtime
	}
	if changed {
		// Gone or replaced since it was prepared: prepare it again rather than
		// hand out something that doesn't match its checksum.
		if f != nil {
			f.Close()
		}
		if err := s.requeue(r.Context(), d); err != nil {
			s.serverError(w, err)
			return
		}
		writeJSON(w, http.StatusConflict, map[string]any{"error": "the file changed on the server and is being prepared again", "state": store.DownloadQueued})
		return
	}
	defer f.Close()

	sum, _ := hex.DecodeString(d.SHA256)
	h := w.Header()
	h.Set("Content-Type", d.MimeType)
	h.Set("ETag", `"`+d.SHA256+`"`)
	h.Set("Repr-Digest", "sha-256=:"+base64.StdEncoding.EncodeToString(sum)+":")
	h.Set("Cache-Control", "private, no-transform")
	if it, err := s.Store.Item(r.Context(), 0, d.ItemID); err == nil {
		h.Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", fileName(it.Title, d.File)))
	}
	http.ServeContent(w, r, "", info.ModTime(), f)
}

// requeue prepares a download again from the item as it is now.
func (s *Server) requeue(ctx context.Context, d store.Download) error {
	it, err := s.Store.Item(ctx, 0, d.ItemID)
	if err != nil {
		return err
	}
	q, _ := download.QualityByName(d.Quality)
	s.Downloads.Plan(&d, it, q)
	if err := s.Store.RequeueDownload(ctx, d); err != nil {
		return err
	}
	s.Downloads.Wake()
	return nil
}

func fileName(title, file string) string {
	clean := strings.Map(func(r rune) rune {
		if strings.ContainsRune(`/\:*?"<>|`, r) || r < 32 {
			return -1
		}
		return r
	}, title)
	if clean == "" {
		clean = "download"
	}
	return clean + filepath.Ext(file)
}

// ---- progress made offline ----

// maxSyncEvents bounds one sync request; a device sends more in batches.
const maxSyncEvents = 1000

// syncProgress takes watch progress a device recorded while offline. Each
// change is applied unless the item's progress changed more recently
// elsewhere, so the latest change wins. A place in an audiobook, or in one
// of its parts, is kept on the book; a podcast episode finishes in its last
// minute, the way it does when played live.
func (s *Server) syncProgress(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Events []store.ProgressEvent `json:"events"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if len(req.Events) > maxSyncEvents {
		writeError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("send at most %d events at a time", maxSyncEvents))
		return
	}
	u := currentUser(r)
	sort.SliceStable(req.Events, func(i, j int) bool { return req.Events[i].At < req.Events[j].At })
	applied, skipped := 0, 0
	for _, e := range req.Events {
		it, err := s.Store.Item(r.Context(), u.ID, e.ItemID)
		book := err == nil && (it.Kind == "book" || it.Kind == "part")
		if errors.Is(err, store.ErrNotFound) || err == nil && !it.Playable && !book {
			skipped++ // removed from the library since
			continue
		}
		if err != nil {
			s.serverError(w, err)
			return
		}
		// A device clock running ahead mustn't make its changes win forever.
		if now := time.Now().Unix(); e.At > now || e.At <= 0 {
			e.At = now
		}
		if book {
			// A place in a book is kept on the book, on its whole clock.
			ok, err := s.syncBookProgress(r.Context(), u.ID, it, e)
			if err != nil {
				s.serverError(w, err)
				return
			}
			if ok {
				applied++
			} else {
				skipped++
			}
			continue
		}
		if it.Kind == "podcastEpisode" {
			// The last minute of an episode finishes it, as it does live.
			if it.DurationMs != nil && *it.DurationMs > 0 && e.PositionMs >= *it.DurationMs-podcastFinishedMs {
				e.Played = true
			}
			if e.Played {
				e.PositionMs = 0
			}
		} else if it.DurationMs != nil && *it.DurationMs > 0 && e.PositionMs > *it.DurationMs*92/100 {
			e.Played, e.PositionMs = true, 0
		}
		ok, err := s.Store.MergeProgress(r.Context(), u.ID, e)
		if err == nil && ok {
			err = s.watched(r.Context(), u.ID, it, e.Played)
		}
		if err != nil {
			s.serverError(w, err)
			return
		}
		if ok {
			applied++
		} else {
			skipped++
		}
	}
	writeJSON(w, http.StatusOK, map[string]int{"applied": applied, "skipped": skipped})
}
