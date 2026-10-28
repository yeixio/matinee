// Package main is a plugin for Matinee's own tests. A .game file is JSON
// describing the game; the plugin files it and records the events it hears.
package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	plugin "github.com/yeixio/matinee/pluginsdk"
)

type game struct {
	Title  string `json:"title"`
	Year   int    `json:"year"`
	System string `json:"system"`
	Skip   bool   `json:"skip"`
	Panic  bool   `json:"panic"`
	Fetch  string `json:"fetch"`
	Kind   string `json:"kind"`
}

func init() {
	plugin.Register(plugin.Handlers{Index: index, Lookup: lookup, Event: event})
}

func main() {}

func index(r plugin.IndexRequest) (plugin.IndexResult, error) {
	b, err := os.ReadFile(r.Path)
	if err != nil {
		return plugin.IndexResult{}, err
	}
	var g game
	if err := json.Unmarshal(b, &g); err != nil {
		return plugin.IndexResult{}, err
	}
	switch {
	case g.Skip:
		return plugin.IndexResult{}, nil
	case g.Panic:
		panic("asked to")
	}
	it := &plugin.Item{
		Kind: "game", Title: g.Title, Year: g.Year,
		Details: plugin.Details{
			Overview: "Says " + r.Config["greeting"],
			Genres:   []string{"Arcade"},
			Facts:    []plugin.Fact{{Label: "File", Value: filepath.Base(r.Path)}},
		},
		Data: map[string]any{"system": g.System},
	}
	if g.Kind != "" {
		it.Kind = g.Kind
	}
	if g.System != "" {
		it.Parents = []plugin.Parent{{Kind: "system", Title: g.System}}
	}
	if poster := strings.TrimSuffix(r.Path, ".game") + ".png"; exists(poster) {
		it.Poster = poster
	}
	if g.Fetch != "" {
		body, err := plugin.Get(g.Fetch)
		if err != nil {
			return plugin.IndexResult{}, err
		}
		it.Facts = append(it.Facts, plugin.Fact{Label: "Fetched", Value: string(body)})
	}
	return plugin.IndexResult{Item: it}, nil
}

func lookup(r plugin.LookupRequest) (*plugin.Details, error) {
	if r.Title == "Nothing" {
		return nil, nil
	}
	return &plugin.Details{Overview: "Looked up " + r.Title, Facts: []plugin.Fact{{Label: "Data", Value: dataString(r.Data)}}}, nil
}

func dataString(d map[string]any) string {
	b, _ := json.Marshal(d)
	return string(b)
}

func event(e plugin.Event) error {
	f, err := os.OpenFile(filepath.Join(plugin.DataDir, "events.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	title := ""
	if e.Item != nil {
		title = e.Item.Title
	}
	_, err = f.WriteString(e.Type + " " + title + "\n")
	if e.Type == "library.scanned" && e.Library == nil {
		return errors.New("no library")
	}
	return err
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
