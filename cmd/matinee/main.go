// Command matinee runs the Matinee media server.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/yeixio/matinee/internal/api"
	"github.com/yeixio/matinee/internal/config"
	"github.com/yeixio/matinee/internal/probe"
	"github.com/yeixio/matinee/internal/scan"
	"github.com/yeixio/matinee/internal/store"
	"github.com/yeixio/matinee/internal/stream"
	"github.com/yeixio/matinee/internal/version"
	"github.com/yeixio/matinee/web"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "matinee:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load(os.Args[1:])
	if err != nil {
		return err
	}
	level := slog.LevelInfo
	if os.Getenv("MATINEE_DEBUG") != "" {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	for _, bin := range []string{cfg.FFmpegPath, cfg.FFprobePath} {
		if _, err := exec.LookPath(bin); err != nil {
			log.Warn("not found; scanning and transcoding need it", "binary", bin)
		}
	}

	db, err := store.Open(cfg.DBPath())
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	scanner := &scan.Scanner{Store: db, Prober: probe.Prober{Path: cfg.FFprobePath}, Log: log}
	transcoder := stream.NewTranscoder(cfg.FFmpegPath, cfg.TranscodeDir(), log)
	defer transcoder.Close()

	srv := &http.Server{
		Addr: cfg.Addr,
		Handler: (&api.Server{
			Store: db, Scanner: scanner, Transcoder: transcoder,
			Web: web.FS(), Log: log, BackgroundCtx: ctx,
		}).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	if cfg.ScanInterval > 0 {
		go func() {
			scanner.ScanAll(ctx)
			t := time.NewTicker(cfg.ScanInterval)
			defer t.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-t.C:
					scanner.ScanAll(ctx)
				}
			}
		}()
	}

	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	log.Info("matinee started", "version", version.Version, "addr", cfg.Addr, "data", cfg.DataDir)

	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		log.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		srv.Shutdown(shutdownCtx)
	}
	return nil
}
