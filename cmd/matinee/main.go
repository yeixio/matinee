// Command matinee runs the Matinee media server.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/yeixio/matinee/internal/api"
	"github.com/yeixio/matinee/internal/artwork"
	"github.com/yeixio/matinee/internal/config"
	"github.com/yeixio/matinee/internal/kube"
	"github.com/yeixio/matinee/internal/metadata"
	"github.com/yeixio/matinee/internal/probe"
	"github.com/yeixio/matinee/internal/scan"
	"github.com/yeixio/matinee/internal/store"
	"github.com/yeixio/matinee/internal/stream"
	"github.com/yeixio/matinee/internal/subtitle"
	"github.com/yeixio/matinee/internal/version"
	"github.com/yeixio/matinee/web"
)

func main() {
	cmd := run
	if len(os.Args) > 1 && os.Args[1] == "init-db-secret" {
		cmd = initDBSecret
	}
	if err := cmd(); err != nil {
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

	db, err := store.Open(cfg.Database())
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	scanner := &scan.Scanner{Store: db, Prober: probe.Prober{Path: cfg.FFprobePath}, Log: log}
	fetcher := &metadata.Fetcher{Store: db, Log: log}
	if cfg.TMDBKey != "" {
		fetcher.Providers = append(fetcher.Providers, metadata.NewTMDB(cfg.TMDBKey))
	} else {
		log.Info("no TMDB key set; movies and TV use local artwork only (set MATINEE_TMDB_KEY for posters and descriptions)")
	}
	if cfg.MusicBrainz {
		fetcher.Providers = append(fetcher.Providers, metadata.NewMusicBrainz(version.Version))
	}
	db.SetMetadataKinds(fetcher.Kinds()...)
	scanner.AfterScan = fetcher.FetchLibrary
	transcoder := stream.NewTranscoder(cfg.FFmpegPath, cfg.TranscodeDir(), log)
	defer transcoder.Close()

	srv := &http.Server{
		Addr: cfg.Addr,
		Handler: (&api.Server{
			Store: db, Scanner: scanner, Transcoder: transcoder,
			Art:       artwork.NewCache(cfg.ImageDir(), cfg.FFmpegPath),
			Subtitles: subtitle.NewConverter(cfg.FFmpegPath, cfg.SubtitleDir()),
			Web:       web.FS(), Log: log, BackgroundCtx: ctx,
			MediaRoot: cfg.MediaRoot,
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
	dbKind := "sqlite"
	if cfg.DatabaseURL != "" {
		dbKind = "postgres"
	}
	log.Info("matinee started", "version", version.Version, "addr", cfg.Addr, "data", cfg.DataDir, "database", dbKind)

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

// initDBSecret runs as the Helm chart's install hook: it creates the bundled
// PostgreSQL's credentials once, so a fresh install needs no hand-made Secret.
func initDBSecret() error {
	fs := flag.NewFlagSet("init-db-secret", flag.ContinueOnError)
	var s kube.DatabaseSecret
	fs.StringVar(&s.Name, "name", "matinee-db", "secret to create")
	fs.StringVar(&s.User, "user", "matinee", "database user")
	fs.StringVar(&s.Database, "database", "matinee", "database name")
	fs.StringVar(&s.Host, "host", "matinee-postgresql", "PostgreSQL service")
	if err := fs.Parse(os.Args[2:]); err != nil {
		return err
	}
	client, ns, err := kube.InCluster()
	if err != nil {
		return err
	}
	s.Namespace = ns
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	created, err := client.EnsureDatabaseSecret(ctx, s)
	if err != nil {
		return err
	}
	if created {
		fmt.Println("created database credentials in secret", s.Name)
	} else {
		fmt.Println("secret", s.Name, "already exists; keeping it")
	}
	return nil
}
