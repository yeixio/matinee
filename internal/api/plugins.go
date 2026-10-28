package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/yeixio/matinee/internal/plugin"
	"github.com/yeixio/matinee/internal/scan"
	"github.com/yeixio/matinee/internal/store"
)

// Plugins (internal/plugin, docs/plugins.md). Admins install them from a
// link, a file or the catalog of official plugins, look at what each may
// reach, and turn them on. Players load in a sandboxed frame (see
// pluginFile); what they keep per person goes in plugin storage.

// What a plugin's player may keep for each person.
const (
	maxPluginValue   = 32 << 20
	maxPluginStorage = 512 << 20
)

var pluginKeyPattern = regexp.MustCompile(`^[A-Za-z0-9._-][A-Za-z0-9._/-]{0,199}$`)

func (s *Server) listPlugins(w http.ResponseWriter, r *http.Request) {
	if s.Plugins == nil {
		writeJSON(w, http.StatusOK, []plugin.View{})
		return
	}
	writeJSON(w, http.StatusOK, s.Plugins.List())
}

func (s *Server) installPlugin(w http.ResponseWriter, r *http.Request) {
	if s.Plugins == nil {
		writeError(w, http.StatusNotFound, "plugins are off on this server")
		return
	}
	var pkg []byte
	var err error
	if ct, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); ct == "application/json" {
		var req struct {
			URL string `json:"url"`
		}
		if err := readJSON(r, &req); err != nil || strings.TrimSpace(req.URL) == "" {
			writeError(w, http.StatusBadRequest, "invalid request")
			return
		}
		pkg, err = s.Plugins.Download(r.Context(), strings.TrimSpace(req.URL))
	} else {
		pkg, err = io.ReadAll(http.MaxBytesReader(w, r.Body, plugin.MaxPackageSize))
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			err = plugin.ErrPackageTooBig
		}
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, "couldn't download the plugin: "+err.Error())
		return
	}
	v, err := s.Plugins.Install(r.Context(), pkg)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) updatePlugin(w http.ResponseWriter, r *http.Request) {
	if s.Plugins == nil {
		writeError(w, http.StatusNotFound, "no such plugin")
		return
	}
	var req struct {
		Enabled *bool             `json:"enabled"`
		Config  map[string]string `json:"config"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	id := r.PathValue("plugin")
	v, err := s.Plugins.Get(id)
	if req.Config != nil && err == nil {
		v, err = s.Plugins.SetConfig(r.Context(), id, req.Config)
	}
	if req.Enabled != nil && err == nil {
		v, err = s.Plugins.SetEnabled(r.Context(), id, *req.Enabled)
	}
	switch {
	case errors.Is(err, plugin.ErrNotFound):
		writeError(w, http.StatusNotFound, "no such plugin")
	case errors.Is(err, plugin.ErrNotSupported):
		writeError(w, http.StatusBadRequest, "this plugin was made for a different version of Matinee")
	case err != nil:
		s.serverError(w, err)
	default:
		writeJSON(w, http.StatusOK, v)
	}
}

func (s *Server) removePlugin(w http.ResponseWriter, r *http.Request) {
	if s.Plugins == nil {
		writeError(w, http.StatusNotFound, "no such plugin")
		return
	}
	err := s.Plugins.Remove(r.Context(), r.PathValue("plugin"))
	switch {
	case errors.Is(err, plugin.ErrNotFound):
		writeError(w, http.StatusNotFound, "no such plugin")
	case err != nil:
		s.serverError(w, err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// catalogEntry is an official plugin, as the catalog lists it.
type catalogEntry struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Version     string `json:"version"`
	URL         string `json:"url"`
	Homepage    string `json:"homepage,omitempty"`
	// Installed is the version installed here, if any.
	Installed string `json:"installed,omitempty"`
}

func (s *Server) pluginCatalog(w http.ResponseWriter, r *http.Request) {
	if s.Plugins == nil || s.PluginCatalog == "" {
		writeJSON(w, http.StatusOK, []catalogEntry{})
		return
	}
	entries, err := cached(&s.catalogCache, "catalog", time.Hour, func() ([]catalogEntry, error) {
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.PluginCatalog, nil)
		if err != nil {
			return nil, err
		}
		res, err := s.Plugins.Client.Do(req)
		if err != nil {
			return nil, err
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			return nil, errors.New(res.Status)
		}
		var c struct {
			Plugins []catalogEntry `json:"plugins"`
		}
		if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&c); err != nil {
			return nil, err
		}
		return c.Plugins, nil
	})
	if err != nil {
		writeError(w, http.StatusBadGateway, "couldn't load the list of plugins")
		return
	}
	installed := map[string]string{}
	for _, p := range s.Plugins.List() {
		installed[p.ID] = p.Version
	}
	out := []catalogEntry{}
	for _, e := range entries {
		e.Installed = installed[e.ID]
		out = append(out, e)
	}
	writeJSON(w, http.StatusOK, out)
}

// libraryKindView is a kind of library the add-library form offers.
type libraryKindView struct {
	Name        string `json:"name"`
	Label       string `json:"label,omitempty"` // a plugin's; built-in kinds are labelled by the client
	Description string `json:"description,omitempty"`
	Plugin      string `json:"plugin,omitempty"`
}

func (s *Server) listLibraryKinds(w http.ResponseWriter, r *http.Request) {
	out := []libraryKindView{}
	var fromPlugins []plugin.LibraryKindView
	if s.Plugins != nil {
		fromPlugins = s.Plugins.LibraryKinds()
	}
	for _, name := range scan.KindNames() {
		v := libraryKindView{Name: name}
		if i := slices.IndexFunc(fromPlugins, func(k plugin.LibraryKindView) bool { return k.Name == name }); i >= 0 {
			v.Label, v.Description, v.Plugin = fromPlugins[i].Label, fromPlugins[i].Description, fromPlugins[i].Plugin
		}
		out = append(out, v)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) listPlayers(w http.ResponseWriter, r *http.Request) {
	if s.Plugins == nil {
		writeJSON(w, http.StatusOK, []plugin.PlayerView{})
		return
	}
	writeJSON(w, http.StatusOK, s.Plugins.Players())
}

// pluginFile serves a plugin's player files and icon. They're code from the
// plugin, so they never run as this site: the CSP sandbox gives the page an
// origin of its own even when it's opened on its own, so it can't read the
// session cookie or call the API as whoever is signed in. Players get
// what they need from the web player through postMessage instead.
func (s *Server) pluginFile(w http.ResponseWriter, r *http.Request) {
	s.servePluginFile(w, r, r.PathValue("plugin"), r.PathValue("path"))
}

func (s *Server) servePluginFile(w http.ResponseWriter, r *http.Request, id, name string) {
	if s.Plugins == nil {
		http.NotFound(w, r)
		return
	}
	file, ok := s.Plugins.File(id, name)
	if !ok {
		http.NotFound(w, r)
		return
	}
	h := w.Header()
	h.Set("Content-Security-Policy", "sandbox allow-scripts allow-pointer-lock allow-downloads")
	h.Set("X-Content-Type-Options", "nosniff")
	// The sandboxed page has no origin, so loading its own scripts and
	// WebAssembly is a cross-origin request.
	h.Set("Access-Control-Allow-Origin", "*")
	h.Set("Cross-Origin-Resource-Policy", "cross-origin")
	h.Set("Cache-Control", "no-cache")
	if ext := strings.ToLower(path.Ext(name)); ext == ".wasm" {
		h.Set("Content-Type", "application/wasm")
	} else if ext == ".js" || ext == ".mjs" {
		h.Set("Content-Type", "text/javascript; charset=utf-8")
	}
	// Not http.ServeFile, which sends .../index.html off to the folder.
	f, err := os.Open(file)
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
	http.ServeContent(w, r, path.Base(name), info.ModTime(), f)
}

// pluginRunning checks the plugin in the path is running and returns its id.
func (s *Server) pluginRunning(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := r.PathValue("plugin")
	if s.Plugins == nil {
		writeError(w, http.StatusNotFound, "no such plugin")
		return "", false
	}
	if v, err := s.Plugins.Get(id); err != nil || !v.Running {
		writeError(w, http.StatusNotFound, "no such plugin")
		return "", false
	}
	return id, true
}

func (s *Server) listPluginStorage(w http.ResponseWriter, r *http.Request) {
	id, ok := s.pluginRunning(w, r)
	if !ok {
		return
	}
	keys, total, err := s.Store.PluginKeys(r.Context(), id, currentUser(r).ID, r.URL.Query().Get("prefix"))
	if err != nil {
		s.serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"keys": keys, "used": total, "limit": maxPluginStorage})
}

func storageKey(w http.ResponseWriter, r *http.Request) (string, bool) {
	key := r.PathValue("key")
	if !pluginKeyPattern.MatchString(key) || strings.Contains(key, "..") {
		writeError(w, http.StatusBadRequest, "invalid request")
		return "", false
	}
	return key, true
}

func (s *Server) getPluginValue(w http.ResponseWriter, r *http.Request) {
	id, ok := s.pluginRunning(w, r)
	if !ok {
		return
	}
	key, ok := storageKey(w, r)
	if !ok {
		return
	}
	v, err := s.Store.PluginValue(r.Context(), id, currentUser(r).ID, key)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "nothing saved under that name")
		return
	}
	if err != nil {
		s.serverError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(v)
}

func (s *Server) putPluginValue(w http.ResponseWriter, r *http.Request) {
	id, ok := s.pluginRunning(w, r)
	if !ok {
		return
	}
	key, ok := storageKey(w, r)
	if !ok {
		return
	}
	b, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxPluginValue))
	if err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "that's too large to save")
		return
	}
	user := currentUser(r).ID
	keys, total, err := s.Store.PluginKeys(r.Context(), id, user, key)
	if err != nil {
		s.serverError(w, err)
		return
	}
	for _, k := range keys {
		if k.Key == key {
			total -= k.Size
		}
	}
	if total+int64(len(b)) > maxPluginStorage {
		writeError(w, http.StatusRequestEntityTooLarge, "that's too large to save")
		return
	}
	if err := s.Store.SetPluginValue(r.Context(), id, user, key, b); err != nil {
		s.serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) deletePluginValue(w http.ResponseWriter, r *http.Request) {
	id, ok := s.pluginRunning(w, r)
	if !ok {
		return
	}
	key, ok := storageKey(w, r)
	if !ok {
		return
	}
	if err := s.Store.DeletePluginValue(r.Context(), id, currentUser(r).ID, key); err != nil {
		s.serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// pluginLibraryFile serves another file of a plugin's library to its
// player, such as the parent set a game needs. Only files the plugin's
// library kind would index are served, from libraries of that kind the
// person can see.
func (s *Server) pluginLibraryFile(w http.ResponseWriter, r *http.Request) {
	id, ok := s.pluginRunning(w, r)
	if !ok {
		return
	}
	libID, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	lib, err := s.Store.Library(r.Context(), libID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "no such library")
		return
	}
	if err != nil {
		s.serverError(w, err)
		return
	}
	visible, err := s.Store.LibraryVisible(r.Context(), currentUser(r), lib.ID)
	if err != nil {
		s.serverError(w, err)
		return
	}
	owner, _ := s.Plugins.LibraryKindPlugin(lib.Kind)
	kind, registered := scan.KindByName(lib.Kind)
	if !visible || owner != id || !registered {
		writeError(w, http.StatusNotFound, "no such library")
		return
	}
	rel := r.URL.Query().Get("path")
	if rel == "" || strings.HasPrefix(rel, "/") || strings.Contains(rel, `\`) || slices.Contains(strings.Split(rel, "/"), "..") {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	for _, root := range lib.Paths {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if !kind.Wants(p) {
			break
		}
		f, err := os.Open(p)
		if err != nil {
			continue
		}
		info, err := f.Stat()
		if err != nil || !info.Mode().IsRegular() {
			f.Close()
			continue
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		http.ServeContent(w, r, "", info.ModTime(), f)
		f.Close()
		return
	}
	writeError(w, http.StatusNotFound, "that file isn't in this library")
}
