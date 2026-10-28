package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/yeixio/matinee/internal/metadata"
	"github.com/yeixio/matinee/internal/probe"
	"github.com/yeixio/matinee/internal/scan"
	"github.com/yeixio/matinee/internal/store"
)

// libraryKind is a library kind a plugin adds. Each scan gets its own
// instance of the plugin, which sees that library's folders.
type libraryKind struct {
	m *Manager
	p *Plugin
	k LibraryKind

	mu       sync.Mutex
	sessions map[int64]*instance // by library, while a scan runs
}

var (
	_ scan.Unprobed = (*libraryKind)(nil)
	_ scan.Session  = (*libraryKind)(nil)
)

func (k *libraryKind) Name() string               { return k.k.Name }
func (k *libraryKind) Unprobed()                  {}
func (k *libraryKind) Playable(probe.Result) bool { return true }
func (k *libraryKind) Wants(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return slices.ContainsFunc(k.k.Extensions, func(e string) bool { return strings.ToLower(e) == ext })
}

// mounts are the library's folders, read only, where the plugin sees them,
// plus the plugin's own.
func (k *libraryKind) mounts(lib store.Library) []mount {
	roots := slices.Clone(lib.Paths)
	sort.Strings(roots)
	ms := k.m.baseMounts(k.p)
	for _, r := range roots {
		r = filepath.Clean(r)
		// A folder inside another one is already there.
		if slices.ContainsFunc(ms[2:], func(m mount) bool { return strings.HasPrefix(r, m.host+string(filepath.Separator)) || r == m.host }) {
			continue
		}
		ms = append(ms, mount{host: r, guest: guestPath(r)})
	}
	return ms
}

func (k *libraryKind) BeginScan(ctx context.Context, lib store.Library) func() {
	in, err := k.m.rt.instantiate(ctx, k.p, k.mounts(lib))
	if err != nil {
		k.m.Log.Warn("plugin can't scan", "plugin", k.p.ID, "library", lib.Name, "err", err)
		return func() {}
	}
	k.mu.Lock()
	k.sessions[lib.ID] = in
	k.mu.Unlock()
	return func() {
		k.mu.Lock()
		if k.sessions[lib.ID] == in {
			delete(k.sessions, lib.ID)
		}
		k.mu.Unlock()
		in.close(context.Background())
	}
}

type indexParams struct {
	Library libraryRef        `json:"library"`
	Root    string            `json:"root"`
	Path    string            `json:"path"`
	Size    int64             `json:"size"`
	Mtime   int64             `json:"mtime"`
	Config  map[string]string `json:"config,omitempty"`
}

type libraryRef struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"`
}

type indexResult struct {
	Item *wireItem `json:"item"`
}

type wireItem struct {
	Kind      string `json:"kind"`
	Title     string `json:"title"`
	SortTitle string `json:"sortTitle"`
	Year      int    `json:"year"`
	Index     int    `json:"index"`
	wireDetails
	Parents []wireParent    `json:"parents"`
	Data    json.RawMessage `json:"data"`
}

type wireDetails struct {
	Overview string       `json:"overview"`
	Genres   []string     `json:"genres"`
	Rating   float64      `json:"rating"`
	Facts    []store.Fact `json:"facts"`
	Poster   string       `json:"poster"`
	Backdrop string       `json:"backdrop"`
}

type wireParent struct {
	Kind     string `json:"kind"`
	Title    string `json:"title"`
	Year     int    `json:"year"`
	Index    int    `json:"index"`
	Poster   string `json:"poster"`
	Backdrop string `json:"backdrop"`
}

const (
	maxFacts    = 20
	maxDataSize = 64 << 10
)

func (k *libraryKind) Index(ctx context.Context, st *store.Store, f scan.File) (store.Item, error) {
	k.mu.Lock()
	in := k.sessions[f.Library.ID]
	k.mu.Unlock()
	if in == nil {
		// Not inside a scan, or the plugin couldn't start for it.
		return store.Item{}, errors.New("the plugin isn't running for this library")
	}
	var res indexResult
	err := in.call(ctx, "scan.index", indexParams{
		Library: libraryRef{f.Library.ID, f.Library.Name, f.Library.Kind},
		Root:    guestPath(f.Root), Path: guestPath(f.Path),
		Size: f.Info.Size(), Mtime: f.Info.ModTime().Unix(),
		Config: k.p.configCopy(),
	}, &res)
	if err != nil {
		return store.Item{}, err
	}
	w := res.Item
	if w == nil {
		return store.Item{}, scan.ErrSkip
	}
	own := k.p.ItemKinds()
	if !slices.Contains(k.k.ItemKinds, w.Kind) || strings.TrimSpace(w.Title) == "" {
		return store.Item{}, fmt.Errorf("the plugin filed %q as a %q item, which its library kind doesn't have", f.Path, w.Kind)
	}
	var parentID *int64
	for _, pa := range w.Parents {
		if !slices.Contains(own, pa.Kind) || strings.TrimSpace(pa.Title) == "" {
			return store.Item{}, fmt.Errorf("the plugin put %q in a %q, which isn't one of its item kinds", f.Path, pa.Kind)
		}
		id, err := st.EnsureContainer(ctx, f.Library.ID, parentID, pa.Kind, clip(pa.Title, 500), nonZero(pa.Index), nonZero(pa.Year))
		if err != nil {
			return store.Item{}, err
		}
		poster, backdrop := in.art(pa.Poster), in.art(pa.Backdrop)
		if poster != "" || backdrop != "" {
			if err := st.SetArt(ctx, id, poster, backdrop); err != nil {
				return store.Item{}, err
			}
		}
		parentID = &id
	}
	it := store.Item{
		Kind: w.Kind, ParentID: parentID, Title: clip(w.Title, 500), Year: nonZero(w.Year), IndexNumber: nonZero(w.Index),
		Poster: in.art(w.Poster), Backdrop: in.art(w.Backdrop),
		Facts: cleanFacts(w.Facts),
	}
	if len(w.Data) > 0 && string(w.Data) != "null" {
		if len(w.Data) > maxDataSize || !json.Valid(w.Data) {
			return store.Item{}, fmt.Errorf("the plugin's data for %q is too large or not JSON", f.Path)
		}
		it.PluginData = w.Data
	}
	if d := w.wireDetails; d.Overview != "" || len(d.Genres) > 0 || d.Rating > 0 {
		it.Details = &store.Metadata{Overview: clip(d.Overview, 10000), Genres: clipAll(d.Genres, 20), Rating: d.Rating}
	}
	return it, nil
}

// art turns a picture the plugin named into one Matinee can show: a file
// in a folder the plugin sees, or an https URL.
func (in *instance) art(s string) string {
	switch {
	case s == "":
		return ""
	case strings.HasPrefix(s, "https://"):
		return s
	}
	if p, ok := in.hostPath(s); ok {
		return p
	}
	return ""
}

func cleanFacts(fs []store.Fact) []store.Fact {
	var out []store.Fact
	for _, f := range fs {
		if l, v := strings.TrimSpace(f.Label), strings.TrimSpace(f.Value); l != "" && v != "" && len(out) < maxFacts {
			out = append(out, store.Fact{Label: clip(l, 100), Value: clip(v, 500)})
		}
	}
	return out
}

func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	r := []rune(s)
	for len(string(r)) > n {
		r = r[:len(r)-1]
	}
	return string(r)
}

func clipAll(ss []string, n int) []string {
	var out []string
	for _, s := range ss {
		if s = clip(s, 100); s != "" && len(out) < n {
			out = append(out, s)
		}
	}
	return out
}

func nonZero(n int) *int {
	if n == 0 {
		return nil
	}
	return &n
}

// provider looks up details for a plugin's item kinds.
type provider struct {
	m *Manager
	p *Plugin
}

func (pr *provider) Name() string    { return pr.p.ID }
func (pr *provider) Kinds() []string { return pr.p.Metadata }

type lookupParams struct {
	Kind   string            `json:"kind"`
	Title  string            `json:"title"`
	Year   int               `json:"year,omitempty"`
	Path   string            `json:"path,omitempty"`
	Data   json.RawMessage   `json:"data,omitempty"`
	Config map[string]string `json:"config,omitempty"`
}

func (pr *provider) Lookup(ctx context.Context, q metadata.Query) (*store.Metadata, error) {
	in, err := pr.m.serviceInstance(ctx, pr.p)
	if err != nil {
		return nil, err
	}
	var d *wireDetails
	params := lookupParams{Kind: q.Kind, Title: q.Title, Year: q.Year, Data: q.Data, Config: pr.p.configCopy()}
	if q.Path != "" {
		params.Path = guestPath(q.Path)
	}
	if err := in.call(ctx, "metadata.lookup", params, &d); err != nil {
		return nil, err
	}
	if d == nil {
		return nil, nil
	}
	// Only URLs: the lookup instance doesn't see library folders.
	poster, backdrop := d.Poster, d.Backdrop
	if !strings.HasPrefix(poster, "https://") {
		poster = in.art(poster)
	}
	if !strings.HasPrefix(backdrop, "https://") {
		backdrop = in.art(backdrop)
	}
	return &store.Metadata{
		Overview: clip(d.Overview, 10000), Genres: clipAll(d.Genres, 20), Rating: d.Rating,
		Poster: poster, Backdrop: backdrop, Facts: cleanFacts(d.Facts),
	}, nil
}
