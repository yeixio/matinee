package stream

import (
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/yeixio/matinee/internal/store"
)

var mimeByExt = map[string]string{
	".mp4": "video/mp4", ".m4v": "video/mp4", ".mov": "video/quicktime", ".webm": "video/webm",
	".mkv": "video/x-matroska", ".mp3": "audio/mpeg", ".flac": "audio/flac", ".m4a": "audio/mp4",
	".aac": "audio/aac", ".ogg": "audio/ogg", ".opus": "audio/ogg", ".wav": "audio/wav",
}

// MimeType guesses a file's content type for clients deciding whether they can play it.
func MimeType(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	if m, ok := mimeByExt[ext]; ok {
		return m
	}
	if m := mime.TypeByExtension(ext); m != "" {
		return m
	}
	return "application/octet-stream"
}

// ServeDirect streams the original file with range support, so seeking is instant.
func ServeDirect(w http.ResponseWriter, r *http.Request, it store.Item) {
	f, err := os.Open(it.Path)
	if err != nil {
		http.Error(w, "file unavailable", http.StatusNotFound)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		http.Error(w, "file unavailable", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", MimeType(it.Path))
	http.ServeContent(w, r, "", info.ModTime(), f)
}
