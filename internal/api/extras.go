package api

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	"github.com/yeixio/matinee/internal/probe"
	"github.com/yeixio/matinee/internal/scan"
	"github.com/yeixio/matinee/internal/store"
	"github.com/yeixio/matinee/internal/stream"
)

// Extras are a title's special features from files next to it: behind the
// scenes, deleted scenes, featurettes, interviews, scenes, shorts, trailers
// and others (see scan/extras.go). They play like trailers, from the file or
// converted, and don't count as watching the title. Commentary tracks live
// inside the title's own file, in its mediaInfo's audio.

type extraView struct {
	ID string `json:"id"`
	// Kind is trailer, behindTheScenes, deleted, featurette, interview,
	// scene, short or other.
	Kind string `json:"kind"`
	// Name is the extra's own name; empty when it has none, so players call
	// it by its kind.
	Name       string `json:"name"`
	MimeType   string `json:"mimeType,omitempty"`
	Container  string `json:"container,omitempty"`
	VideoCodec string `json:"videoCodec,omitempty"`
	AudioCodec string `json:"audioCodec,omitempty"`
	DurationMs int64  `json:"durationMs,omitempty"`
}

type localExtra struct {
	id    string
	extra scan.Extra
	probe probe.Result
}

// findExtras lists the extras in a title's folders: own are the title's,
// and show its show's, for a season.
func (s *Server) findExtras(ctx context.Context, it store.Item) (own, show []scan.Extra) {
	switch it.Kind {
	case "movie":
		if root := s.libraryRoot(ctx, it.LibraryID, it.Path); root != "" {
			own = scan.MovieExtras(root, it.Path)
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
		showDir, seasonDir := scan.ShowFolders(root, file)
		if showDir != "" {
			show = scan.FolderExtras(showDir)
		}
		if it.Kind == "show" {
			own = show
		} else if seasonDir != "" {
			own = scan.FolderExtras(seasonDir)
		}
	}
	return own, show
}

// extraID names an extra by its file, so it stays the same while other
// extras come and go.
func extraID(path string) string {
	sum := sha1.Sum([]byte(path))
	return hex.EncodeToString(sum[:6])
}

// localExtras finds a title's extras that are videos, probing a few at once.
func (s *Server) localExtras(ctx context.Context, it store.Item) []localExtra {
	own, _ := s.findExtras(ctx, it)
	found := make([]localExtra, len(own))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	for i, e := range own {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			pr, err := s.probeTrailer(ctx, e.Path)
			if err == nil && pr.VideoCodec != "" {
				found[i] = localExtra{extraID(e.Path), e, pr}
			}
		}()
	}
	wg.Wait()
	out := []localExtra{}
	for _, e := range found {
		if e.id != "" {
			out = append(out, e)
		}
	}
	return out
}

func (s *Server) listExtras(w http.ResponseWriter, r *http.Request) {
	it, ok := s.loadItem(w, r)
	if !ok {
		return
	}
	// A file named after the title, such as "Heat (1995)-featurette.mkv",
	// has no name of its own.
	titles := []string{it.Title}
	if it.Kind == "movie" && it.Path != "" {
		titles = append(titles, stemOf(it.Path), filepath.Base(filepath.Dir(it.Path)))
	}
	out := []extraView{}
	for _, e := range s.localExtras(r.Context(), it) {
		out = append(out, extraView{
			ID: e.id, Kind: e.extra.Kind, Name: scan.ExtraName(e.extra.Path, titles...),
			MimeType: stream.MimeType(e.extra.Path), Container: e.probe.Container, VideoCodec: e.probe.VideoCodec, AudioCodec: e.probe.AudioCodec, DurationMs: e.probe.DurationMs,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func stemOf(path string) string {
	base := filepath.Base(path)
	return base[:len(base)-len(filepath.Ext(base))]
}

// extraFor finds the extra a request names.
func (s *Server) extraFor(w http.ResponseWriter, r *http.Request) (store.Item, localExtra, bool) {
	it, ok := s.loadItem(w, r)
	if !ok {
		return it, localExtra{}, false
	}
	id := r.PathValue("extra")
	for _, e := range s.localExtras(r.Context(), it) {
		if e.id == id {
			return it, e, true
		}
	}
	writeError(w, http.StatusNotFound, "no such extra")
	return it, localExtra{}, false
}

// extraItem stands an extra in for its title, for the file and transcode
// handlers. It has the title's id, so starting one replaces the title's own
// transcode for this viewer, and the other way round.
func extraItem(it store.Item, e localExtra) store.Item {
	return store.Item{
		ID: it.ID, LibraryID: it.LibraryID, Kind: "extra", Title: it.Title, Path: e.extra.Path, Playable: true,
		VideoCodec: e.probe.VideoCodec, AudioCodec: e.probe.AudioCodec, Container: e.probe.Container,
	}
}

func (s *Server) extraFile(w http.ResponseWriter, r *http.Request) {
	if it, e, ok := s.extraFor(w, r); ok {
		stream.ServeDirect(w, r, extraItem(it, e))
	}
}

func (s *Server) extraHLS(w http.ResponseWriter, r *http.Request) {
	it, e, ok := s.extraFor(w, r)
	if !ok {
		return
	}
	start, _ := strconv.ParseFloat(r.URL.Query().Get("start"), 64)
	x := extraItem(it, e)
	id, err := s.Transcoder.Start(r.Context(), currentUser(r).ID, x, start, -1, 0)
	if errors.Is(err, stream.ErrSuperseded) {
		writeError(w, http.StatusConflict, "replaced by a newer seek")
		return
	}
	if err != nil {
		s.Log.Error("transcode extra", "item", it.ID, "err", err)
		writeError(w, http.StatusInternalServerError, "couldn't start playback for this extra")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"url": "/api/v1/hls/" + id + "/index.m3u8", "startSec": start})
}

// extraImage is a still from an extra, a fifth of the way in, where it's
// usually past any titles.
func (s *Server) extraImage(w http.ResponseWriter, r *http.Request) {
	_, e, ok := s.extraFor(w, r)
	if !ok {
		return
	}
	info, err := os.Stat(e.extra.Path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	sec := float64(e.probe.DurationMs) / 5000
	if sec > 120 {
		sec = 120
	}
	s.Art.Serve(w, r, fmt.Sprintf("frame:%.1f:%d:%s", sec, info.ModTime().Unix(), e.extra.Path))
}
