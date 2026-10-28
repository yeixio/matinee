// Command matinee-plugin helps write Matinee plugins: it starts one from a
// template, checks and packages it, runs its server part on a folder of
// files the way a server would, and previews its player in a browser
// without a Matinee server. The developer guide (docs/guide/plugins) walks
// through it.
//
//	go install github.com/yeixio/matinee/cmd/matinee-plugin@latest
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/yeixio/matinee/internal/plugin"
	"github.com/yeixio/matinee/internal/version"
)

const usage = `matinee-plugin helps write Matinee plugins.

Usage:

  matinee-plugin new [flags] <id>          start a plugin in a new folder
  matinee-plugin check [dir]               check plugin.json and the files it names
  matinee-plugin build [flags] [dir]       build server.wasm, check, and package
  matinee-plugin scan [flags] [dir] <folder>
                                           show what the plugin makes of each file
  matinee-plugin lookup [flags] [dir]      look up details for a title
  matinee-plugin event [flags] [dir] <event.json>
                                           send the plugin an event
  matinee-plugin try [flags] [dir] <folder>
                                           browse and play a folder in a browser,
                                           as the web player would
  matinee-plugin version

dir is the plugin's folder, the one with plugin.json in it; the current
folder when left out. Run "matinee-plugin <command> -h" for a command's flags.
The developer guide: https://matinee.yeix.io/docs/plugins/overview/
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	cmd, args := os.Args[1], os.Args[2:]
	var err error
	switch cmd {
	case "new":
		err = runNew(args)
	case "check":
		err = runCheck(args)
	case "build":
		err = runBuild(args)
	case "scan":
		err = runScan(args)
	case "lookup":
		err = runLookup(args)
	case "event":
		err = runEvent(args)
	case "try":
		err = runTry(args)
	case "version":
		fmt.Println(version.Version)
	case "help", "-h", "-help", "--help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "matinee-plugin: unknown command %q\n\n%s", cmd, usage)
		os.Exit(2)
	}
	if errors.Is(err, flag.ErrHelp) {
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "matinee-plugin:", err)
		os.Exit(1)
	}
}

// flags makes a command's flag set. Errors come back from Parse rather than
// ending the program, so tests can run commands.
func flags(name, args string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: matinee-plugin %s %s\n", name, args)
		fs.PrintDefaults()
	}
	return fs
}

// settings collects -set key=value flags: what an admin would fill in for
// the plugin's settings.
type settings map[string]string

func (s settings) String() string { return "" }

func (s settings) Set(v string) error {
	k, val, ok := strings.Cut(v, "=")
	if !ok || k == "" {
		return fmt.Errorf("%q: want key=value", v)
	}
	s[k] = val
	return nil
}

// dirAnd splits a command's arguments into the plugin folder, which can be
// left out, and the n arguments that follow it.
func dirAnd(fs *flag.FlagSet, n int) (string, []string, error) {
	args := fs.Args()
	switch len(args) {
	case n:
		return ".", args, nil
	case n + 1:
		return args[0], args[1:], nil
	}
	fs.Usage()
	return "", nil, flag.ErrHelp
}

// open loads the plugin in dir to run its server part. Its data folder is
// .matinee-dev/data in the plugin's folder.
func open(ctx context.Context, dir string, config map[string]string, verbose bool) (*plugin.Dev, error) {
	level := slog.LevelInfo
	if verbose {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	return plugin.OpenDev(ctx, dir, filepath.Join(dir, devDir, "data"), config, log)
}

// devDir is where the tool keeps a plugin's data folder and, for try, what
// its player stored. It's in the template's .gitignore.
const devDir = ".matinee-dev"

var stdout io.Writer = os.Stdout
