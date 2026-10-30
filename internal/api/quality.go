package api

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/yeixio/matinee/internal/store"
	"github.com/yeixio/matinee/internal/stream"
)

// Streaming quality (issue #144). With adaptive quality on, a transcode
// makes several quality steps at once and players move between them as the
// connection allows; people can also pick one step themselves. A cap for
// streams away from home applies either way.
const (
	// settingAdaptiveQuality is "true" once an admin turns adaptive quality on.
	settingAdaptiveQuality = "adaptive_quality"
	// settingQualitySteps lists the steps adaptive streams use, by id and
	// comma-separated; unset means all of stream.Steps.
	settingQualitySteps = "quality_steps"
	// settingRemoteQualityCap is the best step a stream away from home may
	// use, by id; unset means no cap.
	settingRemoteQualityCap = "remote_quality_cap"
)

// Qualities a player can ask a transcode for, besides a step's id.
const (
	qualityAuto     = "auto"
	qualityOriginal = "original"
)

var errBadQuality = errors.New("quality must be auto, original or a quality step")

type qualitySettings struct {
	Adaptive bool
	// Steps are the ones adaptive streams use, best first.
	Steps []stream.Step
	// Cap is the remote cap's step id, or "".
	Cap string
}

func (s *Server) qualitySettings(ctx context.Context) (qualitySettings, error) {
	var q qualitySettings
	on, err := s.Store.Setting(ctx, settingAdaptiveQuality)
	if err != nil {
		return q, err
	}
	steps, err := s.Store.Setting(ctx, settingQualitySteps)
	if err != nil {
		return q, err
	}
	q.Cap, err = s.Store.Setting(ctx, settingRemoteQualityCap)
	if err != nil {
		return q, err
	}
	q.Adaptive = on == "true"
	q.Steps = parseSteps(steps)
	if _, ok := stream.StepByID(q.Cap); !ok {
		q.Cap = ""
	}
	return q, nil
}

// parseSteps reads settingQualitySteps, keeping stream.Steps' order.
func parseSteps(v string) []stream.Step {
	if v == "" {
		return stream.Steps
	}
	ids := strings.Split(v, ",")
	var out []stream.Step
	for _, st := range stream.Steps {
		if slices.Contains(ids, st.ID) {
			out = append(out, st)
		}
	}
	if len(out) == 0 {
		return stream.Steps
	}
	return out
}

// stepIDs lists steps by id.
func stepIDs(steps []stream.Step) []string {
	out := []string{}
	for _, st := range steps {
		out = append(out, st.ID)
	}
	return out
}

// streamCap is the best step a stream for r may use: the remote cap away
// from home, nil at home or without one.
func (s *Server) streamCap(r *http.Request, q qualitySettings) *stream.Step {
	if q.Cap == "" || s.fromHome(r) {
		return nil
	}
	st, _ := stream.StepByID(q.Cap)
	return &st
}

// streamingView tells players which qualities they may offer.
type streamingView struct {
	// Adaptive means players offer Auto and the steps; without it they
	// only play the original.
	Adaptive bool `json:"adaptive"`
	// Steps are the fixed qualities to offer, best first, within the cap.
	Steps []stream.Step `json:"steps"`
	// Cap is the best quality this device may stream right now (it's away
	// from home and an admin set a cap), or null. A file that's bigger
	// plays through a transcode at this quality instead of directly.
	Cap *stream.Step `json:"cap"`
}

func (s *Server) getStreaming(w http.ResponseWriter, r *http.Request) {
	q, err := s.qualitySettings(r.Context())
	if err != nil {
		s.serverError(w, err)
		return
	}
	cap := s.streamCap(r, q)
	v := streamingView{Adaptive: q.Adaptive, Steps: []stream.Step{}, Cap: cap}
	if q.Adaptive {
		v.Steps = withinCap(q.Steps, cap)
	}
	writeJSON(w, http.StatusOK, v)
}

// withinCap leaves out the steps better than cap, keeping cap itself if
// none are left.
func withinCap(steps []stream.Step, cap *stream.Step) []stream.Step {
	if cap == nil {
		return steps
	}
	var out []stream.Step
	for _, st := range steps {
		if st.Width <= cap.Width {
			out = append(out, st)
		}
	}
	if len(out) == 0 {
		out = []stream.Step{*cap}
	}
	return out
}

// hlsSteps works out the steps a transcode of it makes for a player asking
// for quality (auto, original or a step's id), and what quality that is.
// Adaptive quality off leaves only the original, and a cap brings anything
// better down to it.
func (s *Server) hlsSteps(r *http.Request, it store.Item, quality string) ([]stream.Step, string, error) {
	q, err := s.qualitySettings(r.Context())
	if err != nil {
		return nil, "", err
	}
	if it.VideoCodec == "" {
		return nil, qualityOriginal, nil
	}
	cap := s.streamCap(r, q)
	if !q.Adaptive {
		quality = qualityOriginal
	}
	switch quality {
	case qualityAuto:
		return withinCap(q.Steps, cap), qualityAuto, nil
	case qualityOriginal, "":
		if cap != nil {
			return []stream.Step{*cap}, qualityOriginal, nil
		}
		return nil, qualityOriginal, nil
	}
	st, ok := stream.StepByID(quality)
	if !ok {
		return nil, "", errBadQuality
	}
	if cap != nil && st.Width > cap.Width {
		st = *cap
	}
	return []stream.Step{st}, st.ID, nil
}
