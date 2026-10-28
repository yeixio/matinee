// Command stories is the server part of the Stories plugin, the example in
// Matinee's developer guide: it files text and Markdown files as stories,
// with a title, an overview and a reading time, in a book for each
// folder.
package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	plugin "github.com/yeixio/matinee/pluginsdk"
)

func init() {
	plugin.Register(plugin.Handlers{Index: index, Event: event})
}

func main() {}

// wordsPerMinute is how fast the reading time assumes people read.
const wordsPerMinute = 230

func index(r plugin.IndexRequest) (plugin.IndexResult, error) {
	f, err := os.Open(r.Path)
	if err != nil {
		return plugin.IndexResult{}, err
	}
	defer f.Close()

	// The title is the first line (without a Markdown #), the overview the
	// first paragraph after it.
	var title string
	var overview []string
	words, overviewDone := 0, false
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		words += len(strings.Fields(line))
		switch {
		case line == "":
			overviewDone = len(overview) > 0
		case title == "":
			title = strings.TrimSpace(strings.TrimLeft(line, "#"))
		case !overviewDone && !strings.HasPrefix(line, "#"):
			overview = append(overview, line)
		}
	}
	if err := sc.Err(); err != nil {
		return plugin.IndexResult{}, err
	}
	if title == "" {
		// An empty file isn't a story.
		return plugin.IndexResult{}, nil
	}

	minutes := max(1, (words+wordsPerMinute/2)/wordsPerMinute)
	it := &plugin.Item{
		Kind:  "story",
		Title: title,
		Details: plugin.Details{
			Overview: strings.Join(overview, " "),
			Facts: []plugin.Fact{
				{Label: "Words", Value: fmt.Sprint(words)},
				{Label: "Reading time", Value: fmt.Sprintf("%d min", minutes)},
			},
		},
		Data: map[string]any{"minutes": minutes, "markdown": strings.EqualFold(filepath.Ext(r.Path), ".md")},
	}
	// A story in a folder of its own belongs to a book named after the
	// folder, with the folder's cover.jpg or cover.png as its poster.
	if dir := filepath.Dir(r.Path); dir != filepath.Clean(r.Root) {
		it.Parents = []plugin.Parent{{Kind: "book", Title: filepath.Base(dir), Poster: cover(dir)}}
	}
	return plugin.IndexResult{Item: it}, nil
}

func cover(dir string) string {
	for _, name := range []string{"cover.jpg", "cover.png"} {
		if p := filepath.Join(dir, name); fileExists(p) {
			return p
		}
	}
	return ""
}

func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.Mode().IsRegular()
}

// event hears when someone stops reading, as plugin.json asks.
func event(e plugin.Event) error {
	if e.Type == "playback.stopped" && e.Played != nil && e.Played.Finished && e.Item != nil && e.User != nil {
		plugin.Info("%s finished %q", e.User.Name, e.Item.Title)
	}
	return nil
}
