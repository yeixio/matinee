package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/yeixio/matinee/internal/metadata"
	"github.com/yeixio/matinee/internal/probe"
	"github.com/yeixio/matinee/internal/scan"
	"github.com/yeixio/matinee/internal/store"
	"github.com/yeixio/matinee/internal/stream"
)

// Trailers come from two places: files next to the media (see
// scan/trailers.go), which play like the title itself, and TMDB's list of
// YouTube trailers, which an admin can turn off.

// settingRemoteTrailers is "false" when an admin turned off online trailers.
const settingRemoteTrailers = "remote_trailers"

// TrailerSource lists a title's trailers online. *metadata.TMDB is one.
type TrailerSource interface {
	Trailers(ctx context.Context, kind string, id int64, season int) ([]metadata.Video, error)
}

type trailerView struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Source string `json:"source"` // local or youtube
	// Local trailers play from /items/{id}/trailers/{trailer}/file or /hls,
	// and carry what players need to choose between the two.
	MimeType   string `json:"mimeType,omitempty"`
	Container  string `json:"container,omitempty"`
	VideoCodec string `json:"videoCodec,omitempty"`
	AudioCodec string `json:"audioCodec,omitempty"`
	DurationMs int64  `json:"durationMs,omitempty"`
	// YouTube trailers: the video id and its page.
	Key string `json:"key,omitempty"`
	URL string `json:"url,omitempty"`
}

type localTrailer struct {
	path  string
	probe probe.Result
}

func (s *Server) remoteTrailersOn(ctx context.Context) (bool, error) {
	v, err := s.Store.Setting(ctx, settingRemoteTrailers)
	return v != "false", err
}

func (s *Server) listTrailers(w http.ResponseWriter, r *http.Request) {
	it, ok := s.loadItem(w, r)
	if !ok {
		return
	}
	out := []trailerView{}
	for i, t := range s.localTrailers(r.Context(), it) {
		v := trailerView{
			ID: "local-" + strconv.Itoa(i), Name: scan.TrailerName(t.path), Source: "local",
			MimeType: stream.MimeType(t.path), Container: t.probe.Container, VideoCodec: t.probe.VideoCodec, AudioCodec: t.probe.AudioCodec, DurationMs: t.probe.DurationMs,
		}
		out = append(out, v)
	}
	on, err := s.remoteTrailersOn(r.Context())
	if err != nil {
		s.serverError(w, err)
		return
	}
	if on {
		for _, v := range s.remoteTrailers(r.Context(), it) {
			out = append(out, trailerView{
				ID: "youtube-" + v.Key, Name: v.Name, Source: "youtube", Key: v.Key,
				URL: "https://www.youtube.com/watch?v=" + v.Key,
			})
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// localTrailers finds a movie's, show's or season's trailer files. A season
// without its own borrows its show's.
func (s *Server) localTrailers(ctx context.Context, it store.Item) []localTrailer {
	var paths []string
	switch it.Kind {
	case "movie":
		if root := s.libraryRoot(ctx, it.LibraryID, it.Path); root != "" {
			paths = scan.MovieTrailers(root, it.Path)
		}
	case "show", "season":
		file, err := s.Store.FileUnder(ctx, it.ID)
		if err != nil {
			s.Log.Warn("find show folder", "item", it.ID, "err", err)
		}
		root := s.libraryRoot(ctx, it.LibraryID, file)
		if file == "" || root == "" {
			break
		}
		show, season := scan.ShowFolders(root, file)
		if it.Kind == "season" && season != "" {
			paths = scan.FolderTrailers(season)
		}
		if len(paths) == 0 && show != "" {
			paths = scan.FolderTrailers(show)
		}
	}
	out := []localTrailer{}
	for _, p := range paths {
		pr, err := s.probeTrailer(ctx, p)
		if err != nil || pr.VideoCodec == "" {
			continue
		}
		out = append(out, localTrailer{p, pr})
	}
	return out
}

// libraryRoot returns the library folder a file is under.
func (s *Server) libraryRoot(ctx context.Context, libraryID int64, path string) string {
	if path == "" {
		return ""
	}
	lib, err := s.Store.Library(ctx, libraryID)
	if err != nil {
		return ""
	}
	for _, root := range lib.Paths {
		if rel, err := filepath.Rel(root, path); err == nil && !strings.HasPrefix(rel, "..") {
			return root
		}
	}
	return ""
}

// probeTrailer reads a trailer file, remembering the answer until the file changes.
func (s *Server) probeTrailer(ctx context.Context, path string) (probe.Result, error) {
	info, err := os.Stat(path)
	if err != nil {
		return probe.Result{}, err
	}
	key := fmt.Sprintf("trailer-probe:%s:%d:%d", path, info.Size(), info.ModTime().UnixNano())
	return cached(&s.trailerCache, key, 24*time.Hour, func() (probe.Result, error) {
		return s.Scanner.Prober.Probe(ctx, path)
	})
}

// remoteTrailers asks TMDB for a title's trailers. A season without its own
// borrows its show's. Failures only mean no online trailers this time.
func (s *Server) remoteTrailers(ctx context.Context, it store.Item) []metadata.Video {
	if s.TrailerSource == nil {
		return nil
	}
	kind, id, season := it.Kind, it.TMDBID, 0
	if it.Kind == "season" {
		if it.ParentID == nil || it.IndexNumber == nil {
			return nil
		}
		show, err := s.Store.Item(ctx, 0, *it.ParentID)
		if err != nil {
			return nil
		}
		kind, id, season = "show", show.TMDBID, *it.IndexNumber
	}
	if id == nil || (kind != "movie" && kind != "show") {
		return nil
	}
	load := func(season int) []metadata.Video {
		key := fmt.Sprintf("trailers:%s:%d:%d", kind, *id, season)
		vids, err := cached(&s.trailerCache, key, 12*time.Hour, func() ([]metadata.Video, error) {
			return s.TrailerSource.Trailers(ctx, kind, *id, season)
		})
		if err != nil {
			s.Log.Warn("online trailers", "item", it.ID, "err", err)
		}
		return vids
	}
	vids := load(season)
	if len(vids) == 0 && season > 0 {
		vids = load(0)
	}
	// Three is plenty to choose from.
	return vids[:min(len(vids), 3)]
}

// trailerItem stands a local trailer in for its title, for the file and
// transcode handlers.
func (s *Server) trailerItem(w http.ResponseWriter, r *http.Request) (store.Item, bool) {
	it, ok := s.loadItem(w, r)
	if !ok {
		return it, false
	}
	n, err := strconv.Atoi(strings.TrimPrefix(r.PathValue("trailer"), "local-"))
	trailers := s.localTrailers(r.Context(), it)
	if err != nil || n < 0 || n >= len(trailers) {
		writeError(w, http.StatusNotFound, "no such trailer")
		return it, false
	}
	t := trailers[n]
	// Same id as the title: starting a trailer replaces the title's own
	// transcode for this viewer, and the other way round.
	return store.Item{
		ID: it.ID, LibraryID: it.LibraryID, Kind: "trailer", Title: it.Title, Path: t.path, Playable: true,
		VideoCodec: t.probe.VideoCodec, AudioCodec: t.probe.AudioCodec, Container: t.probe.Container,
	}, true
}

func (s *Server) trailerFile(w http.ResponseWriter, r *http.Request) {
	if t, ok := s.trailerItem(w, r); ok {
		stream.ServeDirect(w, r, t)
	}
}

func (s *Server) trailerHLS(w http.ResponseWriter, r *http.Request) {
	t, ok := s.trailerItem(w, r)
	if !ok {
		return
	}
	start, _ := strconv.ParseFloat(r.URL.Query().Get("start"), 64)
	id, err := s.Transcoder.Start(r.Context(), currentUser(r).ID, t, start, -1)
	if errors.Is(err, stream.ErrSuperseded) {
		writeError(w, http.StatusConflict, "replaced by a newer seek")
		return
	}
	if err != nil {
		s.Log.Error("transcode trailer", "item", t.ID, "err", err)
		writeError(w, http.StatusInternalServerError, "couldn't start playback for this trailer")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"url": "/api/v1/hls/" + id + "/index.m3u8", "startSec": start})
}
