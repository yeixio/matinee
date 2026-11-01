package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/yeixio/matinee/internal/importer"
)

// Admins can bring watch history, collections and custom posters over from
// another media server's data folder (see the importer package): find or
// pick the folder (or upload its database), check what matches and fix
// paths, say whose history goes to whom, then run it in the background.

// importState is the import being set up or run. One at a time.
type importState struct {
	mu sync.Mutex
	// The last snapshot read, so checking and running don't read it twice.
	key  string
	snap *importer.Snapshot
	at   time.Time
	// The run, if any.
	running bool
	done    int
	total   int
	result  *importer.Result
	err     string
}

// uploadedDB is where an uploaded database is kept until the next one.
func uploadedDB() string { return filepath.Join(os.TempDir(), "matinee-import", "library.db") }

// importSource is where an import reads from: a path on the server, or
// empty for the uploaded database.
func importSource(path string) (importer.Location, error) {
	if strings.TrimSpace(path) == "" {
		db := uploadedDB()
		if _, err := os.Stat(db); err != nil {
			return importer.Location{}, importer.ErrNoDatabase
		}
		return importer.Location{DB: db}, nil
	}
	return importer.Locate(path)
}

// snapshot reads the source, or reuses the last read if the database hasn't
// changed since.
func (s *Server) importSnapshot(ctx context.Context, path string) (*importer.Snapshot, error) {
	loc, err := importSource(path)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(loc.DB)
	if err != nil {
		return nil, importer.ErrNoDatabase
	}
	key := loc.DB + "|" + strconv.FormatInt(info.ModTime().UnixNano(), 10) + "|" + strconv.FormatInt(info.Size(), 10)
	if wal, err := os.Stat(loc.DB + "-wal"); err == nil {
		key += "|" + strconv.FormatInt(wal.ModTime().UnixNano(), 10)
	}
	st := &s.imports
	st.mu.Lock()
	if st.key == key && st.snap != nil && time.Since(st.at) < 30*time.Minute {
		snap := st.snap
		st.mu.Unlock()
		return snap, nil
	}
	st.mu.Unlock()
	snap, err := importer.Read(ctx, loc)
	if err != nil {
		return nil, err
	}
	st.mu.Lock()
	st.key, st.snap, st.at = key, snap, time.Now()
	st.mu.Unlock()
	return snap, nil
}

func (s *Server) importError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, importer.ErrNoDatabase):
		writeError(w, http.StatusBadRequest, "no media server data found there")
	case errors.Is(err, importer.ErrUnreadable):
		s.Log.Warn("read an import", "err", err)
		writeError(w, http.StatusBadRequest, "that isn't a media server database Matinee can read")
	default:
		s.serverError(w, err)
	}
}

// findImports lists other servers' data folders found on this computer.
func (s *Server) findImports(w http.ResponseWriter, r *http.Request) {
	_, err := os.Stat(uploadedDB())
	writeJSON(w, http.StatusOK, map[string]any{"found": importer.Find(), "uploaded": err == nil})
}

// maxImportUpload is the largest database that can be uploaded.
const maxImportUpload = 8 << 30

// uploadImport takes the database itself as the request body, for a server
// that can't see the other one's folder.
func (s *Server) uploadImport(w http.ResponseWriter, r *http.Request) {
	dst := uploadedDB()
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		s.serverError(w, err)
		return
	}
	tmp := dst + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		s.serverError(w, err)
		return
	}
	_, err = io.Copy(f, http.MaxBytesReader(w, r.Body, maxImportUpload))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp)
		writeError(w, http.StatusBadRequest, "the upload didn't finish")
		return
	}
	os.Remove(dst + "-wal")
	if err := os.Rename(tmp, dst); err != nil {
		s.serverError(w, err)
		return
	}
	if _, err := s.importSnapshot(r.Context(), ""); err != nil {
		os.Remove(dst)
		s.importError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type importRequest struct {
	// Path is the other server's data folder (or its database) on this
	// server; empty uses the uploaded database.
	Path    string                `json:"path"`
	Changes []importer.PathChange `json:"changes"`
	// Accounts maps the other server's accounts (by id, as text) to
	// Matinee's accounts and profiles. Only for running.
	Accounts    map[string]int64 `json:"accounts,omitempty"`
	Watched     bool             `json:"watched"`
	Collections bool             `json:"collections"`
	Posters     bool             `json:"posters"`
	Ratings     bool             `json:"ratings"`
}

type importViewer struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	ProfileOf string `json:"profileOf,omitempty"` // the account a profile belongs to
}

type importAccount struct {
	importer.AccountSummary
	// Suggested is the Matinee account or profile it most likely is; 0 for none.
	Suggested int64 `json:"suggested"`
}

func (s *Server) importPlan(ctx context.Context, req importRequest) (*importer.Plan, error) {
	snap, err := s.importSnapshot(ctx, req.Path)
	if err != nil {
		return nil, err
	}
	files, err := s.Store.FileRefs(ctx)
	if err != nil {
		return nil, err
	}
	var changes []importer.PathChange
	for _, c := range req.Changes {
		if strings.TrimSpace(c.From) != "" {
			changes = append(changes, importer.PathChange{From: strings.TrimSpace(c.From), To: strings.TrimSpace(c.To)})
		}
	}
	return importer.NewPlan(snap, files, changes), nil
}

// previewImport says what an import would bring over: whose history, how
// many files match, and path changes that would match more.
func (s *Server) previewImport(w http.ResponseWriter, r *http.Request) {
	var req importRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad request")
		return
	}
	ctx := r.Context()
	plan, err := s.importPlan(ctx, req)
	if err != nil {
		s.importError(w, err)
		return
	}
	viewers, err := s.Store.Viewers(ctx)
	if err != nil {
		s.serverError(w, err)
		return
	}
	names := map[int64]string{}
	out := []importViewer{}
	for _, v := range viewers {
		names[v.ID] = v.Username
	}
	for _, v := range viewers {
		iv := importViewer{ID: v.ID, Name: v.Username}
		if v.AccountID != 0 {
			iv.ProfileOf = names[v.AccountID]
			if iv.ProfileOf == "" {
				iv.ProfileOf = "Home"
			}
		}
		out = append(out, iv)
	}
	me := currentUser(r)
	accounts := []importAccount{}
	for i, a := range plan.Accounts() {
		ia := importAccount{AccountSummary: a}
		for _, v := range viewers {
			if strings.EqualFold(strings.TrimSpace(v.Username), strings.TrimSpace(a.Name)) {
				ia.Suggested = v.ID
				break
			}
		}
		// The first account is the server's owner: most likely whoever is
		// setting up this one.
		if ia.Suggested == 0 && i == 0 {
			ia.Suggested = me.Account()
		}
		accounts = append(accounts, ia)
	}
	loc := plan.Location()
	writeJSON(w, http.StatusOK, map[string]any{
		"database":    loc.DB,
		"canPosters":  loc.DataDir != "",
		"accounts":    accounts,
		"viewers":     out,
		"files":       plan.Files,
		"matched":     plan.Matched,
		"unmatched":   nonNil(plan.Unmatched),
		"suggested":   nonNil(plan.Suggested),
		"collections": plan.Collections(),
		"posters":     plan.Posters(),
	})
}

func nonNil[T any](v []T) []T {
	if v == nil {
		return []T{}
	}
	return v
}

// runImport starts an import in the background; importStatus follows it.
func (s *Server) runImport(w http.ResponseWriter, r *http.Request) {
	var req importRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad request")
		return
	}
	accounts := map[int64]int64{}
	for k, v := range req.Accounts {
		id, err := strconv.ParseInt(k, 10, 64)
		if err != nil || v <= 0 {
			continue
		}
		if _, err := s.Store.Viewer(r.Context(), v); err != nil {
			writeError(w, http.StatusBadRequest, "bad user")
			return
		}
		accounts[id] = v
	}
	if !req.Collections && !req.Posters && ((!req.Watched && !req.Ratings) || len(accounts) == 0) {
		writeError(w, http.StatusBadRequest, "nothing to import")
		return
	}
	plan, err := s.importPlan(r.Context(), req)
	if err != nil {
		s.importError(w, err)
		return
	}
	st := &s.imports
	st.mu.Lock()
	if st.running {
		st.mu.Unlock()
		writeError(w, http.StatusConflict, "an import is already running")
		return
	}
	st.running, st.done, st.total, st.result, st.err = true, 0, 0, nil, ""
	st.mu.Unlock()
	go func() {
		ctx := s.BackgroundCtx
		if ctx == nil {
			ctx = context.Background()
		}
		res, err := plan.Apply(ctx, s.Store, importer.Options{
			Accounts: accounts, Watched: req.Watched, Collections: req.Collections, Posters: req.Posters, Ratings: req.Ratings,
			Step: func(done, total int) {
				st.mu.Lock()
				st.done, st.total = done, total
				st.mu.Unlock()
			},
		})
		st.mu.Lock()
		defer st.mu.Unlock()
		st.running = false
		if err != nil {
			s.Log.Error("import", "err", err)
			st.err = "stopped" // the web player says what that means
			return
		}
		s.Log.Info("import finished", "watched", res.Watched, "inProgress", res.InProgress, "kept", res.Kept,
			"collections", res.Collections, "posters", res.Posters, "ratings", res.Ratings)
		st.result = &res
	}()
	w.WriteHeader(http.StatusAccepted)
}

// importStatus says whether an import is running, how far along it is,
// and what the last one did.
func (s *Server) importStatus(w http.ResponseWriter, r *http.Request) {
	st := &s.imports
	st.mu.Lock()
	defer st.mu.Unlock()
	out := map[string]any{"running": st.running, "done": st.done, "total": st.total}
	if st.result != nil {
		out["result"] = st.result
	}
	if st.err != "" {
		out["error"] = st.err
	}
	writeJSON(w, http.StatusOK, out)
}
