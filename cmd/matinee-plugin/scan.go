package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/yeixio/matinee/internal/plugin"
)

func runScan(args []string) error {
	fs := flags("scan", "[dir] <folder>")
	kind := fs.String("kind", "", "the library kind to scan as (default the plugin's first)")
	asJSON := fs.Bool("json", false, "print the plugin's answers as JSON")
	lookup := fs.Bool("lookup", false, "also look up details for each item, as a server does after a scan")
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
	ctx := context.Background()
	d, err := open(ctx, dir, cfg, *verbose)
	if err != nil {
		return err
	}
	defer d.Close(ctx)

	var items, skipped, failed int
	var all []scanned
	err = d.Scan(ctx, *kind, rest[0], func(f plugin.DevFile) {
		s := scanned{DevFile: f}
		switch {
		case f.Error != "":
			failed++
		case f.Item == nil:
			skipped++
		default:
			items++
			if *lookup {
				s.Lookup = lookupFor(ctx, d, f.Item)
			}
		}
		if *asJSON {
			all = append(all, s)
		} else {
			s.print(stdout)
		}
	})
	if err != nil {
		return err
	}
	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(all)
	}
	fmt.Fprintf(stdout, "\n%d files: %d items, %d skipped, %d errors\n", items+skipped+failed, items, skipped, failed)
	return nil
}

type scanned struct {
	plugin.DevFile
	Lookup *lookedUp `json:"lookup,omitempty"`
}

type lookedUp struct {
	plugin.DevLookup
	Error string `json:"error,omitempty"`
}

// lookupFor looks up an item the scan filed, if the plugin looks up its
// kind.
func lookupFor(ctx context.Context, d *plugin.Dev, raw json.RawMessage) *lookedUp {
	var it struct {
		Kind  string          `json:"kind"`
		Title string          `json:"title"`
		Year  int             `json:"year"`
		Data  json.RawMessage `json:"data"`
	}
	if json.Unmarshal(raw, &it) != nil || !slices.Contains(d.Manifest.Metadata, it.Kind) {
		return nil
	}
	l, err := d.Lookup(ctx, it.Kind, it.Title, it.Year, it.Data)
	res := &lookedUp{DevLookup: l}
	if err != nil {
		res.Error = err.Error()
	}
	return res
}

func (s scanned) print(w io.Writer) {
	switch {
	case s.Error != "":
		fmt.Fprintf(w, "%s\n  error: %s\n", s.Path, s.Error)
		return
	case s.Item == nil:
		fmt.Fprintf(w, "%s\n  skipped\n", s.Path)
		return
	}
	fmt.Fprintf(w, "%s\n", s.Path)
	printItem(w, s.Item)
	for _, p := range s.Problems {
		fmt.Fprintf(w, "  problem: %s\n", p)
	}
	if l := s.Lookup; l != nil {
		switch {
		case l.Error != "":
			fmt.Fprintf(w, "  lookup error: %s\n", l.Error)
		case l.Details == nil:
			fmt.Fprintf(w, "  lookup: no match\n")
		default:
			fmt.Fprintf(w, "  lookup:\n")
			printDetails(w, "    ", l.Details)
			for _, p := range l.Problems {
				fmt.Fprintf(w, "    problem: %s\n", p)
			}
		}
	}
}

// printItem shows an item the way someone browsing would think of it.
func printItem(w io.Writer, raw json.RawMessage) {
	var it struct {
		Kind    string `json:"kind"`
		Title   string `json:"title"`
		Year    int    `json:"year"`
		Index   int    `json:"index"`
		Parents []struct {
			Kind  string `json:"kind"`
			Title string `json:"title"`
		} `json:"parents"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &it); err != nil {
		fmt.Fprintf(w, "  %s\n", raw)
		return
	}
	line := fmt.Sprintf("  %s %q", it.Kind, it.Title)
	if it.Year > 0 {
		line += fmt.Sprintf(" (%d)", it.Year)
	}
	if it.Index > 0 {
		line += fmt.Sprintf(" #%d", it.Index)
	}
	fmt.Fprintln(w, line)
	if len(it.Parents) > 0 {
		var ps []string
		for _, p := range it.Parents {
			ps = append(ps, fmt.Sprintf("%s %q", p.Kind, p.Title))
		}
		fmt.Fprintf(w, "  in: %s\n", strings.Join(ps, " > "))
	}
	printDetails(w, "  ", raw)
	if len(it.Data) > 0 && string(it.Data) != "null" {
		fmt.Fprintf(w, "  data: %s\n", it.Data)
	}
}

func printDetails(w io.Writer, indent string, raw json.RawMessage) {
	var d struct {
		Overview string   `json:"overview"`
		Genres   []string `json:"genres"`
		Rating   float64  `json:"rating"`
		Facts    []struct {
			Label string `json:"label"`
			Value string `json:"value"`
		} `json:"facts"`
		Poster   string `json:"poster"`
		Backdrop string `json:"backdrop"`
	}
	if json.Unmarshal(raw, &d) != nil {
		return
	}
	if d.Overview != "" {
		o := d.Overview
		if len(o) > 100 {
			o = o[:97] + "..."
		}
		fmt.Fprintf(w, "%soverview: %s\n", indent, o)
	}
	if len(d.Genres) > 0 {
		fmt.Fprintf(w, "%sgenres: %s\n", indent, strings.Join(d.Genres, ", "))
	}
	if d.Rating > 0 {
		fmt.Fprintf(w, "%srating: %.1f\n", indent, d.Rating)
	}
	for _, f := range d.Facts {
		fmt.Fprintf(w, "%s%s: %s\n", indent, f.Label, f.Value)
	}
	if d.Poster != "" {
		fmt.Fprintf(w, "%sposter: %s\n", indent, d.Poster)
	}
	if d.Backdrop != "" {
		fmt.Fprintf(w, "%sbackdrop: %s\n", indent, d.Backdrop)
	}
}

func runLookup(args []string) error {
	fs := flags("lookup", "-kind <kind> -title <title> [dir]")
	kind := fs.String("kind", "", "the item kind (default the first under metadata in plugin.json)")
	title := fs.String("title", "", "the title to look up")
	year := fs.Int("year", 0, "the year, if known")
	data := fs.String("data", "", "the data the plugin kept with the item when scanning, as JSON")
	verbose := fs.Bool("v", false, "show the plugin's debug log lines")
	cfg := settings{}
	fs.Var(cfg, "set", "a setting, as key=value; can be repeated")
	if err := fs.Parse(args); err != nil {
		return err
	}
	dir, _, err := dirAnd(fs, 0)
	if err != nil {
		return err
	}
	if *title == "" {
		fs.Usage()
		return fmt.Errorf("-title is needed")
	}
	if *data != "" && !json.Valid([]byte(*data)) {
		return fmt.Errorf("-data isn't JSON")
	}
	ctx := context.Background()
	d, err := open(ctx, dir, cfg, *verbose)
	if err != nil {
		return err
	}
	defer d.Close(ctx)
	if *kind == "" {
		if len(d.Manifest.Metadata) == 0 {
			return fmt.Errorf("the plugin doesn't look up details: plugin.json lists no kinds under metadata")
		}
		*kind = d.Manifest.Metadata[0]
	}
	var raw json.RawMessage
	if *data != "" {
		raw = json.RawMessage(*data)
	}
	l, err := d.Lookup(ctx, *kind, *title, *year, raw)
	if err != nil {
		return err
	}
	if l.Details == nil {
		fmt.Fprintln(stdout, "no match")
		return nil
	}
	printDetails(stdout, "", l.Details)
	for _, p := range l.Problems {
		fmt.Fprintln(stdout, "problem:", p)
	}
	return nil
}

func runEvent(args []string) error {
	fs := flags("event", "[dir] <event.json, or - to read it from standard input>")
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
	var b []byte
	if rest[0] == "-" {
		b, err = io.ReadAll(os.Stdin)
	} else {
		b, err = os.ReadFile(rest[0])
	}
	if err != nil {
		return err
	}
	var e plugin.Event
	if err := json.Unmarshal(b, &e); err != nil {
		return fmt.Errorf("the event: %w", err)
	}
	if e.Time == 0 {
		e.Time = time.Now().UnixMilli()
	}
	ctx := context.Background()
	d, err := open(ctx, dir, cfg, *verbose)
	if err != nil {
		return err
	}
	defer d.Close(ctx)
	if err := d.Event(ctx, e); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "%s handled\n", e.Type)
	return nil
}
