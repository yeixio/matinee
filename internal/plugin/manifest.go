// Package plugin installs and runs plugins: library kinds, details and
// event handlers in a sandboxed WebAssembly module (wazero), and players
// that the web player loads in a sandboxed frame. docs/guide/plugins describes
// the package format and the API.
package plugin

import (
	"encoding/json"
	"fmt"
	"net"
	"regexp"
	"slices"
	"strings"
)

// APIVersions are the plugin API versions this server runs. A plugin built
// for another one is listed but can't be turned on.
var APIVersions = []int{1}

// Manifest is a plugin's plugin.json.
type Manifest struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	API         int    `json:"api"`
	Description string `json:"description,omitempty"`
	Author      string `json:"author,omitempty"`
	License     string `json:"license,omitempty"`
	Homepage    string `json:"homepage,omitempty"`
	// Icon is an image in the package, shown in Settings.
	Icon string `json:"icon,omitempty"`

	// Server is the WebAssembly module in the package, if the plugin has one.
	Server string `json:"server,omitempty"`
	// LibraryKinds are the kinds of library the plugin adds. They need Server.
	LibraryKinds []LibraryKind `json:"libraryKinds,omitempty"`
	// Metadata lists item kinds the plugin looks up details for. They must
	// be kinds the plugin adds; it can't take over movies or music.
	Metadata []string `json:"metadata,omitempty"`
	// Events lists the events the plugin wants to hear about.
	Events []string `json:"events,omitempty"`
	// Players are pages the web player shows to play items of some kinds.
	Players []Player `json:"players,omitempty"`
	// Settings are what an admin can fill in for the plugin, such as an
	// API key. The plugin gets them with every call.
	Settings []Setting `json:"settings,omitempty"`

	Permissions Permissions `json:"permissions,omitempty"`
}

// LibraryKind is a kind of library a plugin adds.
type LibraryKind struct {
	Name  string `json:"name"`  // such as arcade; must be unique on the server
	Label string `json:"label"` // shown when adding a library, such as Arcade
	// Description says what goes in such a library, under the kind's name
	// when adding one.
	Description string `json:"description,omitempty"`
	// Extensions are the file extensions the plugin is asked about, such as
	// .zip. Other files are skipped without asking.
	Extensions []string `json:"extensions"`
	// ItemKinds are the kinds of item the plugin files, playable ones first.
	ItemKinds []string `json:"itemKinds"`
}

// Player is a page that plays items of some kinds.
type Player struct {
	ItemKinds []string `json:"itemKinds"`
	// Entry is the page in the package, such as player/index.html. Every
	// file under its folder is served to the web player.
	Entry string `json:"entry"`
}

// Setting is one thing an admin can fill in for a plugin.
type Setting struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
	Secret      bool   `json:"secret,omitempty"` // shown as a password field and never sent back
}

// Permissions are what a plugin may reach beyond its own package.
type Permissions struct {
	// Network lists the hosts the plugin's server part may fetch from, over
	// https. A leading "*." allows every subdomain.
	Network []string `json:"network,omitempty"`
}

// Events a plugin can hear about.
const (
	EventItemAdded       = "item.added"
	EventLibraryScanned  = "library.scanned"
	EventPlaybackStarted = "playback.started"
	EventPlaybackStopped = "playback.stopped"
)

var knownEvents = []string{EventItemAdded, EventLibraryScanned, EventPlaybackStarted, EventPlaybackStopped}

var (
	idPattern   = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,39}$`)
	kindPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{1,30}$`)
	hostPattern = regexp.MustCompile(`^(\*\.)?([a-z0-9]([a-z0-9-]*[a-z0-9])?\.)+[a-z]{2,}$`)
	keyPattern  = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]{0,39}$`)
)

// builtinKinds are the library and item kinds Matinee itself has, which
// plugins can't add again.
var builtinKinds = []string{
	"movies", "tv", "music",
	"movie", "show", "season", "episode", "artist", "album", "track", "trailer", "collection", "person",
}

// ParseManifest reads and checks a plugin.json.
func ParseManifest(b []byte) (Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return m, fmt.Errorf("plugin.json: %w", err)
	}
	return m, m.check()
}

func (m Manifest) check() error {
	switch {
	case !idPattern.MatchString(m.ID):
		return fmt.Errorf("plugin.json: id %q must be 2 to 40 lowercase letters, digits and dashes", m.ID)
	case strings.TrimSpace(m.Name) == "":
		return fmt.Errorf("plugin.json: name is missing")
	case m.Version == "":
		return fmt.Errorf("plugin.json: version is missing")
	case m.API < 1:
		return fmt.Errorf("plugin.json: api is missing")
	case m.Server == "" && (len(m.LibraryKinds) > 0 || len(m.Metadata) > 0 || len(m.Events) > 0):
		return fmt.Errorf("plugin.json: libraryKinds, metadata and events need a server module")
	case m.Server == "" && len(m.Players) == 0:
		return fmt.Errorf("plugin.json: a plugin needs a server module, a player or both")
	}
	for _, p := range append([]string{m.Server, m.Icon}, entries(m.Players)...) {
		if p != "" && !safePath(p) {
			return fmt.Errorf("plugin.json: %q isn't a path inside the package", p)
		}
	}
	own := map[string]bool{}
	for _, k := range m.LibraryKinds {
		if !kindPattern.MatchString(k.Name) || slices.Contains(builtinKinds, k.Name) {
			return fmt.Errorf("plugin.json: library kind %q can't be used", k.Name)
		}
		if strings.TrimSpace(k.Label) == "" {
			return fmt.Errorf("plugin.json: library kind %q needs a label", k.Name)
		}
		if len(k.Extensions) == 0 || len(k.ItemKinds) == 0 {
			return fmt.Errorf("plugin.json: library kind %q needs extensions and itemKinds", k.Name)
		}
		for _, e := range k.Extensions {
			if !strings.HasPrefix(e, ".") || len(e) < 2 || strings.ContainsAny(e, `/\`) {
				return fmt.Errorf("plugin.json: extension %q must look like .zip", e)
			}
		}
		for _, ik := range k.ItemKinds {
			if !kindPattern.MatchString(ik) || slices.Contains(builtinKinds, ik) {
				return fmt.Errorf("plugin.json: item kind %q can't be used", ik)
			}
			own[ik] = true
		}
	}
	for _, k := range m.Metadata {
		if !own[k] {
			return fmt.Errorf("plugin.json: metadata kind %q isn't one of the plugin's item kinds", k)
		}
	}
	for _, e := range m.Events {
		if !slices.Contains(knownEvents, e) {
			return fmt.Errorf("plugin.json: unknown event %q (known: %s)", e, strings.Join(knownEvents, ", "))
		}
	}
	for _, p := range m.Players {
		if len(p.ItemKinds) == 0 || p.Entry == "" {
			return fmt.Errorf("plugin.json: a player needs itemKinds and an entry")
		}
		for _, k := range p.ItemKinds {
			if !kindPattern.MatchString(k) || slices.Contains(builtinKinds, k) {
				return fmt.Errorf("plugin.json: players can't play %q items", k)
			}
		}
	}
	for _, s := range m.Settings {
		if !keyPattern.MatchString(s.Key) || strings.TrimSpace(s.Label) == "" {
			return fmt.Errorf("plugin.json: setting %q needs a key of letters and digits and a label", s.Key)
		}
	}
	for _, h := range m.Permissions.Network {
		if !hostPattern.MatchString(h) || net.ParseIP(h) != nil {
			return fmt.Errorf("plugin.json: network host %q must be a domain name, like example.com or *.example.com", h)
		}
	}
	return nil
}

func entries(ps []Player) []string {
	var out []string
	for _, p := range ps {
		out = append(out, p.Entry)
	}
	return out
}

// Supported reports whether this server runs the plugin's API version.
func (m Manifest) Supported() bool { return slices.Contains(APIVersions, m.API) }

// ItemKinds lists every item kind the plugin's library kinds file.
func (m Manifest) ItemKinds() []string {
	var out []string
	for _, k := range m.LibraryKinds {
		out = append(out, k.ItemKinds...)
	}
	return out
}

// AllowsHost reports whether the plugin may fetch from host.
func (p Permissions) AllowsHost(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	for _, h := range p.Network {
		if h == host || strings.HasPrefix(h, "*.") && strings.HasSuffix(host, h[1:]) {
			return true
		}
	}
	return false
}

// safePath reports whether p is a relative path that stays inside the package.
func safePath(p string) bool {
	if p == "" || strings.HasPrefix(p, "/") || strings.Contains(p, `\`) || strings.Contains(p, ":") {
		return false
	}
	for _, part := range strings.Split(p, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}
