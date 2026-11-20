package api

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/yeixio/matinee/internal/store"
)

// libraryFolders checks the folders asked for a library (id, 0 for a new
// one, which has the folders had): each new one must be a folder on the
// server, and none may be in another library already, since a file in two
// libraries would flip between them on every scan. A folder the library
// had stays even if it can't be read right now, as an unmounted share
// can't. It writes the error and reports false when they won't do.
func (s *Server) libraryFolders(w http.ResponseWriter, r *http.Request, id int64, had []string, asked []string) ([]string, bool) {
	libs, err := s.Store.Libraries(r.Context())
	if err != nil {
		s.serverError(w, err)
		return nil, false
	}
	var out []string
	for _, p := range asked {
		if strings.TrimSpace(p) == "" {
			continue
		}
		p = filepath.Clean(p)
		if slices.Contains(out, p) {
			continue
		}
		for _, o := range out {
			if inside(p, o) || inside(o, p) {
				writeErrorWith(w, http.StatusBadRequest, "{{inner}} is inside {{outer}}; keep just the outer folder",
					map[string]string{"inner": max(o, p), "outer": min(o, p)})
				return nil, false
			}
		}
		if !slices.Contains(had, p) {
			if info, err := os.Stat(p); err != nil || !info.IsDir() {
				writeError(w, http.StatusBadRequest, "folder not found: "+p)
				return nil, false
			}
		}
		if other, ok := store.FolderOwner(libs, id, p); ok {
			writeErrorWith(w, http.StatusConflict, "{{folder}} is already in the {{library}} library; to make them one library, combine them instead",
				map[string]string{"folder": p, "library": other.Name})
			return nil, false
		}
		out = append(out, p)
	}
	return out, true
}

// updateLibrary renames a library or changes its folders, then scans it
// again, so files in an added folder come in and files in a removed one go.
func (s *Server) updateLibrary(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	var req struct {
		Name  *string  `json:"name"`
		Paths []string `json:"paths"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	lib, err := s.Store.Library(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "library not found")
		return
	} else if err != nil {
		s.serverError(w, err)
		return
	}
	name := lib.Name
	if req.Name != nil {
		if name = strings.TrimSpace(*req.Name); name == "" {
			writeError(w, http.StatusBadRequest, "name is required")
			return
		}
	}
	paths := lib.Paths
	if req.Paths != nil && lib.Kind != "podcasts" {
		if paths, ok = s.libraryFolders(w, r, id, lib.Paths, req.Paths); !ok {
			return
		}
		if len(paths) == 0 {
			writeError(w, http.StatusBadRequest, "a library needs at least one folder")
			return
		}
	}
	changed := !slices.Equal(sorted(paths), sorted(lib.Paths))
	if changed && !s.stopScanAndWait(id) {
		writeError(w, http.StatusConflict, "a scan is still running; try again in a moment")
		return
	}
	updated, err := s.Store.UpdateLibrary(r.Context(), id, name, paths)
	if err != nil {
		s.serverError(w, err)
		return
	}
	if changed {
		if s.Watcher != nil {
			s.Watcher.Reload()
		}
		s.Scanner.Start(s.BackgroundCtx, updated)
	}
	writeJSON(w, http.StatusOK, s.libraryView(updated, s.Scanner.Running(id)))
}

// mergeLibrary makes another library of the same type part of this one:
// its folders and titles join this library, and the other goes. A show
// split across the two becomes one show. What people watched, how far they
// got, their watch lists and ratings stay.
func (s *Server) mergeLibrary(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	var req struct {
		From int64 `json:"from"`
	}
	if err := readJSON(r, &req); err != nil || req.From == 0 {
		writeError(w, http.StatusBadRequest, "pick the library to combine with")
		return
	}
	if req.From == id {
		writeError(w, http.StatusBadRequest, "a library can't be combined with itself")
		return
	}
	if !s.stopScanAndWait(id) || !s.stopScanAndWait(req.From) {
		writeError(w, http.StatusConflict, "a scan is still running; try again in a moment")
		return
	}
	switch err := s.Store.MergeLibraries(r.Context(), id, req.From); {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "library not found")
		return
	case errors.Is(err, store.ErrKindMismatch):
		writeError(w, http.StatusBadRequest, "only libraries of the same type can be combined")
		return
	case err != nil:
		s.serverError(w, err)
		return
	}
	lib, err := s.Store.Library(r.Context(), id)
	if err != nil {
		s.serverError(w, err)
		return
	}
	if s.Watcher != nil {
		s.Watcher.Reload()
	}
	// The scan files everything as one library: versions of a movie that
	// was in both, and empty seasons left behind.
	s.Scanner.Start(s.BackgroundCtx, lib)
	writeJSON(w, http.StatusOK, s.libraryView(lib, s.Scanner.Running(id)))
}

// stopScanAndWait stops a library's scan, if one is running, and waits a
// little for it to end. It reports false if it's still running.
func (s *Server) stopScanAndWait(id int64) bool {
	if !s.Scanner.Stop(id) {
		return true
	}
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		if !s.Scanner.Running(id) {
			return true
		}
	}
	return false
}

func sorted(paths []string) []string {
	out := slices.Clone(paths)
	slices.Sort(out)
	return out
}

// inside reports whether path is in dir (not dir itself).
func inside(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
