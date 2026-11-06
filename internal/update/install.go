package update

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// How Matinee was installed, which decides who updates it.
const (
	KindArgoCD     = "argocd"     // the Helm chart through Argo CD, which Matinee can move
	KindKubernetes = "kubernetes" // the Helm chart without Argo CD
	KindDocker     = "docker"     // a container: Docker, Compose, Unraid, TrueNAS and the like
	KindApt        = "apt"        // the .deb package
	KindDnf        = "dnf"        // the .rpm package
	KindHomebrew   = "homebrew"
	KindApp        = "app" // inside Matinee Server for Mac or the Windows installer, which update themselves
	KindBinary     = "binary"
	KindSource     = "source" // built from source, or a version that can't be compared
)

var known = map[string]bool{KindDocker: true, KindApt: true, KindDnf: true, KindHomebrew: true, KindApp: true, KindBinary: true, KindSource: true}

// Install is how Matinee was installed and what it can do about updates.
type Install struct {
	Kind string `json:"kind"`
	// CanApply is whether the server can install an update itself.
	CanApply bool `json:"canApply"`
	// Channels are the channels this install can follow: only container
	// images are built for each commit on main.
	Channels []string `json:"channels"`
	// Application is the Argo CD Application, as namespace/name.
	Application string `json:"application,omitempty"`
	// Prefix is Homebrew's prefix, such as /opt/homebrew.
	Prefix string `json:"-"`
}

// PackageRequest is where the server asks the Linux packages' root helper
// (matinee-update.path) to upgrade the matinee package, and PackageLog is
// where the helper says how it went. The log is outside the server's data
// directory, which the helper, as root, never writes into.
const (
	PackageRequest = "/var/lib/matinee/update/request"
	PackageLog     = "/var/log/matinee-update.log"
)

// packageHelper is there while the packages' helper is turned on
// (systemctl enable matinee-update.path); an admin who turns it off installs
// updates by hand.
const packageHelper = "/etc/systemd/system/multi-user.target.wants/matinee-update.path"

// Env reads the environment; tests give their own.
type Env struct {
	Getenv func(string) string
	Exists func(string) bool
	// Executable is the server's own path.
	Executable string
	GOOS       string
	// Writable is whether the server can replace a file in dir.
	Writable func(dir string) bool
}

// OSEnv is the real environment.
func OSEnv() Env {
	exe, _ := os.Executable()
	if p, err := filepath.EvalSymlinks(exe); err == nil {
		exe = p
	}
	return Env{
		Getenv:     os.Getenv,
		Exists:     func(p string) bool { _, err := os.Stat(p); return err == nil },
		Executable: exe,
		GOOS:       runtime.GOOS,
		Writable: func(dir string) bool {
			f, err := os.CreateTemp(dir, ".matinee-update-check-*")
			if err != nil {
				return false
			}
			f.Close()
			os.Remove(f.Name())
			return true
		},
	}
}

// Detect works out how Matinee was installed. MATINEE_INSTALL says so
// outright (the image, the chart and the Linux packages set it); otherwise
// it's worked out from where the server runs.
func Detect(b Build, env Env) Install {
	kind := strings.ToLower(strings.TrimSpace(env.Getenv("MATINEE_INSTALL")))
	app := strings.TrimSpace(env.Getenv("MATINEE_ARGOCD_APPLICATION"))
	inKube := env.Getenv("KUBERNETES_SERVICE_HOST") != ""
	switch {
	case inKube && app != "":
		kind = KindArgoCD
	case kind == "helm" || inKube:
		kind = KindKubernetes
	case kind == "package":
		kind = KindApt
		if !env.Exists("/etc/debian_version") {
			kind = KindDnf
		}
	case known[kind]:
	case env.Exists("/.dockerenv") || env.Exists("/run/.containerenv"):
		kind = KindDocker
	case env.Getenv("MATINEE_STOP_ON_STDIN_CLOSE") == "1" || strings.Contains(env.Executable, ".app/Contents/"):
		kind = KindApp
	case strings.Contains(env.Executable, "/Cellar/") || strings.Contains(env.Executable, "/homebrew/") || strings.Contains(env.Executable, "/linuxbrew/"):
		kind = KindHomebrew
	case env.Executable == "/usr/bin/matinee":
		kind = KindApt
		if !env.Exists("/etc/debian_version") {
			kind = KindDnf
		}
	default:
		kind = KindBinary
	}
	if !b.Known() {
		kind = KindSource
	}

	in := Install{Kind: kind, Channels: []string{ChannelRelease}}
	switch kind {
	case KindArgoCD:
		in.CanApply, in.Application = true, app
		in.Channels = append(in.Channels, ChannelMain)
	case KindKubernetes, KindDocker:
		in.Channels = append(in.Channels, ChannelMain)
	case KindBinary:
		// Windows won't replace a running program; its installer does that.
		in.CanApply = env.GOOS != "windows" && env.Executable != "" && env.Writable(filepath.Dir(env.Executable))
	case KindHomebrew:
		// brew upgrade runs as the user the service runs as, who installed it.
		if i := strings.Index(env.Executable, "/Cellar/"); i > 0 {
			in.Prefix = env.Executable[:i]
			in.CanApply = env.Exists(filepath.Join(in.Prefix, "bin", "brew"))
		}
	case KindApt, KindDnf:
		in.CanApply = env.Exists(packageHelper) && env.Writable(filepath.Dir(PackageRequest))
	case KindSource:
		in.Channels = nil
	}
	return in
}

// ArgoApplication splits namespace/name; a bare name is in argocd.
func ArgoApplication(app string) (namespace, name string) {
	if ns, n, ok := strings.Cut(app, "/"); ok {
		return ns, n
	}
	return "argocd", app
}
