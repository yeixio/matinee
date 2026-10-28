package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

// Dev runs a plugin's server part straight from the folder it's built in,
// the way a server runs it, for the matinee-plugin tool: what the plugin
// makes of each file in a folder, what it looks up, and how it takes events.
type Dev struct {
	Manifest Manifest
	rt       *runtime
	p        *Plugin
	data     string
}

// OpenDev loads the plugin in dir. Its data folder is data, and config is
// what an admin would fill in for its settings.
func OpenDev(ctx context.Context, dir, data string, config map[string]string, log *slog.Logger) (*Dev, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	if data, err = filepath.Abs(data); err != nil {
		return nil, err
	}
	b, err := os.ReadFile(filepath.Join(dir, "plugin.json"))
	if err != nil {
		return nil, err
	}
	man, err := ParseManifest(b)
	if err != nil {
		return nil, err
	}
	if man.Server == "" {
		return nil, errors.New("plugin.json names no server module")
	}
	wasm, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(man.Server)))
	if err != nil {
		return nil, fmt.Errorf("the server module: %w", err)
	}
	rt, err := newRuntime(ctx, "", &http.Client{CheckRedirect: noRedirectsElsewhere}, log)
	if err != nil {
		return nil, err
	}
	p := &Plugin{Manifest: man, dir: dir, config: config, enabled: true}
	if p.compiled, err = rt.compile(ctx, wasm); err != nil {
		rt.close(ctx)
		return nil, err
	}
	if err := os.MkdirAll(data, 0o755); err != nil {
		rt.close(ctx)
		return nil, err
	}
	return &Dev{Manifest: man, rt: rt, p: p, data: data}, nil
}

// Close stops the plugin.
func (d *Dev) Close(ctx context.Context) error { return d.rt.close(ctx) }

func (d *Dev) instance(ctx context.Context, roots ...string) (*instance, error) {
	ms := []mount{{host: d.p.dir, guest: packageDir}, {host: d.data, guest: dataDir, writable: true}}
	for _, r := range roots {
		ms = append(ms, mount{host: r, guest: guestPath(r)})
	}
	return d.rt.instantiate(ctx, d.p, ms)
}

// DevFile is what the plugin made of one file.
type DevFile struct {
	// Path is the file, relative to the folder scanned.
	Path string `json:"path"`
	// Item is the plugin's answer as it gave it, or nil when it skipped the
	// file.
	Item json.RawMessage `json:"item,omitempty"`
	// Problems are why a server wouldn't keep the item, or parts of it.
	Problems []string `json:"problems,omitempty"`
	// Error is the plugin's error for the file, or a crash.
	Error string `json:"error,omitempty"`
}

// Scan asks the plugin about every file in root that library kind would be
// asked about, in the order a server would, and hands each answer to fn.
// kind is one of the plugin's library kinds, or "" for its first.
func (d *Dev) Scan(ctx context.Context, kind, root string, fn func(DevFile)) error {
	k, ok := d.libraryKind(kind)
	if !ok {
		return fmt.Errorf("the plugin has no library kind %q", kind)
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	var files []string
	err = filepath.WalkDir(root, func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if strings.HasPrefix(e.Name(), ".") && p != root {
			if e.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if e.Type().IsRegular() && wants(k, p) {
			// A server never asks about empty files either.
			if info, err := e.Info(); err == nil && info.Size() > 0 {
				files = append(files, p)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	sort.Strings(files)
	in, err := d.instance(ctx, root)
	if err != nil {
		return err
	}
	defer func() { in.close(context.Background()) }()
	lib := libraryRef{ID: 1, Name: "Test library", Kind: k.Name}
	for _, f := range files {
		rel, _ := filepath.Rel(root, f)
		res := DevFile{Path: filepath.ToSlash(rel)}
		if in.mod.IsClosed() {
			// A crash ends the instance; a server starts a new one for the
			// next scan, so do the same for the next file.
			if in, err = d.instance(ctx, root); err != nil {
				return err
			}
		}
		info, err := os.Stat(f)
		if err != nil {
			return err
		}
		var raw struct {
			Item json.RawMessage `json:"item"`
		}
		err = in.call(ctx, "scan.index", indexParams{
			Library: lib, Root: guestPath(root), Path: guestPath(f),
			Size: info.Size(), Mtime: info.ModTime().Unix(), Config: d.p.configCopy(),
		}, &raw)
		switch {
		case err != nil:
			res.Error = err.Error()
		case len(raw.Item) > 0 && string(raw.Item) != "null":
			res.Item = raw.Item
			res.Problems = d.problems(in, k, raw.Item)
		}
		fn(res)
	}
	return nil
}

func (d *Dev) libraryKind(name string) (LibraryKind, bool) {
	for _, k := range d.Manifest.LibraryKinds {
		if name == "" || k.Name == name {
			return k, true
		}
	}
	return LibraryKind{}, false
}

func wants(k LibraryKind, path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return slices.ContainsFunc(k.Extensions, func(e string) bool { return strings.ToLower(e) == ext })
}

// problems are what a server would refuse or leave out of an item.
func (d *Dev) problems(in *instance, k LibraryKind, raw json.RawMessage) []string {
	var w wireItem
	if err := json.Unmarshal(raw, &w); err != nil {
		return []string{err.Error()}
	}
	var out []string
	if err := checkItem(&w, k, d.p.ItemKinds()); err != nil {
		out = append(out, err.Error())
	}
	out = append(out, artProblems(in, "poster", w.Poster)...)
	out = append(out, artProblems(in, "backdrop", w.Backdrop)...)
	for _, pa := range w.Parents {
		out = append(out, artProblems(in, pa.Kind+" poster", pa.Poster)...)
		out = append(out, artProblems(in, pa.Kind+" backdrop", pa.Backdrop)...)
	}
	return append(out, detailProblems(w.wireDetails)...)
}

func artProblems(in *instance, what, s string) []string {
	if s != "" && in.art(s) == "" {
		return []string{fmt.Sprintf("the %s %q is neither an https URL nor a file the plugin can see, so it won't show", what, s)}
	}
	return nil
}

func detailProblems(d wireDetails) []string {
	var out []string
	if len(d.Facts) > maxFacts {
		out = append(out, fmt.Sprintf("only the first %d of %d facts are kept", maxFacts, len(d.Facts)))
	}
	for _, f := range d.Facts {
		if strings.TrimSpace(f.Label) == "" || strings.TrimSpace(f.Value) == "" {
			out = append(out, fmt.Sprintf("the fact %q: %q is left out: facts need a label and a value", f.Label, f.Value))
		}
	}
	if d.Rating < 0 || d.Rating > 10 {
		out = append(out, fmt.Sprintf("the rating %v isn't out of 10", d.Rating))
	}
	return out
}

// DevLookup is what the plugin found for a title.
type DevLookup struct {
	// Details are the plugin's answer as it gave it, or nil for no match.
	Details  json.RawMessage `json:"details,omitempty"`
	Problems []string        `json:"problems,omitempty"`
}

// Lookup asks the plugin for details of a title, as a server does after a
// scan. data is what the plugin kept with the item when scanning.
func (d *Dev) Lookup(ctx context.Context, kind, title string, year int, data json.RawMessage) (DevLookup, error) {
	if !slices.Contains(d.Manifest.Metadata, kind) {
		return DevLookup{}, fmt.Errorf("plugin.json doesn't list %q under metadata, so a server never asks it to look one up", kind)
	}
	in, err := d.instance(ctx)
	if err != nil {
		return DevLookup{}, err
	}
	defer in.close(context.Background())
	var raw json.RawMessage
	err = in.call(ctx, "metadata.lookup", lookupParams{Kind: kind, Title: title, Year: year, Data: data, Config: d.p.configCopy()}, &raw)
	if err != nil || len(raw) == 0 {
		return DevLookup{}, err
	}
	res := DevLookup{Details: raw}
	var w wireDetails
	if err := json.Unmarshal(raw, &w); err != nil {
		res.Problems = []string{err.Error()}
		return res, nil
	}
	for _, s := range []string{w.Poster, w.Backdrop} {
		if s != "" && !strings.HasPrefix(s, "https://") {
			res.Problems = append(res.Problems, fmt.Sprintf("%q: looked-up pictures must be https URLs (a lookup doesn't see library folders)", s))
		}
	}
	res.Problems = append(res.Problems, detailProblems(w)...)
	return res, nil
}

// Event sends the plugin an event, as a server does for the events its
// manifest lists.
func (d *Dev) Event(ctx context.Context, e Event) error {
	if !slices.Contains(d.Manifest.Events, e.Type) {
		return fmt.Errorf("plugin.json doesn't list %q under events, so a server never sends it", e.Type)
	}
	in, err := d.instance(ctx)
	if err != nil {
		return err
	}
	defer in.close(context.Background())
	e.Config = d.p.configCopy()
	return in.call(ctx, "event", e, nil)
}

// HostPath finds the file behind a path the plugin gave, such as a poster:
// one in its package, its data folder, or one of the folders roots.
func (d *Dev) HostPath(guest string, roots ...string) (string, bool) {
	in := &instance{mounts: []mount{{host: d.p.dir, guest: packageDir}, {host: d.data, guest: dataDir}}}
	for _, r := range roots {
		if abs, err := filepath.Abs(r); err == nil {
			in.mounts = append(in.mounts, mount{host: abs, guest: guestPath(abs)})
		}
	}
	return in.hostPath(guest)
}
