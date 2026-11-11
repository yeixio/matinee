// Command matinee runs the Matinee media server.
package main

import (
	"cmp"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
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
	"sync/atomic"
	"syscall"
	"time"

	"github.com/yeixio/matinee/internal/alerts"
	"github.com/yeixio/matinee/internal/analyze"
	"github.com/yeixio/matinee/internal/api"
	"github.com/yeixio/matinee/internal/artwork"
	"github.com/yeixio/matinee/internal/backup"
	"github.com/yeixio/matinee/internal/config"
	"github.com/yeixio/matinee/internal/connect"
	"github.com/yeixio/matinee/internal/credits"
	"github.com/yeixio/matinee/internal/discovery"
	"github.com/yeixio/matinee/internal/download"
	"github.com/yeixio/matinee/internal/gapless"
	"github.com/yeixio/matinee/internal/health"
	"github.com/yeixio/matinee/internal/hwaccel"
	"github.com/yeixio/matinee/internal/intro"
	"github.com/yeixio/matinee/internal/kube"
	"github.com/yeixio/matinee/internal/live"
	"github.com/yeixio/matinee/internal/livetv"
	"github.com/yeixio/matinee/internal/lyrics"
	"github.com/yeixio/matinee/internal/metadata"
	"github.com/yeixio/matinee/internal/netstate"
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
	"github.com/yeixio/matinee/internal/thumbs"
	"github.com/yeixio/matinee/internal/together"
	"github.com/yeixio/matinee/internal/version"
	"github.com/yeixio/matinee/web"
)

// databaseWait is how long the server waits for a PostgreSQL server that
// doesn't answer yet before it gives up.
const databaseWait = 150 * time.Second

func main() {
	cmd := runAndRestart
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
	// The admin dashboard shows the latest log lines too (internal/health).
	logs := health.NewLogs(health.DefaultLines)
	log := slog.New(logs.Handler(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))
	monitor := health.NewMonitor(logs)
	// Calls to sites on the internet fail at once while they're known to be
	// down, so an outage never holds up a page, a scan or playback.
	connections := netstate.New()
	direct := http.DefaultTransport
	http.DefaultTransport = connections.Wrap(direct)
	stream.Remote.Transport = connections.Wrap(stream.Remote.Transport)
	livetv.Client.Transport = connections.Wrap(livetv.Client.Transport)
	useWritableCacheDir(cfg.DataDir, log)

	for _, bin := range []string{cfg.FFmpegPath, cfg.FFprobePath} {
		if _, err := exec.LookPath(bin); err != nil {
			log.Warn("not found; scanning and transcoding need it", "binary", bin)
		}
	}

	// A SQLite database is backed up before an upgrade changes it, every
	// day, and when an admin asks (internal/backup); a restore an admin
	// asked for takes its place now, before it's opened.
	var backups *backup.Manager
	var opts store.Options
	if cfg.DatabaseURL == "" {
		backups = &backup.Manager{Dir: cfg.BackupDir(), DBPath: cfg.Database(), Log: log}
		if _, err := backup.ApplyRestore(context.Background(), cfg.Database(), cfg.BackupDir(), log); err != nil {
			return fmt.Errorf("restore the database: %w", err)
		}
		opts.BeforeMigrate = backups.BeforeMigrate
	} else {
		// PostgreSQL may be starting at the same time (both pods replaced at
		// once on Kubernetes). Until it answers the server isn't listening,
		// so it isn't ready either; the chart's startup probe allows 3 minutes.
		opts.Wait = databaseWait
		opts.Waiting = func(err error, next time.Duration) {
			log.Warn("waiting for the database", "retry_in", next, "err", err)
		}
	}
	db, err := store.OpenWith(cfg.Database(), opts)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()
	if backups != nil {
		backups.Store = db
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// Windows has no SIGTERM, so the Windows tray app (cmd/matinee-tray)
	// stops the server by closing its stdin. That also stops it if the tray
	// app dies.
	if os.Getenv("MATINEE_STOP_ON_STDIN_CLOSE") == "1" {
		go func() {
			io.Copy(io.Discard, os.Stdin)
			stop()
		}()
	}

	// Titles looked up before kids profiles get their rating levels now.
	country := strings.ToUpper(cfg.RatingCountry)
	if n, err := db.BackfillRatingLevels(ctx, func(cert string) int { return rating.Level(country, cert) }); err != nil {
		log.Warn("fill in rating levels", "err", err)
	} else if n > 0 {
		log.Info("filled in rating levels", "titles", n)
	}

	// A file the watcher sees deleted waits a while for the copy that
	// replaces it, so its watch history moves over to the new file.
	scanner := &scan.Scanner{Store: db, Prober: probe.Prober{Path: cfg.FFprobePath}, Log: log, MissingGrace: 10 * time.Minute}
	fetcher := &metadata.Fetcher{Store: db, Log: log, Edition: scan.Edition}
	// An admin can change the TMDB key in Settings (see internal/api/tmdbkey.go);
	// without one, TMDB looks nothing up until they do.
	tmdbKey, err := api.TMDBKey(ctx, db, cfg.TMDBKey)
	if err != nil {
		return err
	}
	tmdb := metadata.NewTMDB(tmdbKey)
	tmdb.Region = country
	// Without a TMDB key, movies and shows get their artwork through Radarr
	// and Sonarr (or Box Office), if they're connected.
	arrMeta := &metadata.Arr{Store: db, TMDB: tmdb}
	fetcher.Providers = append(fetcher.Providers, tmdb, arrMeta)
	if tmdbKey == "" {
		log.Info("no TMDB key set; movies and TV use local artwork, or Radarr's and Sonarr's once connected (add a key in Settings or set MATINEE_TMDB_KEY for full details)")
	}
	if cfg.Wikidata {
		fetcher.Awards = metadata.NewWikidata(version.Version)
	}
	// Covers for admins to pick share the lookups' clients, which keep to
	// each service's rate limit.
	var mb *metadata.MusicBrainz
	var ol *metadata.OpenLibrary
	if cfg.MusicBrainz {
		mb = metadata.NewMusicBrainz(version.Version)
		fetcher.Providers = append(fetcher.Providers, mb)
	}
	if cfg.OpenLibrary {
		ol = metadata.NewOpenLibrary(version.Version)
		fetcher.Providers = append(fetcher.Providers, ol)
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
	if cfg.CacheLimit > 0 {
		// The volume has a cap below its disk (a Kubernetes emptyDir): what
		// else is in the data folder counts against it too.
		transcoder.Limit = stream.SpaceLimit{Size: cfg.CacheLimit, Others: othersInCache(ctx, cfg.DataDir, cfg.TranscodeDir())}
	}

	thumbnails := &thumbs.Generator{Store: db, FFmpeg: cfg.FFmpegPath, Dir: cfg.ThumbnailDir(), Log: log, LowPriority: probe.LowerPriority}
	if err := os.MkdirAll(cfg.ThumbnailDir(), 0o755); err != nil {
		return err
	}
	// Videos someone plays get their pictures right away.
	go thumbnails.Serve(ctx)

	// Media analysis reads every file once more, slowly and at the lowest
	// priority, for the details the scan doesn't need (see internal/analyze).
	analyzer := &analyze.Analyzer{
		Store: db, Log: log,
		Prober: probe.Prober{Path: cfg.FFprobePath, Timeout: 2 * time.Minute, LowPriority: true},
		Busy:   func() bool { return transcoder.Active() > 0 },
		// Then it finds each TV episode's intro by matching the audio of a
		// season's episodes (see internal/intro).
		Intros: &intro.Detector{Store: db, FFmpeg: cfg.FFmpegPath, Log: log, LowPriority: probe.LowerPriority},
		// Then where each episode's and movie's end credits start, from the
		// music a season's episodes share at their end or the picture
		// going dark (see internal/credits).
		Credits: &credits.Detector{Store: db, FFmpeg: cfg.FFmpegPath, Log: log, LowPriority: probe.LowerPriority},
		// And listens for albums whose songs run into each other, so they
		// play without gaps (see internal/gapless).
		Gapless: &gapless.Detector{Store: db, FFmpeg: cfg.FFmpegPath, Log: log, LowPriority: probe.LowerPriority,
			Prober: probe.Prober{Path: cfg.FFprobePath, Timeout: time.Minute, LowPriority: true}},
		// And the seek bar's pictures of every video (see internal/thumbs).
		Thumbs: thumbnails,
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
	// Watch together sessions (internal/together).
	sessions := together.New()
	go sessions.Run(ctx)
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
		Store: db, Scanner: scanner, Watcher: watcher, ScanInterval: cfg.ScanInterval, Transcoder: transcoder, HW: hw, Downloads: downloads, Analyzer: analyzer, Thumbs: thumbnails,
		Art:       artwork.NewCache(cfg.ImageDir(), cfg.FFmpegPath),
		Covers:    metadata.NewCovers(version.Version, mb, ol),
		Subtitles: subtitle.NewConverter(cfg.FFmpegPath, cfg.SubtitleDir()).WithFFprobe(cfg.FFprobePath),
		Levels:    spectrum.New(cfg.FFmpegPath, cfg.LevelsDir()),
		Lyrics: &lyrics.Finder{
			Prober: probe.Prober{Path: cfg.FFprobePath},
			// Only asked when an admin turns on online lyrics in Settings.
			Online: &lyrics.LRCLIB{UserAgent: "Matinee/" + strings.TrimPrefix(version.Version, "v") + " (+https://matinee.yeix.io)", Dir: cfg.LyricsDir(), Client: &http.Client{Timeout: 15 * time.Second}},
		},
		Web: web.FS(), Log: log, BackgroundCtx: ctx,
		MediaRoot: cfg.MediaRoot, Name: cfg.ServerName,
		HomeListener: cfg.HomeAddr != "", ThemeDir: cfg.ThemeDir(),
		PublicURL: cfg.PublicURL,
		Plugins:   plugins, PluginCatalog: cfg.PluginCatalog,
		Live: hub, Together: sessions, UUID: uuid,
		Health: monitor,
		Storage: []api.StoragePath{
			{Use: "data", Path: cfg.DataDir}, {Use: "transcode", Path: cfg.TranscodeDir()}, {Use: "thumbnails", Path: cfg.ThumbnailDir()},
			{Use: "images", Path: cfg.ImageDir()}, {Use: "downloads", Path: cfg.DownloadDir()},
		},
	}
	monitor.Streams = apiServer.StreamCounts
	go monitor.Run(ctx)
	// Admin alerts (Settings → Server health → Alerts) go straight to the
	// admins' own mail server and webhooks.
	alerter := &alerts.Manager{Store: db, Log: log, Server: cfg.ServerName, URL: cfg.PublicURL, Version: version.Version, Read: apiServer.AlertReadings}
	apiServer.Alerts = alerter
	transcoder.OnNoSpace = func(detail string) { alerter.TranscodeSpace(ctx, detail) }
	if backups != nil {
		backups.Failed = alerter.BackupFailed
		apiServer.Backups = backups
		go backups.Run(ctx)
	}
	// Settings → Server health → Updates (internal/update).
	updates := newUpdates(db, log, stop, func() bool {
		playing, transcodes := apiServer.StreamCounts()
		return playing+transcodes > 0
	})
	apiServer.Updates = updates
	go updates.Run(ctx)
	if _, problems := theme.Load(cfg.ThemeDir()); len(problems) > 0 {
		for _, p := range problems {
			log.Warn("skipping a theme file", "dir", cfg.ThemeDir(), "err", p)
		}
	}
	apiServer.TrailerSource = tmdb
	apiServer.Matcher, apiServer.Refetch = tmdb, fetcher.FetchLibrary
	apiServer.Connections = connections
	go connections.Run(ctx, direct)
	// Titles added while the internet was down get their details as soon
	// as it's back, without waiting for the next scan.
	connections.Back = func() {
		log.Info("the internet is back; looking up what waited for it")
		libs, err := db.Libraries(ctx)
		if err != nil {
			return
		}
		for _, lib := range libs {
			fetcher.FetchLibrary(ctx, lib)
		}
	}
	apiServer.People = tmdb
	apiServer.Translate = fetcher.TranslateAll
	apiServer.Keywords = tmdb
	apiServer.TMDB, apiServer.TMDBFallback, apiServer.TMDBFallbackSource = tmdb, cfg.TMDBKey, api.TMDBKeyFromEnvironment
	if cfg.TMDBKey != "" && cfg.TMDBKey == config.DefaultTMDBKey {
		apiServer.TMDBFallbackSource = api.TMDBKeyBuiltIn
	}
	// A new key fills in what the old one couldn't: titles never looked up
	// count as waiting again, and a scan of every library looks them up.
	// A new Radarr or Sonarr connection can look up what TMDB couldn't,
	// without a key.
	apiServer.OnArr = func() {
		arrMeta.Forget()
		db.SetMetadataKinds(fetcher.Kinds()...)
		libs, err := db.Libraries(ctx)
		if err != nil {
			return
		}
		for _, lib := range libs {
			fetcher.FetchLibrary(ctx, lib)
		}
	}
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
	// Live TV sources' channels and guides, and checks of their streams.
	liveTV := &livetv.Manager{Store: db, Log: log}
	apiServer.LiveTV = liveTV
	go liveTV.Run(ctx)

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
	go alerter.Run(ctx)
	// Box Office's notifications go on to the people they're for.
	go apiServer.RunNotifications(ctx)
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
		// A clean stop, so the next start isn't taken for a crash.
		stoppedCtx, cancelStopped := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancelStopped()
		alerter.Stopped(stoppedCtx)
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

// othersInCache is how much of the data folder's volume everything but
// transcodes uses, measured now and every five minutes after (walking the
// image cache each time a transcode checks its room would be slow).
func othersInCache(ctx context.Context, dataDir, transcodeDir string) func() int64 {
	var used atomic.Int64
	used.Store(stream.DirSize(dataDir, transcodeDir))
	go func() {
		tick := time.NewTicker(5 * time.Minute)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				used.Store(stream.DirSize(dataDir, transcodeDir))
			}
		}
	}()
	return used.Load
}
