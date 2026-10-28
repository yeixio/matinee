package main

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/yeixio/matinee/internal/plugin"
)

//go:embed template
var templates embed.FS

// sdkModule is the Go SDK plugins import.
const sdkModule = "github.com/yeixio/matinee/pluginsdk"

func runNew(args []string) error {
	fs := flags("new", "[flags] <id>")
	name := fs.String("name", "", "the plugin's name, as people see it (default from the id)")
	kind := fs.String("kind", "", "the library kind it adds (default the id)")
	item := fs.String("item", "item", "the kind of item it files")
	exts := fs.String("ext", ".txt", "the file extensions it's asked about, separated by commas")
	module := fs.String("module", "", "the Go module path (default example.com/<id>)")
	sdk := fs.String("sdk", "latest", `the SDK version to require, a folder with the SDK in it (for working on the SDK itself), or "none" to leave go.mod for later`)
	out := fs.String("dir", "", "the folder to make (default the id)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return fmt.Errorf("give the new plugin's id, such as my-plugin")
	}
	p := newPlugin{ID: fs.Arg(0), Name: *name, Kind: *kind, Item: *item, Module: *module}
	for _, e := range strings.Split(*exts, ",") {
		if e = strings.TrimSpace(e); e != "" {
			if !strings.HasPrefix(e, ".") {
				e = "." + e
			}
			p.Exts = append(p.Exts, strings.ToLower(e))
		}
	}
	if p.Name == "" {
		p.Name = titleCase(p.ID)
	}
	if p.Kind == "" {
		p.Kind = p.ID
	}
	if p.Module == "" {
		p.Module = "example.com/" + p.ID
	}
	if len(p.Exts) > 0 {
		p.Ext = p.Exts[0]
	}
	man := p.manifest()
	b, err := json.MarshalIndent(man, "", "  ")
	if err != nil {
		return err
	}
	// The template has to make a plugin a server would take.
	if _, err := plugin.ParseManifest(b); err != nil {
		return err
	}
	dir := *out
	if dir == "" {
		dir = p.ID
	}
	if _, err := os.Stat(dir); err == nil {
		return fmt.Errorf("%s is already there", dir)
	}
	if err := p.write(dir, append(b, '\n')); err != nil {
		return err
	}
	if err := writeIcon(filepath.Join(dir, "icon.png")); err != nil {
		return err
	}
	if err := p.goMod(dir, *sdk); err != nil {
		fmt.Fprintf(stdout, "warning: %v\n  Run this in %s when you can: go get %s@latest\n", err, dir, sdkModule)
	}
	fmt.Fprintf(stdout, "Made %s in %s. Next:\n\n  cd %s\n  go test ./...\n  matinee-plugin try path/to/a/folder   # with %s files in it\n  matinee-plugin build\n", p.Name, dir, dir, strings.Join(p.Exts, " or "))
	return nil
}

type newPlugin struct {
	ID, Name, Kind, Item, Module string
	Exts                         []string
	Ext                          string
}

func (p newPlugin) manifest() plugin.Manifest {
	return plugin.Manifest{
		ID: p.ID, Name: p.Name, Version: "0.1.0", API: 1,
		Description: "Plays " + strings.Join(p.Exts, ", ") + " files.",
		License:     "MIT",
		Icon:        "icon.png",
		Server:      "server.wasm",
		LibraryKinds: []plugin.LibraryKind{{
			Name: p.Kind, Label: p.Name, Description: "A folder of " + strings.Join(p.Exts, ", ") + " files.",
			Extensions: p.Exts, ItemKinds: []string{p.Item},
		}},
		Players: []plugin.Player{{ItemKinds: []string{p.Item}, Entry: "player/index.html"}},
	}
}

func (p newPlugin) write(dir string, manifest []byte) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"), manifest, 0o644); err != nil {
		return err
	}
	return fs.WalkDir(templates, "template", func(name string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() {
			return err
		}
		b, err := templates.ReadFile(name)
		if err != nil {
			return err
		}
		rel := strings.TrimPrefix(name, "template/")
		if rel == "gitignore" {
			rel = ".gitignore" // embed leaves out dot files
		}
		if strings.HasSuffix(rel, ".tmpl") {
			rel = strings.TrimSuffix(rel, ".tmpl")
			t, err := template.New(rel).Parse(string(b))
			if err != nil {
				return err
			}
			var buf bytes.Buffer
			if err := t.Execute(&buf, p); err != nil {
				return err
			}
			b = buf.Bytes()
		}
		dst := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		return os.WriteFile(dst, b, 0o644)
	})
}

// goMod writes go.mod and requires the SDK.
func (p newPlugin) goMod(dir, sdk string) error {
	mod := fmt.Sprintf("module %s\n\ngo 1.24\n", p.Module)
	local := ""
	if info, err := os.Stat(sdk); err == nil && info.IsDir() {
		if local, err = filepath.Abs(sdk); err != nil {
			return err
		}
		mod += fmt.Sprintf("\nrequire %s v0.0.0\n\nreplace %s => %s\n", sdkModule, sdkModule, filepath.ToSlash(local))
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(mod), 0o644); err != nil {
		return err
	}
	if sdk == "none" || local != "" {
		return nil
	}
	cmd := exec.Command("go", "get", sdkModule+"@"+sdk)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("go get %s: %v\n%s", sdkModule, err, out)
	}
	return nil
}

func titleCase(id string) string {
	words := strings.Split(id, "-")
	for i, w := range words {
		if w != "" {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}

// writeIcon draws a placeholder icon: a coral disc on a dark square, to be
// replaced with the plugin's own.
func writeIcon(dst string) error {
	const size = 256
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	bg, fg := color.RGBA{0x22, 0x1d, 0x26, 0xff}, color.RGBA{0xff, 0x6a, 0x55, 0xff}
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			dx, dy := float64(x)-size/2+0.5, float64(y)-size/2+0.5
			if dx*dx+dy*dy < 72*72 {
				img.Set(x, y, fg)
			} else {
				img.Set(x, y, bg)
			}
		}
	}
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	if err := png.Encode(f, img); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
