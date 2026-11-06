package scan

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/yeixio/matinee/internal/nfo"
	"github.com/yeixio/matinee/internal/store"
)

// ReadNFOs applies the .nfo files next to a movie or TV library's files
// (see store.ApplyLocal): <file>.nfo or movie.nfo for a movie,
// <file>.nfo for an episode, and tvshow.nfo in a show's folder. Files
// whose .nfo hasn't changed since it was last read are left alone, so
// this is cheap to run after every scan.
func ReadNFOs(ctx context.Context, st *store.Store, lib store.Library) error {
	files, err := st.LibraryFiles(ctx, lib.ID)
	if err != nil {
		return err
	}
	// Most folders have no .nfo in them; look in each once.
	hasNFO := map[string]bool{}
	dirHasNFO := func(dir string) bool {
		if has, ok := hasNFO[dir]; ok {
			return has
		}
		entries, _ := os.ReadDir(dir)
		has := false
		for _, e := range entries {
			if strings.EqualFold(filepath.Ext(e.Name()), ".nfo") {
				has = true
				break
			}
		}
		hasNFO[dir] = has
		return has
	}
	apply := func(id int64, was string, info nfo.Info, ok, pin bool) error {
		if !ok || info.Empty() {
			if was == "" {
				return nil
			}
			return st.ApplyLocal(ctx, id, nil)
		}
		if info.Hash == was {
			return nil
		}
		l := &store.Local{Title: info.Title, Year: info.Year, Overview: info.Plot, Genres: info.Genres,
			Rating: info.Rating, Poster: localArt(info.Poster), Backdrop: localArt(info.Backdrop), Hash: info.Hash}
		if pin {
			l.TMDBID = info.IDs.TMDB
		}
		return st.ApplyLocal(ctx, id, l)
	}
	shows := map[int64]string{} // show id → its folder
	for _, f := range files {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		root := rootOf(lib.Paths, f.Path)
		switch {
		case f.Kind == "movie" && f.PartOf == 0:
			dir := filepath.Dir(f.Path)
			var info nfo.Info
			ok := false
			if dirHasNFO(dir) {
				info, ok = nfo.ForMovie(f.Path, dir != root)
			}
			if err := apply(f.ID, f.NFOHash, info, ok, true); err != nil {
				return err
			}
		case f.Kind == "episode":
			var info nfo.Info
			ok := false
			if dirHasNFO(filepath.Dir(f.Path)) {
				info, ok = nfo.ForEpisode(f.Path)
			}
			// An episode file's ids are the episode's, not its show's.
			if err := apply(f.ID, f.NFOHash, info, ok, false); err != nil {
				return err
			}
			if show, _ := ShowFolders(root, f.Path); show != "" && f.ShowID != 0 {
				shows[f.ShowID] = show
			}
		}
	}
	for id, dir := range shows {
		was, err := st.LocalHash(ctx, id)
		if err != nil {
			return err
		}
		var info nfo.Info
		ok := false
		if dirHasNFO(dir) {
			info, ok = nfo.ForShow(dir)
		}
		// A show is found by its folder's name and year when its episodes
		// are scanned, so those stay the folder's.
		info.Title, info.Year = "", 0
		if err := apply(id, was, info, ok, true); err != nil {
			return err
		}
	}
	return nil
}

// localArt keeps an image an .nfo file names when it's a web address or
// a file that's there.
func localArt(p string) string {
	if p == "" || strings.Contains(p, "://") {
		return p
	}
	if info, err := os.Stat(p); err == nil && !info.IsDir() {
		return p
	}
	return ""
}
