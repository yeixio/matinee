package subtitle

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// ASS/SSA subtitles carry more than words: where each line goes, fonts
// (often attached to an MKV), colors, karaoke and signs. Players that can
// draw them (the web player, with a WASM build of libass) get the original
// track and the file's fonts from here; others get them burned in.

// ServeASS writes a styled track as it is, in UTF-8.
func (c *Converter) ServeASS(w http.ResponseWriter, r *http.Request, videoPath string, t Track) {
	if !t.Styled {
		http.Error(w, "not an ASS/SSA track", http.StatusUnprocessableEntity)
		return
	}
	path, err := c.ass(videoPath, t)
	if err != nil {
		http.Error(w, "subtitles unavailable", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/x-ssa; charset=utf-8")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	http.ServeFile(w, r, path)
}

func (c *Converter) ass(videoPath string, t Track) (string, error) {
	src, key := videoPath, videoPath+"#ass"+strconv.Itoa(t.Stream)
	if t.External {
		src, key = t.Path, t.Path+"#ass"
	}
	info, err := os.Stat(src)
	if err != nil {
		return "", err
	}
	dst := strings.TrimSuffix(c.cachePath(key, info), ".vtt") + ".ass"
	return dst, c.once(dst, func() error {
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if t.External {
			data, err := os.ReadFile(t.Path)
			if err != nil {
				return err
			}
			data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
			if utf8.Valid(data) {
				return writeAtomic(dst, data)
			}
			// FFmpeg rewrites it in UTF-8, styles and all.
			args := []string{"-v", "error", "-nostdin", "-y"}
			if !isUTF16(data) {
				args = append(args, "-sub_charenc", "CP1252")
			}
			args = append(args, "-i", t.Path, "-map", "0:s:0", "-c:s", "ass", "-f", "ass", dst+".tmp")
			if err := c.run(args, time.Minute); err != nil {
				os.Remove(dst + ".tmp")
				return err
			}
			return os.Rename(dst+".tmp", dst)
		}
		args := []string{"-v", "error", "-nostdin", "-y", "-i", videoPath, "-map", "0:s:" + strconv.Itoa(t.Stream), "-c:s", "copy", "-f", "ass", dst + ".tmp"}
		if err := c.run(args, extractTimeout); err != nil {
			os.Remove(dst + ".tmp")
			return err
		}
		return os.Rename(dst+".tmp", dst)
	})
}

// Font is a font attached to a video file, as Matroska files carry the
// fonts their ASS subtitles use.
type Font struct {
	// Index is the font's place among the file's fonts, for FontFile.
	Index int `json:"index"`
	// Name is its file name in the video, such as "Gandhi Sans.ttf".
	Name     string `json:"name"`
	MimeType string `json:"mimeType,omitempty"`

	stream int // the attachment's stream index in the file
}

var fontExts = map[string]bool{".ttf": true, ".otf": true, ".ttc": true, ".otc": true, ".woff": true, ".woff2": true}

func isFont(name, mime string) bool {
	mime = strings.ToLower(mime)
	return fontExts[strings.ToLower(filepath.Ext(name))] || strings.Contains(mime, "font") || strings.Contains(mime, "truetype") || strings.Contains(mime, "opentype")
}

// Fonts lists the fonts attached to a video, in file order.
func (c *Converter) Fonts(ctx context.Context, videoPath string) ([]Font, error) {
	if c.FFprobe == "" {
		return []Font{}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, c.FFprobe, "-v", "error", "-print_format", "json", "-show_entries", "stream=index,codec_type:stream_tags=filename,mimetype", videoPath).Output()
	if err != nil {
		return nil, err
	}
	return parseFonts(out)
}

func parseFonts(data []byte) ([]Font, error) {
	var o struct {
		Streams []struct {
			Index     int               `json:"index"`
			CodecType string            `json:"codec_type"`
			Tags      map[string]string `json:"tags"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(data, &o); err != nil {
		return nil, err
	}
	fonts := []Font{}
	for _, s := range o.Streams {
		tags := map[string]string{}
		for k, v := range s.Tags {
			tags[strings.ToLower(k)] = v
		}
		if s.CodecType != "attachment" || !isFont(tags["filename"], tags["mimetype"]) {
			continue
		}
		// Only the name, never a path someone wrote into the file.
		name := filepath.Base(strings.ReplaceAll(tags["filename"], `\`, "/"))
		if name == "." || name == "/" || name == "" {
			name = "font" + strconv.Itoa(len(fonts)+1)
		}
		fonts = append(fonts, Font{Index: len(fonts), Name: name, MimeType: tags["mimetype"], stream: s.Index})
	}
	return fonts, nil
}

// ServeFont writes font f of the video, extracting it once.
func (c *Converter) ServeFont(w http.ResponseWriter, r *http.Request, videoPath string, f Font) {
	info, err := os.Stat(videoPath)
	if err != nil {
		http.Error(w, "font unavailable", http.StatusNotFound)
		return
	}
	dst := strings.TrimSuffix(c.cachePath(videoPath+"#font"+strconv.Itoa(f.stream), info), ".vtt") + ".font"
	err = c.once(dst, func() error {
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		// FFmpeg dumps attachments while opening the input, then complains
		// that there's no output; the font is what counts.
		tmp := dst + ".tmp"
		os.Remove(tmp)
		runErr := c.run([]string{"-v", "error", "-nostdin", "-y", "-dump_attachment:" + strconv.Itoa(f.stream), tmp, "-i", videoPath}, time.Minute)
		if st, err := os.Stat(tmp); err != nil || st.Size() == 0 {
			os.Remove(tmp)
			if runErr == nil {
				runErr = errors.New("no font written")
			}
			return runErr
		}
		return os.Rename(tmp, dst)
	})
	if err != nil {
		http.Error(w, "font unavailable", http.StatusNotFound)
		return
	}
	mime := f.MimeType
	if mime == "" || !strings.Contains(mime, "/") {
		mime = "font/ttf"
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Cache-Control", "private, max-age=86400")
	http.ServeFile(w, r, dst)
}
