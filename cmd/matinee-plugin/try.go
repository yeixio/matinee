package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/yeixio/matinee/internal/plugin"
)

func runTry(args []string) error {
	fs := flags("try", "[dir] <folder>")
	addr := fs.String("addr", "127.0.0.1:7390", "where to listen")
	kind := fs.String("kind", "", "the library kind to scan as (default the plugin's first)")
	verbose := fs.Bool("v", false, "show the plugin's debug log lines")
	cfg := settings{}
	fs.Var(cfg, "set", "a setting, as key=value; can be repeated")
	if err := fs.Parse(args); err != nil {
		return err
	}
	dir, rest, err := dirAnd(fs, 1)
	if err != nil {
		return err
	}
	t, err := newTry(dir, rest[0], *kind, cfg, *verbose)
	if err != nil {
		return err
	}
	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Open http://%s to browse %s and play it with the plugin's player.\n", ln.Addr(), rest[0])
	fmt.Fprintln(stdout, "The page rescans and reloads the player's files each time, so rebuild and refresh. Ctrl-C stops.")
	return http.Serve(ln, t)
}

// try stands in for a Matinee server and its web player: it scans a folder
// with the plugin, lists what it found, and plays items with the plugin's
// player in the same sandboxed frame, answering the player's requests the
// way the web player does. What the player stores is kept in
// .matinee-dev/storage in the plugin's folder.
type try struct {
	dir, folder, kind string
	shown             string // the folder as it was given, for the page
	config            map[string]string
	verbose           bool
	mux               *http.ServeMux

	mu    sync.Mutex
	files map[int64]string // the last scan's items, by id
}

//go:embed try.html
var tryPage []byte

func newTry(dir, given, kind string, config map[string]string, verbose bool) (*try, error) {
	folder, err := filepath.Abs(given)
	if err != nil {
		return nil, err
	}
	if info, err := os.Stat(folder); err != nil || !info.IsDir() {
		return nil, fmt.Errorf("%s isn't a folder", folder)
	}
	t := &try{dir: dir, folder: folder, shown: given, kind: kind, config: config, verbose: verbose, mux: http.NewServeMux()}
	t.mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(tryPage)
	})
	t.mux.HandleFunc("GET /api/items", t.items)
	t.mux.HandleFunc("GET /api/art", t.art)
	t.mux.HandleFunc("GET /api/file/{id}", t.file)
	t.mux.HandleFunc("GET /api/library-file", t.libraryFile)
	t.mux.HandleFunc("GET /api/storage", t.storageList)
	t.mux.HandleFunc("GET /api/storage/{key...}", t.storageGet)
	t.mux.HandleFunc("PUT /api/storage/{key...}", t.storagePut)
	t.mux.HandleFunc("DELETE /api/storage/{key...}", t.storageDelete)
	t.mux.HandleFunc("POST /api/log", t.log)
	t.mux.HandleFunc("GET /plugin/{path...}", t.pluginFile)
	return t, nil
}

func (t *try) ServeHTTP(w http.ResponseWriter, r *http.Request) { t.mux.ServeHTTP(w, r) }

type tryItem struct {
	ID         int64           `json:"id"`
	LibraryID  int64           `json:"libraryId"`
	Path       string          `json:"path"`
	Kind       string          `json:"kind"`
	Title      string          `json:"title"`
	Year       int             `json:"year,omitempty"`
	Parents    []string        `json:"parents,omitempty"`
	Facts      json.RawMessage `json:"facts,omitempty"`
	PluginData json.RawMessage `json:"pluginData,omitempty"`
	Poster     string          `json:"poster,omitempty"`
	Player     string          `json:"player,omitempty"`
	Problems   []string        `json:"problems,omitempty"`
	Error      string          `json:"error,omitempty"`
}

// items scans the folder afresh, so a rebuilt server module shows at once.
func (t *try) items(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	d, err := open(ctx, t.dir, t.config, t.verbose)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err)
		return
	}
	defer d.Close(context.Background())
	players := map[string]string{}
	for _, p := range d.Manifest.Players {
		for _, k := range p.ItemKinds {
			players[k] = "/plugin/" + p.Entry
		}
	}
	var list []tryItem
	files := map[int64]string{}
	err = d.Scan(ctx, t.kind, t.folder, func(f plugin.DevFile) {
		if f.Item == nil && f.Error == "" {
			return
		}
		it := tryItem{ID: int64(len(list) + 1), LibraryID: 1, Path: f.Path, Problems: f.Problems, Error: f.Error}
		if f.Item != nil {
			var raw struct {
				Kind     string          `json:"kind"`
				Title    string          `json:"title"`
				Year     int             `json:"year"`
				Facts    json.RawMessage `json:"facts"`
				Data     json.RawMessage `json:"data"`
				Poster   string          `json:"poster"`
				Backdrop string          `json:"backdrop"`
				Parents  []struct {
					Title string `json:"title"`
				} `json:"parents"`
			}
			json.Unmarshal(f.Item, &raw)
			it.Kind, it.Title, it.Year, it.Facts, it.PluginData = raw.Kind, raw.Title, raw.Year, raw.Facts, raw.Data
			it.Player = players[raw.Kind]
			for _, p := range raw.Parents {
				it.Parents = append(it.Parents, p.Title)
			}
			switch {
			case strings.HasPrefix(raw.Poster, "https://"):
				it.Poster = raw.Poster
			case raw.Poster != "":
				if _, ok := d.HostPath(raw.Poster, t.folder); ok {
					it.Poster = "/api/art?path=" + url.QueryEscape(raw.Poster)
				}
			}
		}
		files[it.ID] = filepath.Join(t.folder, filepath.FromSlash(f.Path))
		list = append(list, it)
	})
	if err != nil {
		httpError(w, http.StatusInternalServerError, err)
		return
	}
	t.mu.Lock()
	t.files = files
	t.mu.Unlock()
	writeJSON(w, map[string]any{"plugin": d.Manifest.Name, "folder": t.shown, "items": list})
}

func (t *try) art(w http.ResponseWriter, r *http.Request) {
	d, err := open(r.Context(), t.dir, t.config, false)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err)
		return
	}
	defer d.Close(context.Background())
	p, ok := d.HostPath(r.URL.Query().Get("path"), t.folder)
	if !ok {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, p)
}

func (t *try) file(w http.ResponseWriter, r *http.Request) {
	var id int64
	fmt.Sscan(r.PathValue("id"), &id)
	t.mu.Lock()
	p, ok := t.files[id]
	t.mu.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}
	serveFile(w, r, p)
}

// libraryFile is another file in the folder, by its path there, if the
// library kind would be asked about it, as a server allows.
func (t *try) libraryFile(w http.ResponseWriter, r *http.Request) {
	rel := r.URL.Query().Get("path")
	if rel == "" || strings.HasPrefix(rel, "/") || strings.Contains(rel, `\`) || slices.Contains(strings.Split(rel, "/"), "..") {
		httpError(w, http.StatusBadRequest, errors.New("path must be relative to the library folder"))
		return
	}
	man, err := readManifest(t.dir)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err)
		return
	}
	ext := strings.ToLower(path.Ext(rel))
	wanted := false
	for _, k := range man.LibraryKinds {
		if (t.kind == "" || k.Name == t.kind) && slices.ContainsFunc(k.Extensions, func(e string) bool { return strings.ToLower(e) == ext }) {
			wanted = true
		}
		if t.kind == "" {
			break
		}
	}
	if !wanted {
		httpError(w, http.StatusNotFound, fmt.Errorf("%s: the library kind isn't asked about %s files, so a server won't hand it over", rel, ext))
		return
	}
	serveFile(w, r, filepath.Join(t.folder, filepath.FromSlash(rel)))
}

func serveFile(w http.ResponseWriter, r *http.Request, p string) {
	f, err := os.Open(p)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	http.ServeContent(w, r, "", info.ModTime(), f)
}

// pluginFile serves the player's files as a server does: only what's in a
// player's folder, never as this page's own origin.
func (t *try) pluginFile(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("path")
	man, err := readManifest(t.dir)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err)
		return
	}
	clean := path.Clean(name)
	ok := clean == man.Icon && man.Icon != ""
	for _, p := range man.Players {
		if strings.HasPrefix(clean, path.Dir(p.Entry)+"/") && !strings.Contains(name, "..") {
			ok = true
		}
	}
	if !ok {
		http.NotFound(w, r)
		return
	}
	h := w.Header()
	h.Set("Content-Security-Policy", "sandbox allow-scripts allow-pointer-lock allow-downloads")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Access-Control-Allow-Origin", "*")
	h.Set("Cross-Origin-Resource-Policy", "cross-origin")
	h.Set("Cache-Control", "no-cache")
	switch strings.ToLower(path.Ext(clean)) {
	case ".wasm":
		h.Set("Content-Type", "application/wasm")
	case ".js", ".mjs":
		h.Set("Content-Type", "text/javascript; charset=utf-8")
	}
	f, err := os.Open(filepath.Join(t.dir, filepath.FromSlash(clean)))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.IsDir() {
		http.NotFound(w, r)
		return
	}
	http.ServeContent(w, r, path.Base(clean), info.ModTime(), f)
}

// Storage, as a server keeps it for one person: the same keys and limits.
const (
	maxValue   = 32 << 20
	maxStorage = 512 << 20
)

var keyPattern = regexp.MustCompile(`^[A-Za-z0-9._-][A-Za-z0-9._/-]{0,199}$`)

func (t *try) storageDir() string { return filepath.Join(t.dir, devDir, "storage") }

func (t *try) storagePath(w http.ResponseWriter, r *http.Request) (string, bool) {
	key := r.PathValue("key")
	if !keyPattern.MatchString(key) || strings.Contains(key, "..") || strings.HasSuffix(key, "/") {
		httpError(w, http.StatusBadRequest, fmt.Errorf("%q isn't a storage key: letters, digits, . _ - and /, up to 200", key))
		return "", false
	}
	return filepath.Join(t.storageDir(), filepath.FromSlash(key)), true
}

type storedKey struct {
	Key       string `json:"key"`
	Size      int64  `json:"size"`
	UpdatedAt string `json:"updatedAt"`
}

func (t *try) stored() ([]storedKey, int64) {
	var keys []storedKey
	var total int64
	root := t.storageDir()
	filepath.WalkDir(root, func(p string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() {
			return nil
		}
		info, err := e.Info()
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		keys = append(keys, storedKey{Key: filepath.ToSlash(rel), Size: info.Size(), UpdatedAt: info.ModTime().UTC().Format("2006-01-02T15:04:05Z")})
		total += info.Size()
		return nil
	})
	return keys, total
}

func (t *try) storageList(w http.ResponseWriter, r *http.Request) {
	prefix := r.URL.Query().Get("prefix")
	all, total := t.stored()
	keys := []storedKey{}
	for _, k := range all {
		if strings.HasPrefix(k.Key, prefix) {
			keys = append(keys, k)
		}
	}
	writeJSON(w, map[string]any{"keys": keys, "used": total, "limit": maxStorage})
}

func (t *try) storageGet(w http.ResponseWriter, r *http.Request) {
	if p, ok := t.storagePath(w, r); ok {
		serveFile(w, r, p)
	}
}

func (t *try) storagePut(w http.ResponseWriter, r *http.Request) {
	p, ok := t.storagePath(w, r)
	if !ok {
		return
	}
	b, err := io.ReadAll(io.LimitReader(r.Body, maxValue+1))
	if err != nil {
		httpError(w, http.StatusBadRequest, err)
		return
	}
	if len(b) > maxValue {
		httpError(w, http.StatusRequestEntityTooLarge, errors.New("a value can be up to 32 MB"))
		return
	}
	_, total := t.stored()
	if old, err := os.Stat(p); err == nil {
		total -= old.Size()
	}
	if total+int64(len(b)) > maxStorage {
		httpError(w, http.StatusRequestEntityTooLarge, errors.New("a plugin can keep up to 512 MB for each person"))
		return
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		httpError(w, http.StatusInternalServerError, err)
		return
	}
	if err := os.WriteFile(p, b, 0o644); err != nil {
		httpError(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (t *try) storageDelete(w http.ResponseWriter, r *http.Request) {
	if p, ok := t.storagePath(w, r); ok {
		os.Remove(p)
		w.WriteHeader(http.StatusNoContent)
	}
}

// log prints what the player asked for, so it shows next to the plugin's
// own log lines.
func (t *try) log(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(io.LimitReader(r.Body, 4096))
	fmt.Fprintf(stdout, "player: %s\n", b)
	w.WriteHeader(http.StatusNoContent)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func httpError(w http.ResponseWriter, status int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}
