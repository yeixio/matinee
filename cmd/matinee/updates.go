package main

import (
	"log/slog"
	"os"
	"strings"
	"sync/atomic"

	"github.com/yeixio/matinee/internal/kube"
	"github.com/yeixio/matinee/internal/store"
	"github.com/yeixio/matinee/internal/update"
	"github.com/yeixio/matinee/internal/version"
)

// restartRequested is set when the server stops to start again: a new
// version of itself (a plain binary that installed an update), or the same
// one when an admin pressed Restart.
var restartRequested atomic.Bool

// restartPath is the binary to start: the path the server started from,
// read before the update moved a new file there, or Homebrew's link to the
// version it just installed.
var restartPath string

// newUpdates sets up update checks for how this server was installed.
// stop shuts the server down, for a restart into an update.
func newUpdates(db *store.Store, log *slog.Logger, stop func(), busy func() bool) *update.Manager {
	env := update.OSEnv()
	build := update.ParseBuild(version.Version)
	in := update.Detect(build, env)
	restartPath = env.Executable
	m := &update.Manager{
		Store: db, Log: log, Build: build, Install: in,
		GitHub:     &update.GitHub{Base: envOr("MATINEE_UPDATE_REPO", update.DefaultRepo)},
		Executable: env.Executable,
		Busy:       busy,
		Restart: func(exe string) {
			restartPath = exe
			restartRequested.Store(true)
			stop()
		},
	}
	if in.Kind == update.KindArgoCD {
		client, _, err := kube.InCluster()
		if err != nil {
			log.Warn("updates: can't reach Kubernetes to move the Argo CD Application", "err", err)
			m.Install.CanApply = false
		} else {
			ns, name := update.ArgoApplication(in.Application)
			m.Argo = &kube.ArgoApp{Client: client, Namespace: ns, Name: name}
		}
	}
	log.Info("updates", "install", m.Install.Kind, "can_install", m.Install.CanApply)
	return m
}

// newRestart is Settings' Restart: the server stops the way it does for a
// deploy and starts the same binary again. nil where it can't.
func newRestart(stop func()) func() {
	if !canRestart() {
		return nil
	}
	return func() {
		restartRequested.Store(true)
		stop()
	}
}

func envOr(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

// runAndRestart is run, starting the new binary in this one's place when
// it stopped for an update.
func runAndRestart() error {
	if err := run(); err != nil {
		return err
	}
	if restartRequested.Load() {
		return restart()
	}
	return nil
}
