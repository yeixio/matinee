package main

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/yeixio/matinee/internal/plugin"
)

func runCheck(args []string) error {
	fs := flags("check", "[dir]")
	if err := fs.Parse(args); err != nil {
		return err
	}
	dir, _, err := dirAnd(fs, 0)
	if err != nil {
		return err
	}
	_, err = check(context.Background(), dir)
	return err
}

func runBuild(args []string) error {
	fs := flags("build", "[dir]")
	out := fs.String("o", "", "the package to write (default <id>.matinee-plugin in the plugin's folder)")
	var include multi
	fs.Var(&include, "include", "another file or folder to put in the package, relative to the plugin's folder; can be repeated")
	noGo := fs.Bool("no-go", false, "don't build the server module with Go; package the one that's there")
	if err := fs.Parse(args); err != nil {
		return err
	}
	dir, _, err := dirAnd(fs, 0)
	if err != nil {
		return err
	}
	man, err := readManifest(dir)
	if err != nil {
		return err
	}
	if man.Server != "" && !*noGo {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			if err := goBuild(dir, man.Server); err != nil {
				return err
			}
		}
	}
	man, err = check(context.Background(), dir)
	if err != nil {
		return err
	}
	if *out == "" {
		*out = filepath.Join(dir, man.ID+".matinee-plugin")
	}
	n, size, err := pack(dir, man, include, *out)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "wrote %s: %d files, %s\n", *out, n, megabytes(size))
	return nil
}

type multi []string

func (m *multi) String() string     { return strings.Join(*m, ",") }
func (m *multi) Set(v string) error { *m = append(*m, v); return nil }

// goBuild builds the server module as Matinee runs it: a WASI reactor.
func goBuild(dir, server string) error {
	fmt.Fprintf(stdout, "building %s\n", server)
	cmd := exec.Command("go", "build", "-buildmode=c-shared", "-trimpath", "-o", filepath.FromSlash(server), ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOOS=wasip1", "GOARCH=wasm", "CGO_ENABLED=0")
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("go build: %w", err)
	}
	return nil
}

func readManifest(dir string) (plugin.Manifest, error) {
	b, err := os.ReadFile(filepath.Join(dir, "plugin.json"))
	if err != nil {
		return plugin.Manifest{}, err
	}
	return plugin.ParseManifest(b)
}

var semver = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

// check reads plugin.json the way a server does, makes sure the files it
// names are there and the server module would start, and warns about what
// isn't wrong but should be looked at.
func check(ctx context.Context, dir string) (plugin.Manifest, error) {
	man, err := readManifest(dir)
	if err != nil {
		return man, err
	}
	var problems, warnings []string
	need := func(what, name string) {
		if name == "" {
			return
		}
		if info, err := os.Stat(filepath.Join(dir, filepath.FromSlash(name))); err != nil || info.IsDir() {
			problems = append(problems, fmt.Sprintf("%s %s isn't there", what, name))
		}
	}
	need("the server module", man.Server)
	need("the icon", man.Icon)
	for _, p := range man.Players {
		need("the player", p.Entry)
	}
	if len(problems) == 0 && man.Server != "" {
		d, err := open(ctx, dir, nil, false)
		if err != nil {
			problems = append(problems, err.Error())
		} else {
			d.Close(ctx)
		}
	}
	if !man.Supported() {
		problems = append(problems, fmt.Sprintf("api %d isn't one this version of Matinee runs (it runs %v)", man.API, plugin.APIVersions))
	}

	if !semver.MatchString(man.Version) {
		warnings = append(warnings, fmt.Sprintf("version %q isn't like 1.2.3; people see it when updating", man.Version))
	}
	if man.Icon == "" {
		warnings = append(warnings, "no icon: Settings shows a plain one")
	}
	if man.Description == "" {
		warnings = append(warnings, "no description: Settings shows it under the name")
	}
	if man.License == "" {
		warnings = append(warnings, "no license")
	}
	if man.Homepage == "" {
		warnings = append(warnings, "no homepage: Settings links to it for help")
	}
	for _, k := range man.Metadata {
		if len(man.Permissions.Network) == 0 {
			warnings = append(warnings, fmt.Sprintf("it looks up %s details but lists no network hosts, so it can only read its own files", k))
			break
		}
	}
	played := map[string]bool{}
	for _, p := range man.Players {
		for _, k := range p.ItemKinds {
			played[k] = true
		}
	}
	for _, k := range man.LibraryKinds {
		if len(k.ItemKinds) > 0 && !played[k.ItemKinds[0]] {
			warnings = append(warnings, fmt.Sprintf("no player plays %s items, so they can be browsed but not played", k.ItemKinds[0]))
		}
	}

	for _, w := range warnings {
		fmt.Fprintln(stdout, "warning:", w)
	}
	if len(problems) > 0 {
		for _, p := range problems {
			fmt.Fprintln(stdout, "problem:", p)
		}
		return man, fmt.Errorf("%s isn't ready: %d problems", man.ID, len(problems))
	}
	fmt.Fprintf(stdout, "%s %s looks good\n", man.ID, man.Version)
	return man, nil
}

// pack writes the package: plugin.json, the server module and icon, every
// player's folder, license and readme files, and anything else asked for.
// It returns how many files went in and their size.
func pack(dir string, man plugin.Manifest, include []string, out string) (int, int64, error) {
	files := map[string]bool{"plugin.json": true}
	add := func(name string) error {
		name = path.Clean(filepath.ToSlash(name))
		if name == "." || strings.HasPrefix(name, "../") || path.IsAbs(name) {
			return fmt.Errorf("%s isn't inside the plugin's folder", name)
		}
		root := filepath.Join(dir, filepath.FromSlash(name))
		return filepath.WalkDir(root, func(p string, e fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if strings.HasPrefix(e.Name(), ".") && p != root {
				if e.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if e.Type().IsRegular() {
				rel, _ := filepath.Rel(dir, p)
				files[filepath.ToSlash(rel)] = true
			}
			return nil
		})
	}
	for _, name := range []string{man.Server, man.Icon} {
		if name != "" {
			files[name] = true
		}
	}
	for _, p := range man.Players {
		if err := add(path.Dir(p.Entry)); err != nil {
			return 0, 0, err
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, 0, err
	}
	for _, e := range entries {
		upper := strings.ToUpper(e.Name())
		for _, prefix := range []string{"LICENSE", "LICENCE", "COPYING", "NOTICE", "README"} {
			if strings.HasPrefix(upper, prefix) && e.Type().IsRegular() {
				files[e.Name()] = true
			}
		}
	}
	for _, name := range include {
		if err := add(name); err != nil {
			return 0, 0, err
		}
	}
	// The package itself, if it's written inside a folder being packed.
	if abs, err := filepath.Abs(out); err == nil {
		if absDir, err := filepath.Abs(dir); err == nil {
			if rel, err := filepath.Rel(absDir, abs); err == nil {
				delete(files, filepath.ToSlash(rel))
			}
		}
	}

	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	tmp := out + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return 0, 0, err
	}
	defer os.Remove(tmp)
	zw := zip.NewWriter(f)
	var size int64
	for _, name := range names {
		n, err := addFile(zw, filepath.Join(dir, filepath.FromSlash(name)), name)
		if err != nil {
			f.Close()
			return 0, 0, err
		}
		size += n
	}
	if err := zw.Close(); err != nil {
		f.Close()
		return 0, 0, err
	}
	info, err := f.Stat()
	f.Close()
	if err != nil {
		return 0, 0, err
	}
	if info.Size() > plugin.MaxPackageSize {
		return 0, 0, fmt.Errorf("the package is %s; Matinee takes up to %s", megabytes(info.Size()), megabytes(plugin.MaxPackageSize))
	}
	return len(names), info.Size(), os.Rename(tmp, out)
}

func addFile(zw *zip.Writer, src, name string) (int64, error) {
	f, err := os.Open(src)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return 0, err
	}
	h, err := zip.FileInfoHeader(info)
	if err != nil {
		return 0, err
	}
	h.Name, h.Method = name, zip.Deflate
	if ext := path.Ext(name); ext == ".png" || ext == ".jpg" || ext == ".webp" || ext == ".gz" || ext == ".zip" {
		h.Method = zip.Store
	}
	w, err := zw.CreateHeader(h)
	if err != nil {
		return 0, err
	}
	n, err := io.Copy(w, f)
	if err != nil {
		return 0, err
	}
	if n != info.Size() {
		return 0, errors.New(src + " changed while packing")
	}
	return n, nil
}

func megabytes(n int64) string {
	if n < 1<<20 {
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
}
