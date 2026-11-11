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
	"github.com/yeixio/matinee/internal/subsync"
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
	// Styled marks ASS/SSA tracks, whose positions, fonts and effects a
	// player can draw from the original (GET …/subtitles/{id}.ass, with the
	// file's fonts) or the server can draw into a transcode.
	Styled bool `json:"styled,omitempty"`
	// Found marks subtitles someone found online for the video (see
	// OpenSubtitles); Removable says the person asking may remove them.
	Found     bool `json:"found,omitempty"`
	Removable bool `json:"removable,omitempty"`
	// Sync says whether the track's times were lined up with the audio,
	// and how (see subsync). Absent for tracks nobody synced.
	Sync *Sync `json:"sync,omitempty"`
	// Shift is how the track's times are moved when it's served.
	Shift subsync.Shift `json:"-"`

	Stream int    `json:"-"` // embedded: index among the file's subtitle streams
	Path   string `json:"-"` // external: the sidecar file
}

// Sync is what lining a track up with the audio did.
type Sync struct {
	// State is syncing (under way), synced (moved), inSync (already
	// right), unsure (no fit stood out, left as it was), failed (couldn't
	// be read or listened to) or off (someone undid it).
	State string `json:"state"`
	// OffsetMs and Rate are how a synced track was moved: each time t is
	// shown at t×Rate + OffsetMs. Rate is absent when it's 1.
	OffsetMs int64   `json:"offsetMs,omitempty"`
	Rate     float64 `json:"rate,omitempty"`
	// Auto says Matinee did it on its own, without anyone asking.
	Auto bool `json:"auto,omitempty"`
	// Reference is what it was lined up with: audio, or subtitles (another
	// track inside the video, whose times are right).
	Reference string `json:"reference,omitempty"`
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
var sidecarExts = map[string]string{".srt": "subrip", ".vtt": "webvtt", ".ass": "ass", ".ssa": "ssa", ".sup": "hdmv_pgs_subtitle", ".idx": "dvd_subtitle"}

// List returns a video's tracks: embedded streams in file order, then
// sidecar files. Codecs Matinee can't show at all are left out.
func List(videoPath string, embedded []probe.Subtitle) []Track {
	return append(Embedded(embedded), Sidecars(videoPath)...)
}

// Embedded is List without the subtitle files beside the video: the
// tracks inside it, from what probing it found.
func Embedded(embedded []probe.Subtitle) []Track {
	out := []Track{}
	for _, s := range embedded {
		t := Track{
			ID: fmt.Sprintf("e%d", s.Index), Stream: s.Index, Codec: s.Codec,
			Forced: s.Forced, Default: s.Default, SDH: s.SDH,
		}
		switch {
		case textCodecs[s.Codec]:
			t.Styled = s.Codec == "ass" || s.Codec == "ssa"
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
	return out
}

// Sidecars finds subtitle files named after a video, like "Movie.srt",
// "Movie.en.srt", "Movie.en.forced.srt" or "Movie.English.SDH.srt", and in a
// Subs or Subtitles folder beside it: "Subs/Movie.en.srt", "Subs/<video
// name>/2_English.srt" (how many TV releases come), or "Subs/2_English.srt"
// when the folder holds just the one video. Picture subtitles come as
// Blu-ray .sup files and VobSub .idx/.sub pairs; they're drawn in.
func Sidecars(videoPath string) []Track {
	dir, base := filepath.Split(videoPath)
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	out := sidecarsIn(dir, "", stem)
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if !e.IsDir() || !subFolders[strings.ToLower(e.Name())] {
			continue
		}
		out = append(out, sidecarsIn(dir, e.Name(), stem)...)
		if sub, err := os.ReadDir(filepath.Join(dir, e.Name())); err == nil {
			for _, d := range sub {
				if d.IsDir() && strings.EqualFold(d.Name(), stem) {
					out = append(out, sidecarsIn(dir, filepath.Join(e.Name(), d.Name()), "")...)
				}
			}
		}
		if onlyVideo(entries) {
			out = append(out, sidecarsIn(dir, e.Name(), "")...)
		}
	}
	slices.SortFunc(out, func(a, b Track) int { return strings.Compare(a.Path, b.Path) })
	return slices.CompactFunc(out, func(a, b Track) bool { return a.Path == b.Path })
}

var subFolders = map[string]bool{"subs": true, "subtitles": true, "sub": true}

var videoExts = map[string]bool{".mkv": true, ".mp4": true, ".m4v": true, ".avi": true, ".mov": true, ".wmv": true,
	".ts": true, ".m2ts": true, ".webm": true, ".mpg": true, ".mpeg": true, ".flv": true}

// onlyVideo says whether a folder holds one video at most, so a Subs folder
// in it can only be that video's.
func onlyVideo(entries []os.DirEntry) bool {
	n := 0
	for _, e := range entries {
		if !e.IsDir() && videoExts[strings.ToLower(filepath.Ext(e.Name()))] {
			n++
		}
	}
	return n <= 1
}

// sidecarsIn finds the subtitle files in videoDir/rel whose names start with
// stem (all of them for ""). A track's id comes from its path from the
// video's folder, which for files right beside the video is its name.
func sidecarsIn(videoDir, rel, stem string) []Track {
	dir := filepath.Join(videoDir, rel)
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
		if ext == ".idx" && !hasPair(dir, name) {
			continue // VobSub needs its .sub beside the .idx
		}
		// What's between the video's name and the extension: "", ".en", ".en.forced"…
		middle := name[len(stem) : len(name)-len(ext)]
		if stem != "" && middle != "" && middle[0] != '.' {
			continue // "Movie 2.srt" belongs to "Movie 2.mkv", not "Movie.mkv"
		}
		middle = strings.TrimPrefix(middle, ".")
		t := Track{Codec: codec, External: true, Path: filepath.Join(dir, name), Styled: codec == "ass" || codec == "ssa", BurnIn: imageCodecs[codec]}
		sum := sha1.Sum([]byte(filepath.ToSlash(filepath.Join(rel, name))))
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
				case strings.Trim(lw, "0123456789") == "":
					// "2_English.srt": how a release numbers its files.
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
	return out
}

// hasPair says whether a VobSub .idx has its .sub.
func hasPair(dir, idx string) bool {
	stem := strings.TrimSuffix(idx, filepath.Ext(idx))
	for _, ext := range []string{".sub", ".SUB"} {
		if fileExists(filepath.Join(dir, stem+ext)) {
			return true
		}
	}
	return false
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
