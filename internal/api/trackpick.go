package api

import (
	"cmp"
	"net/http"
	"slices"
	"strings"

	"github.com/yeixio/matinee/i18n"
	"github.com/yeixio/matinee/internal/probe"
	"github.com/yeixio/matinee/internal/store"
	"github.com/yeixio/matinee/internal/subtitle"
)

// Which sound and subtitle tracks a video starts with, worked out here so
// the web player and every app start the same way:
//
//   - What the person picked for this title last time, if anything. An
//     episode's pick carries over to the rest of its show, and a version's
//     to the title's other versions, by language.
//   - Else the sound in the person's language (the one Matinee shows them,
//     see languages.go), else in the title's original language, else the
//     file's own default. Among tracks in that language, the best: Atmos or
//     DTS:X, then the most channels, then the better format, then the
//     higher bitrate. Commentaries and audio description are only played
//     when picked.
//   - Subtitles as the person's subtitle choices say (see next.go), against
//     the language of the sound picked.

// trackPickView is what a video starts with. Players that know it start
// with these tracks; ones from before it keep picking for themselves.
type trackPickView struct {
	// Audio is the index of the audio stream to play, as in mediaInfo;
	// absent until the file is analyzed.
	Audio *int `json:"audio,omitempty"`
	// Subtitle is the subtitle track to show, "" for none.
	Subtitle string `json:"subtitle"`
	// AudioRemembered and SubtitleRemembered say the person picked these
	// for this title (or its show) before.
	AudioRemembered    bool `json:"audioRemembered,omitempty"`
	SubtitleRemembered bool `json:"subtitleRemembered,omitempty"`
}

// langBase is a language tag as a short code without its region: "eng",
// "en-GB" and "English" are all "en". Unknown or undetermined is "".
func langBase(tag string) string {
	if c := subtitle.LanguageCode(tag); c != "" {
		return c
	}
	l := strings.ToLower(strings.TrimSpace(tag))
	if i := strings.IndexAny(l, "-_"); i >= 0 {
		l = l[:i]
	}
	if c := subtitle.LanguageCode(l); c != "" {
		return c
	}
	switch l {
	case "", "und", "unk", "mis", "zxx", "mul":
		return ""
	}
	return l
}

// audioKind is what a sound track is besides a plain mix.
func audioKind(a probe.AudioStream) string {
	switch {
	case a.Commentary:
		return "commentary"
	case a.Descriptive:
		return "descriptive"
	}
	return ""
}

// subtitleKind is what a subtitle track is besides plain lines.
func subtitleKind(t subtitle.Track) string {
	switch {
	case t.Forced:
		return "forced"
	case t.SDH:
		return "sdh"
	}
	return ""
}

// codecRank orders sound formats, lossless first.
func codecRank(a probe.AudioStream) int {
	c, p := strings.ToLower(a.Codec), strings.ToLower(a.Profile)
	switch {
	case c == "truehd" || c == "mlp":
		return 9
	case c == "dts" && (strings.Contains(p, "ma") || strings.Contains(p, "x")):
		return 8
	case c == "flac" || c == "alac" || strings.HasPrefix(c, "pcm"):
		return 8
	case c == "eac3":
		return 6
	case c == "dts":
		return 5
	case c == "ac3", c == "opus":
		return 4
	case c == "aac", c == "vorbis":
		return 3
	case c == "mp3":
		return 2
	case c == "mp2":
		return 1
	}
	return 0
}

// betterAudio orders tracks best first.
func betterAudio(a, b probe.AudioStream) int {
	immersive := func(s probe.AudioStream) int {
		if s.Atmos || s.DTSX {
			return 1
		}
		return 0
	}
	return cmp.Or(
		cmp.Compare(immersive(b), immersive(a)),
		cmp.Compare(b.Channels, a.Channels),
		cmp.Compare(codecRank(b), codecRank(a)),
		cmp.Compare(b.Bitrate, a.Bitrate),
		cmp.Compare(boolInt(b.Default), boolInt(a.Default)),
		cmp.Compare(a.Index, b.Index),
	)
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// fileDefaultAudio is the track a file plays when nobody picks one.
func fileDefaultAudio(audio []probe.AudioStream) (probe.AudioStream, bool) {
	for _, a := range audio {
		if a.Default {
			return a, true
		}
	}
	if len(audio) > 0 {
		return audio[0], true
	}
	return probe.AudioStream{}, false
}

// bestAudioIn is the best plain track (no commentary, no description) in
// a language, if the file has one.
func bestAudioIn(audio []probe.AudioStream, lang, kind string) (probe.AudioStream, bool) {
	var same []probe.AudioStream
	for _, a := range audio {
		if lang != "" && langBase(a.Language) == lang && audioKind(a) == kind {
			same = append(same, a)
		}
	}
	if len(same) == 0 {
		return probe.AudioStream{}, false
	}
	slices.SortStableFunc(same, betterAudio)
	return same[0], true
}

// pickAudio chooses the sound a video starts with, for someone who
// hasn't picked one: in the first of languages the file has, the best.
// languages are the person's own, then the title's original one.
func pickAudio(audio []probe.AudioStream, languages ...string) (probe.AudioStream, bool) {
	def, ok := fileDefaultAudio(audio)
	if !ok {
		return def, false
	}
	for _, l := range append(languages, langBase(def.Language)) {
		if a, ok := bestAudioIn(audio, langBase(l), ""); ok {
			return a, true
		}
	}
	return def, true
}

// rememberedAudio is the track a person picked before, on this item (exact
// index when it's still the same track) or on its show or other version
// (same language, plain sound only).
func rememberedAudio(audio []probe.AudioStream, c store.TrackChoice, sameItem bool) (probe.AudioStream, bool) {
	if c.Audio < 0 {
		return probe.AudioStream{}, false
	}
	if sameItem {
		for _, a := range audio {
			if a.Index == c.Audio && langBase(a.Language) == c.AudioLanguage && audioKind(a) == c.AudioKind {
				return a, true
			}
		}
		return bestAudioIn(audio, c.AudioLanguage, c.AudioKind)
	}
	if c.AudioKind != "" {
		return probe.AudioStream{}, false
	}
	return bestAudioIn(audio, c.AudioLanguage, "")
}

// rememberedSubtitle is the subtitle track a person picked before: off,
// the same track, or one in the same language and of the same kind.
func rememberedSubtitle(tracks []subtitle.Track, c store.TrackChoice, sameItem bool) (string, bool) {
	switch c.Subtitle {
	case "":
		return "", false
	case "off":
		return "", true
	}
	if sameItem {
		if t, ok := subtitle.Find(tracks, c.Subtitle); ok {
			return t.ID, true
		}
	}
	var same []subtitle.Track
	for _, t := range tracks {
		if c.SubtitleLanguage != "" && langBase(t.Language) == c.SubtitleLanguage && subtitleKind(t) == c.SubtitleKind {
			same = append(same, t)
		}
	}
	if len(same) == 0 {
		return "", false
	}
	slices.SortStableFunc(same, betterSubtitle)
	return same[0].ID, true
}

// betterSubtitle puts text before pictures (nothing drawn in), then plain
// lines before SDH.
func betterSubtitle(a, b subtitle.Track) int {
	return cmp.Or(cmp.Compare(boolInt(a.BurnIn), boolInt(b.BurnIn)), cmp.Compare(boolInt(a.SDH), boolInt(b.SDH)))
}

// wantedSubtitleLanguages are the person's subtitle languages, else the
// language Matinee shows them.
func wantedSubtitleLanguages(p *subtitlePrefs, appLanguage string) []string {
	list := []string{appLanguage}
	if p != nil && len(p.Languages) > 0 {
		list = p.Languages
	}
	var out []string
	for _, l := range list {
		if b := langBase(l); b != "" && !slices.Contains(out, b) {
			out = append(out, b)
		}
	}
	return out
}

// pickSubtitle chooses the subtitles a video starts with, as the web
// player and the apps did before (subtitlePrefs.ts): off is off; a full
// track in the person's first language that has one when they always want
// subtitles, or when the sound isn't in one of their languages; else
// forced lines, best in the sound's language.
func pickSubtitle(tracks []subtitle.Track, p *subtitlePrefs, appLanguage, audioLanguage string) string {
	mode := "forced"
	if p != nil {
		mode = p.Mode
	}
	if mode == "off" {
		return ""
	}
	languages := wantedSubtitleLanguages(p, appLanguage)
	audio := langBase(audioLanguage)
	if mode == "always" || mode == "foreign" && audio != "" && !slices.Contains(languages, audio) {
		for _, l := range languages {
			var same []subtitle.Track
			for _, t := range tracks {
				if !t.Forced && langBase(t.Language) == l {
					same = append(same, t)
				}
			}
			if len(same) > 0 {
				slices.SortStableFunc(same, betterSubtitle)
				return same[0].ID
			}
		}
	}
	var forced []subtitle.Track
	for _, t := range tracks {
		if t.Forced {
			forced = append(forced, t)
		}
	}
	if audio != "" {
		for _, t := range forced {
			if langBase(t.Language) == audio {
				return t.ID
			}
		}
	}
	for _, t := range forced {
		if t.Default {
			return t.ID
		}
	}
	if len(forced) > 0 {
		return forced[0].ID
	}
	return ""
}

// trackFamily are the items whose picks carry over to this one: an
// episode's show, a version's main file.
func trackFamily(v *itemView) []int64 {
	var ids []int64
	if v.VersionOf != nil {
		ids = append(ids, *v.VersionOf)
	}
	if v.Kind == "episode" && len(v.Parents) > 0 && v.Parents[0].Kind == "show" {
		ids = append(ids, v.Parents[0].ID)
	}
	return ids
}

// addTrackPick works out which tracks a video starts with.
func (s *Server) addTrackPick(r *http.Request, v *itemView) {
	if !v.Playable || v.VideoCodec == "" {
		return
	}
	ctx, user := r.Context(), currentUser(r)
	pick := &trackPickView{}
	var audio []probe.AudioStream
	if v.MediaInfo != nil {
		audio = v.MediaInfo.Audio
	}
	// What the person picked, here first, then on the show or main version.
	type choice struct {
		store.TrackChoice
		same bool
	}
	var choices []choice
	for i, id := range append([]int64{v.ID}, trackFamily(v)...) {
		c, err := s.Store.TrackChoiceFor(ctx, user.ID, id)
		if err != nil {
			continue
		}
		choices = append(choices, choice{c, i == 0})
	}

	var played probe.AudioStream
	known := false
	for _, c := range choices {
		if a, ok := rememberedAudio(audio, c.TrackChoice, c.same); ok {
			played, known, pick.AudioRemembered = a, true, true
			break
		}
	}
	appLanguage := s.requestLanguage(r, user)
	original := s.originalLanguage(r, v)
	if !known {
		played, known = pickAudio(audio, appLanguage, original)
	}
	if known {
		pick.Audio = &played.Index
	}

	for _, c := range choices {
		if id, ok := rememberedSubtitle(v.Subtitles, c.TrackChoice, c.same); ok {
			pick.Subtitle, pick.SubtitleRemembered = id, true
			break
		}
	}
	if !pick.SubtitleRemembered {
		prefs, err := s.myPlayback(r)
		if err != nil {
			s.Log.Warn("playback prefs", "err", err)
		}
		if appLanguage == "" {
			appLanguage = i18n.Source
		}
		audioLanguage := played.Language
		if audioLanguage == "" {
			audioLanguage = original
		}
		pick.Subtitle = pickSubtitle(v.Subtitles, prefs.Subtitles, appLanguage, audioLanguage)
	}
	v.TrackPick = pick
}

// originalLanguage is the language a title was made in: its own
// details', else its show's.
func (s *Server) originalLanguage(r *http.Request, v *itemView) string {
	if v.Details != nil && v.Details.OriginalLanguage != "" {
		return v.Details.OriginalLanguage
	}
	if len(v.Parents) > 0 && v.Parents[0].Kind == "show" {
		if d, err := s.Store.ItemDetails(r.Context(), v.Parents[0].ID); err == nil && d != nil {
			return d.OriginalLanguage
		}
	}
	return ""
}

// startAudio is the sound a stream plays when the player doesn't say: the
// same pick the item's trackPick names, so apps from before trackPick
// start in the person's language too.
func (s *Server) startAudio(r *http.Request, it store.Item, mi *probe.MediaInfo) int {
	if mi == nil || len(mi.Audio) == 0 || it.VideoCodec == "" {
		return 0
	}
	v := itemView{Item: it, MediaInfo: mi}
	if it.Kind == "episode" {
		if show, ok := s.showOf(r, it); ok {
			v.Parents = []store.Item{show}
		}
	}
	if d, err := s.Store.ItemDetails(r.Context(), it.ID); err == nil {
		v.Details = d
	}
	s.addTrackPick(r, &v)
	if v.TrackPick == nil || v.TrackPick.Audio == nil {
		return 0
	}
	return *v.TrackPick.Audio
}

// trackChoiceIn is a pick from a player. Only what's sent changes: a
// player that changed the sound sends just audio.
type trackChoiceIn struct {
	// Audio is the index of the audio stream picked.
	Audio *int `json:"audio"`
	// Subtitle is the subtitle track picked, or "off".
	Subtitle *string `json:"subtitle"`
}

// setTrackChoice remembers the tracks a person picked while watching, for
// the next time they play the title, and the rest of its show.
func (s *Server) setTrackChoice(w http.ResponseWriter, r *http.Request) {
	it, ok := s.loadItem(w, r)
	if !ok {
		return
	}
	if !it.Playable || it.VideoCodec == "" {
		writeError(w, http.StatusBadRequest, "not a video")
		return
	}
	var in trackChoiceIn
	if err := readJSON(r, &in); err != nil || in.Audio == nil && in.Subtitle == nil {
		writeError(w, http.StatusBadRequest, "bad request")
		return
	}
	c := store.NoTrackChoice
	if in.Audio != nil {
		// The file's tracks are known once it's analyzed.
		var audio []probe.AudioStream
		if mi, _ := s.mediaInfo(r, it); mi != nil {
			audio = mi.Audio
		}
		i := slices.IndexFunc(audio, func(a probe.AudioStream) bool { return a.Index == *in.Audio })
		if i < 0 {
			writeError(w, http.StatusBadRequest, "no such audio track")
			return
		}
		a := audio[i]
		c.Audio, c.AudioLanguage, c.AudioKind = a.Index, langBase(a.Language), audioKind(a)
	}
	if in.Subtitle != nil {
		if *in.Subtitle == "off" || *in.Subtitle == "" {
			c.Subtitle = "off"
		} else {
			t, ok := subtitle.Find(s.subtitleTracks(r.Context(), it), *in.Subtitle)
			if !ok {
				writeError(w, http.StatusBadRequest, "no such subtitle track")
				return
			}
			c.Subtitle, c.SubtitleLanguage, c.SubtitleKind = t.ID, langBase(t.Language), subtitleKind(t)
		}
	}
	ctx, user := r.Context(), currentUser(r)
	v := itemView{Item: it}
	if it.Kind == "episode" {
		if show, ok := s.showOf(r, it); ok {
			v.Parents = []store.Item{show}
		}
	}
	for _, id := range append([]int64{it.ID}, trackFamily(&v)...) {
		if err := s.Store.SetTrackChoice(ctx, user.ID, id, c, in.Audio != nil, in.Subtitle != nil); err != nil {
			s.serverError(w, err)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}
