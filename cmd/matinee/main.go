// Command matinee runs the Matinee media server.
package main

import (
	"cmp"
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/yeixio/matinee/internal/analyze"
	"github.com/yeixio/matinee/internal/api"
	"github.com/yeixio/matinee/internal/artwork"
	"github.com/yeixio/matinee/internal/config"
	"github.com/yeixio/matinee/internal/connect"
	"github.com/yeixio/matinee/internal/discovery"
	"github.com/yeixio/matinee/internal/download"
	"github.com/yeixio/matinee/internal/hwaccel"
	"github.com/yeixio/matinee/internal/kube"
	"github.com/yeixio/matinee/internal/live"
	"github.com/yeixio/matinee/internal/metadata"
	"github.com/yeixio/matinee/internal/plugin"
	"github.com/yeixio/matinee/internal/podcast"
	"github.com/yeixio/matinee/internal/probe"
	"github.com/yeixio/matinee/internal/rating"
	"github.com/yeixio/matinee/internal/remote"
	"github.com/yeixio/matinee/internal/scan"
	"github.com/yeixio/matinee/internal/spectrum"
	"github.com/yeixio/matinee/internal/store"
	"github.com/yeixio/matinee/internal/stream"
	"github.com/yeixio/matinee/internal/subtitle"
	"github.com/yeixio/matinee/internal/theme"
	"github.com/yeixio/matinee/internal/version"
	"github.com/yeixio/matinee/web"
)

func main() {
	cmd := run
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "init-db-secret":
			cmd = initDBSecret
		case "announce":
			cmd = announce
		}
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

	// Titles looked up before kids profiles get their rating levels now.
	country := strings.ToUpper(cfg.RatingCountry)
	if n, err := db.BackfillRatingLevels(ctx, func(cert string) int { return rating.Level(country, cert) }); err != nil {
		log.Warn("fill in rating levels", "err", err)
	} else if n > 0 {
		log.Info("filled in rating levels", "titles", n)
	}

	scanner := &scan.Scanner{Store: db, Prober: probe.Prober{Path: cfg.FFprobePath}, Log: log}
	fetcher := &metadata.Fetcher{Store: db, Log: log}
	// An admin can change the TMDB key in Settings (see internal/api/tmdbkey.go);
	// without one, TMDB looks nothing up until they do.
	tmdbKey, err := api.TMDBKey(ctx, db, cfg.TMDBKey)
	if err != nil {
		return err
	}
	tmdb := metadata.NewTMDB(tmdbKey)
	tmdb.Region = country
	fetcher.Providers = append(fetcher.Providers, tmdb)
	if tmdbKey == "" {
		log.Info("no TMDB key set; movies and TV use local artwork only (add one in Settings or set MATINEE_TMDB_KEY for posters and descriptions)")
	}
	if cfg.Wikidata {
		fetcher.Awards = metadata.NewWikidata(version.Version)
	}
	if cfg.MusicBrainz {
		fetcher.Providers = append(fetcher.Providers, metadata.NewMusicBrainz(version.Version))
	}
	if cfg.OpenLibrary {
		fetcher.Providers = append(fetcher.Providers, metadata.NewOpenLibrary(version.Version))
	}
	db.SetMetadataKinds(fetcher.Kinds()...)
	// Testing the GPU takes a few seconds; transcodes use the CPU until it's done.
	hw := hwaccel.NewManager(cfg.FFmpegPath, log)
	pref, err := db.Setting(ctx, api.SettingHardwareAcceleration)
	if err != nil {
		return err
	}
	hw.SetPreference(pref)
	go hw.Detect(ctx)
	transcoder := stream.NewTranscoder(cfg.FFmpegPath, cfg.TranscodeDir(), hw, log)
	defer transcoder.Close()

	// Media analysis reads every file once more, slowly and at the lowest
	// priority, for the details the scan doesn't need (see internal/analyze).
	analyzer := &analyze.Analyzer{
		Store: db, Log: log,
		Prober: probe.Prober{Path: cfg.FFprobePath, Timeout: 2 * time.Minute, LowPriority: true},
		Busy:   func() bool { return transcoder.Active() > 0 },
	}
	// Plugins add library kinds, details and event handlers (internal/plugin).
	var plugins *plugin.Manager
	if cfg.Plugins {
		plugins, err = plugin.Open(ctx, cfg.PluginDir(), db, log)
		if err != nil {
			return fmt.Errorf("plugins: %w", err)
		}
		defer plugins.Close(context.Background())
		fetcher.Plugins = plugins.Providers
		db.SetMetadataKinds(fetcher.Kinds()...)
		plugins.OnChange = func() { db.SetMetadataKinds(fetcher.Kinds()...) }
	}
	// What's playing where, remote control, and the event feed for home
	// automation (internal/live).
	hub := live.New()
	go hub.Run(ctx)
	scanner.Added = func(ctx context.Context, lib store.Library, id int64) {
		plugins.ItemAdded(ctx, id)
		if it, err := db.Item(ctx, 0, id); err == nil {
			e := api.LiveItem(it)
			hub.Publish(live.Event{Type: live.EventItemAdded, Item: &e, Library: &live.Library{ID: lib.ID, Name: lib.Name, Kind: lib.Kind}})
		}
	}
	scanner.AfterScan = func(ctx context.Context, lib store.Library) {
		analyzer.Wake()
		fetcher.FetchLibrary(ctx, lib)
		plugins.Emit(plugin.LibraryEvent(plugin.EventLibraryScanned, lib.ID, lib.Name, lib.Kind))
		hub.Publish(live.Event{Type: live.EventLibraryScanned, Library: &live.Library{ID: lib.ID, Name: lib.Name, Kind: lib.Kind}})
	}
	go analyzer.Run(ctx)

	// New files show up seconds after they land (see internal/scan/watch.go).
	watcher := scan.NewWatcher(scanner, log)
	watcher.PollInterval = cfg.WatchPoll
	go watcher.Run(ctx)

	downloads := download.NewManager(db, cfg.FFmpegPath, cfg.DownloadDir(), log)

	uuid, err := api.ServerUUID(ctx, db)
	if err != nil {
		return err
	}
	apiServer := &api.Server{
		Store: db, Scanner: scanner, Watcher: watcher, ScanInterval: cfg.ScanInterval, Transcoder: transcoder, HW: hw, Downloads: downloads, Analyzer: analyzer,
		Art:       artwork.NewCache(cfg.ImageDir(), cfg.FFmpegPath),
		Subtitles: subtitle.NewConverter(cfg.FFmpegPath, cfg.SubtitleDir()),
		Levels:    spectrum.New(cfg.FFmpegPath, cfg.LevelsDir()),
		Web:       web.FS(), Log: log, BackgroundCtx: ctx,
		MediaRoot: cfg.MediaRoot, Name: cfg.ServerName,
		HomeListener: cfg.HomeAddr != "", ThemeDir: cfg.ThemeDir(),
		PublicURL: cfg.PublicURL,
		Plugins:   plugins, PluginCatalog: cfg.PluginCatalog,
		Live: hub, UUID: uuid,
	}
	if _, problems := theme.Load(cfg.ThemeDir()); len(problems) > 0 {
		for _, p := range problems {
			log.Warn("skipping a theme file", "dir", cfg.ThemeDir(), "err", p)
		}
	}
	apiServer.TrailerSource = tmdb
	apiServer.People = tmdb
	apiServer.Translate = fetcher.TranslateAll
	apiServer.Keywords = tmdb
	apiServer.TMDB, apiServer.TMDBFallback, apiServer.TMDBFallbackSource = tmdb, cfg.TMDBKey, api.TMDBKeyFromEnvironment
	if cfg.TMDBKey != "" && cfg.TMDBKey == config.DefaultTMDBKey {
		apiServer.TMDBFallbackSource = api.TMDBKeyBuiltIn
	}
	// A new key fills in what the old one couldn't: titles never looked up
	// count as waiting again, and a scan of every library looks them up.
	apiServer.OnTMDBKey = func() {
		db.SetMetadataKinds(fetcher.Kinds()...)
		scanner.ScanAll(ctx)
	}
	serving, stopServing := context.WithCancel(context.Background())
	defer stopServing()
	if cfg.RemoteAddr != "" {
		apiServer.Remote = &remote.Manager{Addr: cfg.RemoteAddr, Dir: cfg.RemoteDir(), Handler: apiServer.RemoteHandler(), Log: log}
		if cfg.ConnectURL != "" {
			key, err := connect.LoadKey(filepath.Join(cfg.RemoteDir(), "server.key"))
			if err != nil {
				return err
			}
			apiServer.Remote.Connect = &connect.Client{URL: cfg.ConnectURL, Key: key}
			if cfg.BillingURL != "" {
				apiServer.Remote.Billing = &connect.Billing{URL: cfg.BillingURL}
			}
		}
		on, err := api.RemoteAccessOn(ctx, db)
		if err != nil {
			return err
		}
		port, err := api.RemoteForwardedPort(ctx, db)
		if err != nil {
			return err
		}
		apiServer.Remote.SetForwardedPort(port)
		apiServer.Remote.SetEnabled(on)
		// Remote access keeps serving while the server drains (see below).
		go apiServer.Remote.Run(serving)
	}
	downloads.SubtitleStream = apiServer.BurnStream
	go downloads.Run(ctx)
	// Podcast libraries follow their shows' feeds; scanning one checks them now.
	podcasts := &podcast.Manager{Store: db, Log: log, Added: func(ctx context.Context, libraryID, id int64) {
		if lib, err := db.Library(ctx, libraryID); err == nil {
			scanner.Added(ctx, lib, id)
		}
	}}
	apiServer.Podcasts = podcasts
	scanner.Refresh = podcasts.RefreshStale
	stream.AllowHomeNetwork.Store(cfg.PodcastsHomeNetwork)
	go podcasts.Run(ctx)

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           apiServer.Handler(),
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

	if cfg.Discovery {
		go func() {
			port := listenPort(cfg.Addr)
			if cfg.HomeAddr != "" {
				port = listenPort(cfg.HomeAddr)
			}
			a := discovery.Announcement{Name: cfg.ServerName, Port: port, URL: cfg.PublicURL, Version: version.Version, Home: true, UUID: uuid}
			if err := discovery.Run(ctx, a, log); err != nil {
				log.Warn("apps on this network won't find the server by themselves; they can still connect by address", "err", err)
			}
		}()
	}

	errc := make(chan error, 2)
	go func() { errc <- srv.ListenAndServe() }()
	// The home network's own entrance, when the main address is behind a proxy.
	var homeSrv *http.Server
	if cfg.HomeAddr != "" {
		homeSrv = &http.Server{Addr: cfg.HomeAddr, Handler: apiServer.HomeHandler(), ReadHeaderTimeout: 10 * time.Second}
		go func() { errc <- homeSrv.ListenAndServe() }()
	}
	dbKind := "sqlite"
	if cfg.DatabaseURL != "" {
		dbKind = "postgres"
	}
	log.Info("matinee started", "version", version.Version, "addr", cfg.Addr, "home_addr", cfg.HomeAddr, "remote_addr", cfg.RemoteAddr, "data", cfg.DataDir, "database", dbKind)

	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		// Players hear that the server is restarting, so they can say so
		// and pick up where they were once it's back (a deploy or restart).
		log.Info("shutting down", "drain_delay", cfg.DrainDelay, "timeout", cfg.ShutdownTimeout)
		apiServer.Drain()
		time.Sleep(cfg.DrainDelay)
		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		var wg sync.WaitGroup
		for _, s := range []*http.Server{srv, homeSrv} {
			if s != nil {
				wg.Go(func() { s.Shutdown(shutdownCtx) })
			}
		}
		wg.Wait()
		stopServing()
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

// announce runs on its own to make a server the apps can't otherwise see on
// the local network findable, such as one inside Kubernetes: the Helm chart
// runs it on the host network, pointing at the server's public address.
func announce() error {
	fs := flag.NewFlagSet("announce", flag.ContinueOnError)
	var a discovery.Announcement
	fs.StringVar(&a.URL, "url", os.Getenv("MATINEE_PUBLIC_URL"), "the server's address, such as https://matinee.example.com")
	fs.StringVar(&a.Name, "name", cmp.Or(os.Getenv("MATINEE_SERVER_NAME"), "Matinee"), "the name the apps show")
	homePort := fs.Int("home-port", 0, "a port on this host that serves Matinee to the home network (the server's MATINEE_HOME_ADDR)")
	if err := fs.Parse(os.Args[2:]); err != nil {
		return err
	}
	if a.URL == "" {
		return errors.New("announce needs -url")
	}
	u, err := url.Parse(a.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("-url %q isn't an http(s) address", a.URL)
	}
	a.Port = 443
	if u.Scheme == "http" {
		a.Port = 80
	}
	if p, err := strconv.Atoi(u.Port()); err == nil {
		a.Port = p
	}
	if *homePort > 0 {
		a.Port, a.Home = *homePort, true
	}
	a.Version = version.Version
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return discovery.Run(ctx, a, slog.New(slog.NewTextHandler(os.Stderr, nil)))
}

// listenPort is the port in an address like ":7171" or "0.0.0.0:8080".
func listenPort(addr string) int {
	_, p, err := net.SplitHostPort(addr)
	if n, perr := strconv.Atoi(p); err == nil && perr == nil {
		return n
	}
	return 7171
}
