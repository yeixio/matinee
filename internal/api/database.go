package api

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/yeixio/matinee/internal/store"
)

// Settings → Server health → Database: which database Matinee uses, and
// moving it to another one (see internal/store/move.go). The move itself
// happens the next time the server starts, before anything else uses the
// database (cmd/matinee/database.go), so nothing written meanwhile is left
// behind.

// DatabaseInfo is where the server's database is.
type DatabaseInfo struct {
	// DSN is the database in use; Configured is what the server's settings
	// say, an older database when it was moved and they weren't changed.
	DSN, Configured string
	// DataDir is the data folder, where a SQLite file goes by default.
	DataDir string
	// Kubernetes: the data folder is emptied whenever the pod is replaced,
	// so a SQLite file can't live there.
	Kubernetes bool
}

type databaseView struct {
	Kind  string `json:"kind"` // sqlite or postgres
	Where string `json:"where"`
	// Configured is set when the server's settings still name the database
	// it was moved from.
	Configured string           `json:"configured,omitempty"`
	MovedFrom  *store.MovedFrom `json:"movedFrom,omitempty"`
	// Pending is a move waiting for the next start.
	Pending    string `json:"pending,omitempty"`
	LastError  string `json:"lastError,omitempty"`
	CanRestart bool   `json:"canRestart"`
	// SuggestedFile is where a SQLite file could go; "" when a SQLite file
	// isn't a choice (on Kubernetes) or the usual place is taken.
	SuggestedFile string `json:"suggestedFile,omitempty"`
	// SQLiteOK is false on Kubernetes.
	SQLiteOK bool `json:"sqliteOk"`
}

func (s *Server) databaseView(ctx context.Context) databaseView {
	d := s.Database
	v := databaseView{Kind: "sqlite", Where: store.Describe(d.DSN), CanRestart: s.canRestart(), SQLiteOK: !d.Kubernetes}
	if store.IsPostgres(d.DSN) {
		v.Kind = "postgres"
	}
	if d.Configured != "" && d.Configured != d.DSN {
		v.Configured = store.Describe(d.Configured)
	}
	if b, _ := s.Store.Setting(ctx, store.SettingMovedFrom); b != "" {
		var m store.MovedFrom
		if json.Unmarshal([]byte(b), &m) == nil {
			v.MovedFrom = &m
		}
	}
	if p, _ := s.Store.Setting(ctx, store.SettingMovePending); p != "" {
		v.Pending = store.Describe(p)
	}
	v.LastError, _ = s.Store.Setting(ctx, store.SettingMoveError)
	if v.SQLiteOK && d.DataDir != "" {
		if dir, err := filepath.Abs(d.DataDir); err == nil {
			f := filepath.Join(dir, "matinee.db")
			if _, err := os.Stat(f); os.IsNotExist(err) {
				v.SuggestedFile = f
			}
		}
	}
	return v
}

func (s *Server) getDatabase(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.databaseView(r.Context()))
}

type moveRequest struct {
	To string `json:"to"`
}

// moveTarget reads and checks where an admin wants the database moved.
func (s *Server) moveTarget(w http.ResponseWriter, r *http.Request) (string, bool) {
	var in moveRequest
	if err := readJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return "", false
	}
	to := strings.TrimSpace(in.To)
	if to == "" {
		writeError(w, http.StatusBadRequest, "say where to move the database: a PostgreSQL URL or a SQLite file's full path")
		return "", false
	}
	if !store.IsPostgres(to) && s.Database.Kubernetes {
		writeError(w, http.StatusBadRequest, "on Kubernetes the data folder is emptied whenever Matinee's pod is replaced, so the database can't be a SQLite file there; move it to a PostgreSQL database")
		return "", false
	}
	if err := s.Store.CheckMoveTarget(r.Context(), to); err != nil {
		writeError(w, http.StatusBadRequest, "can't move the database there: "+err.Error())
		return "", false
	}
	return to, true
}

// checkDatabaseMove says whether a move to a database would work, without
// moving anything.
func (s *Server) checkDatabaseMove(w http.ResponseWriter, r *http.Request) {
	to, ok := s.moveTarget(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"where": store.Describe(to)})
}

type moveAnswer struct {
	databaseView
	// Restarting: the server is restarting to move the database now.
	Restarting bool   `json:"restarting"`
	BootID     string `json:"bootId,omitempty"`
}

// moveDatabase asks for the database to be moved at the next start, and
// restarts the server to do it when it can.
func (s *Server) moveDatabase(w http.ResponseWriter, r *http.Request) {
	to, ok := s.moveTarget(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	s.Store.DeleteSetting(ctx, store.SettingMoveError)
	if err := s.Store.SetSetting(ctx, store.SettingMovePending, to); err != nil {
		s.serverError(w, err)
		return
	}
	s.Log.Warn("a database move is waiting for the next start", "to", store.Describe(to), "by", currentUser(r).Username)
	out := moveAnswer{databaseView: s.databaseView(ctx)}
	if s.canRestart() && s.restarting.CompareAndSwap(false, true) {
		s.Log.Info("restarting to move the database", "by", currentUser(r).Username)
		out.Restarting, out.BootID = true, bootID
		go s.Restart()
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) cancelDatabaseMove(w http.ResponseWriter, r *http.Request) {
	if err := s.Store.DeleteSetting(r.Context(), store.SettingMovePending); err != nil {
		s.serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.databaseView(r.Context()))
}
