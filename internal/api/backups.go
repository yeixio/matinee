package api

import (
	"errors"
	"net/http"
	"os"

	"github.com/yeixio/matinee/internal/backup"
)

// Database backups (see internal/backup), under Settings → Server.

type backupsView struct {
	// Supported is false for PostgreSQL, which is backed up with pg_dump.
	Supported bool            `json:"supported"`
	Database  string          `json:"database"` // sqlite or postgres
	Dir       string          `json:"dir,omitempty"`
	Settings  backup.Settings `json:"settings"`
	Backups   []backup.Backup `json:"backups"`
	// Restore is the backup that replaces the database at the next start.
	Restore   string `json:"restore,omitempty"`
	LastError string `json:"lastError,omitempty"`
}

func (s *Server) backupsView(r *http.Request) (backupsView, error) {
	v := backupsView{Database: "postgres", Backups: []backup.Backup{}, Settings: backup.DefaultSettings}
	if s.Store.SQLite() {
		v.Database = "sqlite"
	}
	m := s.Backups
	if !m.Supported() {
		return v, nil
	}
	list, err := m.List()
	if err != nil {
		return v, err
	}
	v.Supported, v.Dir, v.Backups = true, m.Dir, list
	v.Settings = m.Settings(r.Context())
	v.Restore, v.LastError = m.PendingRestore(), m.LastError()
	return v, nil
}

func (s *Server) writeBackups(w http.ResponseWriter, r *http.Request) {
	v, err := s.backupsView(r)
	if err != nil {
		s.serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) getBackups(w http.ResponseWriter, r *http.Request) { s.writeBackups(w, r) }

// needBackups answers for a server that doesn't make its own backups.
func (s *Server) needBackups(w http.ResponseWriter) bool {
	if !s.Backups.Supported() {
		writeError(w, http.StatusConflict, "this server's database is PostgreSQL; back it up with pg_dump")
		return false
	}
	return true
}

func (s *Server) setBackupSettings(w http.ResponseWriter, r *http.Request) {
	if !s.needBackups(w) {
		return
	}
	var in backup.Settings
	if err := readJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if _, err := s.Backups.SaveSettings(r.Context(), in); err != nil {
		s.serverError(w, err)
		return
	}
	s.writeBackups(w, r)
}

func (s *Server) makeBackup(w http.ResponseWriter, r *http.Request) {
	if !s.needBackups(w) {
		return
	}
	if _, err := s.Backups.Make(r.Context(), backup.ReasonManual); err != nil {
		writeError(w, http.StatusInternalServerError, "couldn't back up the database: "+err.Error())
		return
	}
	s.writeBackups(w, r)
}

func (s *Server) downloadBackup(w http.ResponseWriter, r *http.Request) {
	if !s.needBackups(w) {
		return
	}
	name := r.PathValue("name")
	p, err := s.Backups.Path(name)
	if err != nil {
		writeError(w, http.StatusNotFound, "no such backup")
		return
	}
	w.Header().Set("Content-Type", "application/vnd.sqlite3")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	http.ServeFile(w, r, p)
}

func (s *Server) deleteBackup(w http.ResponseWriter, r *http.Request) {
	if !s.needBackups(w) {
		return
	}
	if err := s.Backups.Delete(r.PathValue("name")); errors.Is(err, os.ErrNotExist) {
		writeError(w, http.StatusNotFound, "no such backup")
		return
	} else if err != nil {
		s.serverError(w, err)
		return
	}
	s.writeBackups(w, r)
}

// restoreBackup puts a backup in place of the database at the next start.
func (s *Server) restoreBackup(w http.ResponseWriter, r *http.Request) {
	if !s.needBackups(w) {
		return
	}
	if err := s.Backups.Restore(r.Context(), r.PathValue("name")); errors.Is(err, os.ErrNotExist) {
		writeError(w, http.StatusNotFound, "no such backup")
		return
	} else if err != nil {
		writeError(w, http.StatusBadRequest, "can't restore this backup: "+err.Error())
		return
	}
	s.Log.Warn("a database restore is waiting for the next start", "backup", r.PathValue("name"), "by", currentUser(r).Username)
	s.writeBackups(w, r)
}

func (s *Server) cancelRestore(w http.ResponseWriter, r *http.Request) {
	if !s.needBackups(w) {
		return
	}
	if err := s.Backups.CancelRestore(); err != nil {
		s.serverError(w, err)
		return
	}
	s.writeBackups(w, r)
}
