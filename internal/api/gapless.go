package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/yeixio/matinee/internal/stream"
)

// setGapless saves whether an album plays without gaps between its songs,
// for the whole household: {"gapless": true} or false, or null to leave it
// to Matinee (see internal/gapless). Friends from other servers can't.
func (s *Server) setGapless(w http.ResponseWriter, r *http.Request) {
	if currentUser(r).Friend {
		writeError(w, http.StatusForbidden, "only the household can change this")
		return
	}
	it, ok := s.loadItem(w, r)
	if !ok {
		return
	}
	if it.Kind != "album" {
		writeError(w, http.StatusBadRequest, "not an album")
		return
	}
	var body struct {
		Gapless *bool `json:"gapless"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "bad request")
		return
	}
	if err := s.Store.SetGapless(r.Context(), it.ID, body.Gapless); err != nil {
		s.serverError(w, err)
		return
	}
	it, ok = s.loadItem(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, it)
}

// gaplessTrack is where a song starts on a gapless stream's clock.
type gaplessTrack struct {
	ID       int64   `json:"id"`
	StartSec float64 `json:"startSec"`
}

// startGapless starts one continuous stream of an album's songs, in the
// order given, so they play without a gap between them: {"tracks": [ids],
// "start": seconds into the first}. The answer has the stream's playlist
// and where each song starts on its clock; players show the song whose
// start they have passed.
func (s *Server) startGapless(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Tracks []int64  `json:"tracks"`
		Start  *float64 `json:"start"`
	}
	if err := readJSON(r, &body); err != nil || len(body.Tracks) == 0 {
		writeError(w, http.StatusBadRequest, "bad request")
		return
	}
	if len(body.Tracks) > stream.MaxGapless {
		body.Tracks = body.Tracks[:stream.MaxGapless]
	}
	start := 0.0
	if body.Start != nil && *body.Start > 0 {
		start = *body.Start
	}
	tracks, ok, err := s.Store.GaplessTracks(r.Context(), currentUser(r).ID, body.Tracks)
	if err != nil {
		s.serverError(w, err)
		return
	}
	if !ok {
		writeError(w, http.StatusBadRequest, "tracks must be songs on one album")
		return
	}
	group := "album:" + strconv.FormatInt(*tracks[0].ParentID, 10)
	id, err := s.Transcoder.StartGapless(r.Context(), currentUser(r).ID, group, tracks, start)
	if errors.Is(err, stream.ErrSuperseded) {
		writeError(w, http.StatusConflict, "replaced by a newer seek")
		return
	}
	if err != nil {
		s.Log.Error("gapless stream", "album", *tracks[0].ParentID, "err", err)
		writeError(w, http.StatusInternalServerError, "couldn't start playback for this album")
		return
	}
	offsets := stream.Offsets(tracks, start)
	out := make([]gaplessTrack, len(tracks))
	for i, tr := range tracks {
		out[i] = gaplessTrack{ID: tr.ID, StartSec: offsets[i]}
	}
	writeJSON(w, http.StatusOK, map[string]any{"url": "/api/v1/hls/" + id + "/index.m3u8", "startSec": start, "tracks": out})
}
