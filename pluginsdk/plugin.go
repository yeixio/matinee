// Package plugin is the Go SDK for Matinee server plugins.
//
// A server plugin is a WebAssembly module that Matinee runs in a sandbox. It
// can add a library kind (deciding what each file in such a library is), look
// up details for its own kinds of items, and hear about things that happen
// on the server, such as an item being added or someone starting to watch.
//
// Build a plugin as a WASI reactor:
//
//	GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o server.wasm .
//
// and register its handlers from an init function:
//
//	func init() {
//		plugin.Register(plugin.Handlers{Index: index})
//	}
//
//	func main() {}
//
// The plugin sees the library folders it indexes read-only, at the paths it
// is given, its own package read-only at PackageDir, and a folder of its own
// to keep things in at DataDir. It reaches the network only through Fetch,
// and only the hosts its manifest lists.
//
// The wire format between Matinee and a plugin is JSON over the plugin's
// memory; docs/plugins.md describes it for plugins written in other
// languages.
package plugin

// Where a plugin finds its own files inside the sandbox.
const (
	PackageDir = "/_matinee/plugin" // the plugin's package, read only
	DataDir    = "/_matinee/data"   // a folder the plugin keeps things in
)

// APIVersion is the plugin API this SDK speaks. A plugin's manifest says
// which one it was built for in its "api" field.
const APIVersion = 1

// Handlers are a plugin's answers to Matinee. Leave out the ones the plugin
// doesn't need.
type Handlers struct {
	// Index files one file of a library whose kind the plugin adds.
	Index func(IndexRequest) (IndexResult, error)
	// Lookup finds details for an item of a kind the plugin adds, when the
	// plugin's manifest lists that kind under metadata.
	Lookup func(LookupRequest) (*Details, error)
	// Event is told about things that happen on the server, for the events
	// the plugin's manifest lists under events.
	Event func(Event) error
}

var handlers Handlers

// Register sets the plugin's handlers. Call it from an init function.
func Register(h Handlers) { handlers = h }

// Library is the library a file belongs to.
type Library struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"`
}

// IndexRequest asks the plugin about one file.
type IndexRequest struct {
	Library Library `json:"library"`
	// Root is the library folder the file was found under, and Path the
	// file itself. Both can be opened with the os package.
	Root  string `json:"root"`
	Path  string `json:"path"`
	Size  int64  `json:"size"`
	Mtime int64  `json:"mtime"` // seconds since 1970
	// Config is what the admin filled in for the plugin's settings.
	Config map[string]string `json:"config,omitempty"`
}

// IndexResult is what a file is. Leave Item nil to skip the file, for
// example when it belongs to another file the plugin already listed.
type IndexResult struct {
	Item *Item `json:"item,omitempty"`
}

// Item describes a playable title. Kind is one of the item kinds in the
// plugin's manifest.
type Item struct {
	Kind      string `json:"kind"`
	Title     string `json:"title"`
	SortTitle string `json:"sortTitle,omitempty"`
	Year      int    `json:"year,omitempty"`
	Index     int    `json:"index,omitempty"` // a number to sort by under its parent
	Details
	// Parents are the containers the item sits in, outermost first, such as
	// a system and then a series. Each is made once and shared by every item
	// that names it.
	Parents []Parent `json:"parents,omitempty"`
	// Data is kept with the item and handed to the plugin's player, for
	// whatever the player needs to know, such as which emulator to load.
	Data map[string]any `json:"data,omitempty"`
}

// Details are what Matinee shows on a title's page.
type Details struct {
	Overview string   `json:"overview,omitempty"`
	Genres   []string `json:"genres,omitempty"`
	Rating   float64  `json:"rating,omitempty"` // out of 10
	// Facts are short labelled lines, such as Manufacturer: Namco.
	Facts []Fact `json:"facts,omitempty"`
	// Poster and Backdrop are a file path the plugin can see (in a library
	// folder or under PackageDir or DataDir) or an https URL.
	Poster   string `json:"poster,omitempty"`
	Backdrop string `json:"backdrop,omitempty"`
}

// Fact is one labelled line on a title's page.
type Fact struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// Parent is a container an item sits in.
type Parent struct {
	Kind     string `json:"kind"`
	Title    string `json:"title"`
	Year     int    `json:"year,omitempty"`
	Index    int    `json:"index,omitempty"`
	Poster   string `json:"poster,omitempty"`
	Backdrop string `json:"backdrop,omitempty"`
}

// LookupRequest asks for details of an item the plugin's kinds cover.
type LookupRequest struct {
	Kind   string            `json:"kind"`
	Title  string            `json:"title"`
	Year   int               `json:"year,omitempty"`
	Path   string            `json:"path,omitempty"`
	Data   map[string]any    `json:"data,omitempty"`
	Config map[string]string `json:"config,omitempty"`
}

// Event is something that happened on the server.
type Event struct {
	// Type is item.added, library.scanned, playback.started or
	// playback.stopped.
	Type string `json:"type"`
	// Time is when it happened, in milliseconds since 1970.
	Time    int64             `json:"time"`
	Item    *EventItem        `json:"item,omitempty"`
	Library *Library          `json:"library,omitempty"`
	User    *EventUser        `json:"user,omitempty"`
	Played  *EventPlay        `json:"played,omitempty"`
	Config  map[string]string `json:"config,omitempty"`
}

// EventItem is the item an event is about.
type EventItem struct {
	ID        int64  `json:"id"`
	LibraryID int64  `json:"libraryId"`
	Kind      string `json:"kind"`
	Title     string `json:"title"`
	Year      int    `json:"year,omitempty"`
	// For episodes: the show and season.
	ShowTitle string `json:"showTitle,omitempty"`
	Season    int    `json:"season,omitempty"`
	Episode   int    `json:"episode,omitempty"`
	TMDBID    int64  `json:"tmdbId,omitempty"`
}

// EventUser is who an event is about: an account or a profile.
type EventUser struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// EventPlay is how far someone got, for playback events.
type EventPlay struct {
	PositionMs int64 `json:"positionMs"`
	DurationMs int64 `json:"durationMs,omitempty"`
	Finished   bool  `json:"finished,omitempty"`
}
