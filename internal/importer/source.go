// Package importer brings watch history, collections and custom posters
// over from another media server's data folder, so switching to Matinee
// doesn't mean starting again. The media files stay where they are: what
// the other server knew about a file is matched to Matinee's item for the
// same file, by path, after the admin's path changes (the two servers may
// see the same share under different folders).
//
// The format read here is the library database (SQLite) and metadata
// bundles that server keeps in its data folder.
package importer

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// dbName is the library database's file name in the other server's data folder.
const dbName = "com.plexapp.plugins.library.db"

// ErrNoDatabase means a folder has no library database in it, nor in the
// folders a few levels down.
var ErrNoDatabase = errors.New("no media server data found there")

// Location is where an import reads from.
type Location struct {
	DB string // the library database
	// DataDir is the data folder holding Metadata/ (custom posters); empty
	// when only the database is at hand, such as one uploaded on its own.
	DataDir string
}

// Locate finds the library database for a path an admin gave: the data
// folder itself, a folder above it, the Databases folder, or the database
// file.
func Locate(path string) (Location, error) {
	path = filepath.Clean(path)
	info, err := os.Stat(path)
	if err != nil {
		return Location{}, ErrNoDatabase
	}
	if !info.IsDir() {
		if !isDB(path) {
			return Location{}, ErrNoDatabase
		}
		return fromDB(path), nil
	}
	// The usual places first, then a short walk for anything else, such as
	// a backup copied somewhere with its folders around it.
	for _, rel := range []string{
		filepath.Join("Plug-in Support", "Databases", dbName),
		filepath.Join("Databases", dbName),
		dbName,
		filepath.Join("Library", "Application Support", "Plex Media Server", "Plug-in Support", "Databases", dbName),
	} {
		if p := filepath.Join(path, rel); isDB(p) {
			return fromDB(p), nil
		}
	}
	var found string
	root := strings.Count(path, string(filepath.Separator))
	filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err != nil || found != "" {
			return filepath.SkipDir
		}
		if d.IsDir() {
			if strings.Count(p, string(filepath.Separator))-root >= 6 || d.Name() == "Metadata" || d.Name() == "Cache" || d.Name() == "Media" {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() == dbName {
			found = p
			return filepath.SkipAll
		}
		return nil
	})
	if found == "" {
		return Location{}, ErrNoDatabase
	}
	return fromDB(found), nil
}

func isDB(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.Mode().IsRegular() && info.Size() > 0
}

// fromDB works out the data folder from the database's place in it.
func fromDB(db string) Location {
	loc := Location{DB: db}
	dir := filepath.Dir(db) // Databases
	if filepath.Base(dir) == "Databases" && filepath.Base(filepath.Dir(dir)) == "Plug-in Support" {
		data := filepath.Dir(filepath.Dir(dir))
		if info, err := os.Stat(filepath.Join(data, "Metadata")); err == nil && info.IsDir() {
			loc.DataDir = data
		}
	}
	return loc
}

// Find lists data folders found where that server keeps them by default on
// this computer, for an admin to pick from.
func Find() []string {
	var candidates []string
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "darwin":
		if home != "" {
			candidates = append(candidates, filepath.Join(home, "Library", "Application Support", "Plex Media Server"))
		}
	case "windows":
		if d := os.Getenv("LOCALAPPDATA"); d != "" {
			candidates = append(candidates, filepath.Join(d, "Plex Media Server"))
		}
	default:
		candidates = append(candidates,
			"/var/lib/plexmediaserver/Library/Application Support/Plex Media Server",
			"/config/Library/Application Support/Plex Media Server",
			"/var/snap/plexmediaserver/common/Library/Application Support/Plex Media Server",
		)
		if home != "" {
			candidates = append(candidates, filepath.Join(home, ".local", "share", "plexmediaserver", "Library", "Application Support", "Plex Media Server"))
		}
	}
	found := []string{}
	for _, c := range candidates {
		if isDB(filepath.Join(c, "Plug-in Support", "Databases", dbName)) {
			found = append(found, c)
		}
	}
	return found
}
