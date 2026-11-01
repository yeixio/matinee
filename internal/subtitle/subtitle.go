// Package subtitle finds a video's subtitle tracks, embedded or in files next
// to it, and serves the text ones as WebVTT for the web player.
package subtitle

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/yeixio/matinee/internal/probe"
)

// Track is one subtitle track a player can offer.
type Track struct {
	ID    string `json:"id"`    // "e<N>" for embedded stream N, "x<hash>" for a sidecar file
	Label string `json:"label"` // in English, such as "English (SDH)"
	// Note is the track's own title, where it says more than its language
	// and flags, such as "Commentary", so apps can build a label in their
	// own language.
	Note     string `json:"note,omitempty"`
	Language string `json:"language,omitempty"` // ISO 639-1 where known, e.g. "en"
	Codec    string `json:"codec"`
	Forced   bool   `json:"forced,omitempty"`
	Default  bool   `json:"default,omitempty"`
	SDH      bool   `json:"sdh,omitempty"`
	External bool   `json:"external,omitempty"`
	// BurnIn marks picture-based subtitles (PGS, VobSub, DVB), which browsers
	// can't render: the server draws them into the video during a transcode.
	BurnIn bool `json:"burnIn,omitempty"`
	// Found marks subtitles someone found online for the video (see
	// OpenSubtitles); Removable says the person asking may remove them.
	Found     bool `json:"found,omitempty"`
	Removable bool `json:"removable,omitempty"`

	Stream int    `json:"-"` // embedded: index among the file's subtitle streams
	Path   string `json:"-"` // external: the sidecar file
}

// Text codecs FFmpeg can turn into WebVTT.
var textCodecs = map[string]bool{
	"subrip": true, "srt": true, "ass": true, "ssa": true, "webvtt": true, "mov_text": true,
	"text": true, "microdvd": true, "subviewer": true, "subviewer1": true, "mpl2": true, "vplayer": true,
	"realtext": true, "sami": true, "jacosub": true, "pjs": true, "stl": true,
}

// Picture codecs that have to be burned in.
var imageCodecs = map[string]bool{
	"hdmv_pgs_subtitle": true, "dvd_subtitle": true, "dvb_subtitle": true, "xsub": true,
}

// Sidecar extensions, mapped to the codec name FFmpeg gives them.
var sidecarExts = map[string]string{".srt": "subrip", ".vtt": "webvtt", ".ass": "ass", ".ssa": "ssa"}

// List returns a video's tracks: embedded streams in file order, then
// sidecar files. Codecs Matinee can't show at all are left out.
func List(videoPath string, embedded []probe.Subtitle) []Track {
	out := []Track{}
	for _, s := range embedded {
		t := Track{
			ID: fmt.Sprintf("e%d", s.Index), Stream: s.Index, Codec: s.Codec,
			Forced: s.Forced, Default: s.Default, SDH: s.SDH,
		}
		switch {
		case textCodecs[s.Codec]:
		case imageCodecs[s.Codec]:
			t.BurnIn = true
		default:
			continue
		}
		code, name := lookupLanguage(s.Language)
		t.Language = code
		t.Label, t.Note = label(name, s.Title, t.Forced, t.SDH), note(name, s.Title, t.Forced, t.SDH)
		if t.Label == "" {
			t.Label = fmt.Sprintf("Track %d", s.Index+1)
		}
		out = append(out, t)
	}
	return append(out, Sidecars(videoPath)...)
}

// Sidecars finds subtitle files named after a video, like "Movie.srt",
// "Movie.en.srt", "Movie.en.forced.srt" or "Movie.English.SDH.srt".
func Sidecars(videoPath string) []Track {
	dir, base := filepath.Split(videoPath)
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []Track
	for _, e := range entries {
		name := e.Name()
		ext := strings.ToLower(filepath.Ext(name))
		codec, ok := sidecarExts[ext]
		if !ok || e.IsDir() || len(name) < len(stem) || !strings.EqualFold(name[:len(stem)], stem) {
			continue
		}
		// What's between the video's name and the extension: "", ".en", ".en.forced"…
		middle := name[len(stem) : len(name)-len(ext)]
		if middle != "" && middle[0] != '.' {
			continue // "Movie 2.srt" belongs to "Movie 2.mkv", not "Movie.mkv"
		}
		middle = strings.TrimPrefix(middle, ".")
		t := Track{Codec: codec, External: true, Path: filepath.Join(dir, name)}
		sum := sha1.Sum([]byte(name))
		t.ID = "x" + hex.EncodeToString(sum[:6])
		var langName string
		var extra []string
		for _, part := range strings.Split(middle, ".") {
			// "pt-BR" is one language; "en_forced" is two words.
			words := []string{part}
			if code, n := lookupLanguage(part); n == "" || langName != "" {
				words = strings.FieldsFunc(part, func(r rune) bool { return r == '_' || r == '-' || r == ' ' })
			} else {
				t.Language, langName = code, n
				continue
			}
			for _, w := range words {
				switch lw := strings.ToLower(w); {
				case lw == "forced" || lw == "foreign":
					t.Forced = true
				case lw == "sdh" || lw == "cc" || lw == "hi" && langName != "":
					t.SDH = true // "hi" is Hindi, unless it follows a language: "Movie.en.hi.srt"
				case lw == "default":
					t.Default = true
				default:
					if code, n := lookupLanguage(w); n != "" && langName == "" {
						t.Language, langName = code, n
					} else {
						extra = append(extra, w)
					}
				}
			}
		}
		t.Label = label(langName, strings.Join(extra, " "), t.Forced, t.SDH)
		t.Note = note(langName, strings.Join(extra, " "), t.Forced, t.SDH)
		if t.Label == "" {
			t.Label = strings.ToUpper(strings.TrimPrefix(ext, ".")) + " file"
		}
		out = append(out, t)
	}
	slices.SortFunc(out, func(a, b Track) int { return strings.Compare(a.Path, b.Path) })
	return out
}

// label reads like "English", "English (Forced)" or "English (SDH, Commentary)".
func label(lang, title string, forced, sdh bool) string {
	var notes []string
	if forced {
		notes = append(notes, "Forced")
	}
	if sdh {
		notes = append(notes, "SDH")
	}
	if t := note(lang, title, forced, sdh); t != "" {
		notes = append(notes, t)
	}
	if lang == "" {
		if len(notes) == 0 {
			return ""
		}
		lang, notes = notes[len(notes)-1], notes[:len(notes)-1]
	}
	if len(notes) == 0 {
		return lang
	}
	return lang + " (" + strings.Join(notes, ", ") + ")"
}

// note is a track's title where it adds to its language and flags: titles
// often just repeat them.
func note(lang, title string, forced, sdh bool) string {
	t := strings.TrimSpace(title)
	if strings.EqualFold(t, lang) || forced && strings.EqualFold(t, "forced") || sdh && strings.EqualFold(t, "sdh") {
		return ""
	}
	return t
}

// Find returns the track with the given id.
func Find(tracks []Track, id string) (Track, bool) {
	for _, t := range tracks {
		if t.ID == id {
			return t, true
		}
	}
	return Track{}, false
}
