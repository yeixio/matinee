// Package update finds newer Matinee versions on GitHub and, where this
// server can replace itself, installs them (Settings → Updates). It asks
// GitHub directly; nothing goes through anyone else's servers.
package update

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Kind is how this server was installed, which decides how it's updated.
type Kind string

const (
	KindBinary     Kind = "binary"      // an archive from a release, run by hand or by the admin's own service
	KindDocker     Kind = "docker"      // a container image
	KindKubernetes Kind = "kubernetes"  // the Helm chart
	KindDeb        Kind = "deb"         // the apt package
	KindRPM        Kind = "rpm"         // the dnf/yum package
	KindHomebrew   Kind = "homebrew"    // brew install matinee
	KindMacApp     Kind = "macos-app"   // Matinee Server.app, which updates itself
	KindWindowsApp Kind = "windows-app" // the Windows installer's tray app, which updates itself
)

// Install says how this server was installed and what updating it can do.
type Install struct {
	Kind Kind `json:"kind"`
	// ManagedBy names what deploys this server from Git, such as argocd or
	// flux (MATINEE_UPDATES_MANAGED_BY, which the Helm chart sets from
	// updates.managedBy). Updates then go through Git, never from here.
	ManagedBy string `json:"managedBy,omitempty"`
	// CanApply is whether this server can install an update itself.
	CanApply bool `json:"canApply"`
	// Why says why it can't, as a word the web player explains:
	// managed, read-only, no-helper, windows, dev, or empty when the
	// install kind always updates some other way.
	Why string `json:"why,omitempty"`
	// Beta is whether the beta channel (builds of main that passed CI) is
	// offered. Only container images are built from main.
	Beta bool `json:"beta"`
	// Release is the Helm release's name and namespace, for the command.
	Release   string `json:"release,omitempty"`
	Namespace string `json:"namespace,omitempty"`

	exe    string // the running executable
	prefix string // Homebrew's prefix, such as /opt/homebrew
}

// System is what Detect looks at, so tests can pretend.
type System struct {
	Getenv     func(string) string
	Executable func() (string, error)
	Exists     func(string) bool
	Writable   func(dir string) bool
	GOOS       string
}

// Local is this computer.
func Local() System {
	return System{
		Getenv:     os.Getenv,
		Executable: os.Executable,
		Exists: func(p string) bool {
			_, err := os.Stat(p)
			return err == nil
		},
		Writable: writable,
		GOOS:     runtime.GOOS,
	}
}

// PackageRequest is where the server asks the packages' root helper
// (matinee-update.path) to upgrade the matinee package, and PackageLog is
// where the helper says how it went. The log is outside the server's data
// directory, which the helper as root must never write into.
const (
	PackageRequest = "/var/lib/matinee/update/request"
	PackageLog     = "/var/log/matinee-update.log"
)

// packageHelpers are where the packages install the helper's path unit.
var packageHelpers = []string{"/usr/lib/systemd/system/matinee-update.path", "/lib/systemd/system/matinee-update.path"}

// Detect works out how this server was installed. MATINEE_INSTALL says so
// outright (the packages and the chart set it); otherwise it's told from
// where the executable is and what's around it.
func Detect(sys System, version string) Install {
	exe, _ := sys.Executable()
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	in := Install{exe: exe, ManagedBy: strings.ToLower(strings.TrimSpace(sys.Getenv("MATINEE_UPDATES_MANAGED_BY")))}
	kind := Kind(strings.ToLower(strings.TrimSpace(sys.Getenv("MATINEE_INSTALL"))))
	if kind == "package" {
		kind = packageKind(sys)
	}
	if kind == "" {
		kind = detectKind(sys, exe)
	}
	in.Kind = kind
	if kind == KindHomebrew {
		if i := strings.Index(exe, "/Cellar/"); i > 0 {
			in.prefix = exe[:i]
		}
	}
	if kind == KindKubernetes {
		in.Release = sys.Getenv("MATINEE_HELM_RELEASE")
		in.Namespace = sys.Getenv("MATINEE_HELM_NAMESPACE")
	}
	in.Beta = kind == KindDocker || kind == KindKubernetes

	switch {
	case !isRelease(version) && !isMainBuild(version):
		in.Why = "dev"
	case in.ManagedBy != "":
		in.Why = "managed"
	case kind == KindBinary:
		switch {
		case sys.GOOS == "windows":
			in.Why = "windows"
		case !isRelease(version):
			in.Why = "dev"
		case !sys.Writable(filepath.Dir(exe)):
			in.Why = "read-only"
		default:
			in.CanApply = true
		}
	case kind == KindHomebrew:
		if in.prefix != "" && sys.Exists(filepath.Join(in.prefix, "bin", "brew")) {
			in.CanApply = true
		} else {
			in.Why = "no-helper"
		}
	case kind == KindDeb || kind == KindRPM:
		helper := false
		for _, p := range packageHelpers {
			helper = helper || sys.Exists(p)
		}
		switch {
		case !helper:
			in.Why = "no-helper"
		case !sys.Writable(filepath.Dir(filepath.Dir(PackageRequest))):
			in.Why = "read-only"
		default:
			in.CanApply = true
		}
	}
	return in
}

func detectKind(sys System, exe string) Kind {
	switch {
	case sys.Getenv("KUBERNETES_SERVICE_HOST") != "":
		return KindKubernetes
	case sys.Exists("/.dockerenv") || sys.Exists("/run/.containerenv"):
		return KindDocker
	// The Windows tray app runs the server this way (cmd/matinee-tray).
	case sys.Getenv("MATINEE_STOP_ON_STDIN_CLOSE") == "1":
		return KindWindowsApp
	case strings.Contains(exe, ".app/Contents/MacOS/"):
		return KindMacApp
	case strings.Contains(exe, "/Cellar/matinee/"):
		return KindHomebrew
	case exe == "/usr/bin/matinee":
		return packageKind(sys)
	}
	return KindBinary
}

func packageKind(sys System) Kind {
	if sys.Exists("/var/lib/dpkg/info/matinee.list") || (sys.Exists("/usr/bin/dpkg") && !sys.Exists("/usr/bin/rpm")) {
		return KindDeb
	}
	return KindRPM
}

// writable is whether a file can be made in dir, as replacing the
// executable needs.
func writable(dir string) bool {
	f, err := os.CreateTemp(dir, ".matinee-write-test-*")
	if err != nil {
		return false
	}
	f.Close()
	os.Remove(f.Name())
	return true
}
