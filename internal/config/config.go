// Package config loads server settings from flags and environment variables.
package config

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/yeixio/matinee/internal/connect"
)

// DefaultTMDBKey is the project's own TMDB key, linked into official builds with
// -ldflags "-X github.com/yeixio/matinee/internal/config.DefaultTMDBKey=...", so
// posters work without users registering a key. MATINEE_TMDB_KEY overrides it.
var DefaultTMDBKey = ""

type Config struct {
	Addr          string        // HTTP listen address
	DataDir       string        // SQLite database (when used), image cache and transcode scratch space
	FFmpegPath    string        // ffmpeg binary
	FFprobePath   string        // ffprobe binary
	ScanInterval  time.Duration // periodic library rescan; 0 disables it
	WatchPoll     time.Duration // how often folders on network shares are checked for new files
	TMDBKey       string        // themoviedb.org API key or read token; empty disables online metadata
	RatingCountry string        // whose age ratings to use, such as US or GB
	DatabaseURL   string        // postgres:// URL; empty uses SQLite in DataDir
	MediaRoot     string        // where the folder picker starts when adding a library
	MusicBrainz   bool          // look up artists and albums on MusicBrainz (no key needed)
	Wikidata      bool          // look up movies' and shows' awards on Wikidata (no key needed)
	Discovery     bool          // announce the server on the local network so the apps find it
	ServerName    string        // the name the apps show for this server
	PublicURL     string        // the address the apps should connect to; empty uses this host's IP and port
	HomeAddr      string        // a second listen address only for the home network; empty means Addr serves both
	RemoteAddr    string        // HTTPS listen address for remote access (internal/remote); empty means the server doesn't offer it
	ConnectURL    string        // the service the apps find this server through when its address changes (internal/connect); empty for none
	BillingURL    string        // where that service's subscription is bought; empty for a service that needs none
}

// Database is what store.Open takes: the PostgreSQL URL, or the SQLite file.
func (c Config) Database() string {
	if c.DatabaseURL != "" {
		return c.DatabaseURL
	}
	return filepath.Join(c.DataDir, "matinee.db")
}

func (c Config) TranscodeDir() string { return filepath.Join(c.DataDir, "transcode") }
func (c Config) ImageDir() string     { return filepath.Join(c.DataDir, "images") }
func (c Config) SubtitleDir() string  { return filepath.Join(c.DataDir, "subtitles") }
func (c Config) DownloadDir() string  { return filepath.Join(c.DataDir, "downloads") }
func (c Config) RemoteDir() string    { return filepath.Join(c.DataDir, "remote") }

// ThemeDir holds an admin's own theme files (docs/themes.md).
func (c Config) ThemeDir() string { return filepath.Join(c.DataDir, "themes") }

// Load reads flags, falling back to MATINEE_* environment variables, then defaults.
func Load(args []string) (Config, error) {
	fs := flag.NewFlagSet("matinee", flag.ContinueOnError)
	c := Config{}
	fs.StringVar(&c.Addr, "addr", env("MATINEE_ADDR", ":7171"), "HTTP listen address")
	fs.StringVar(&c.DataDir, "data", env("MATINEE_DATA_DIR", "./data"), "data directory")
	fs.StringVar(&c.FFmpegPath, "ffmpeg", env("MATINEE_FFMPEG", "ffmpeg"), "path to ffmpeg")
	fs.StringVar(&c.FFprobePath, "ffprobe", env("MATINEE_FFPROBE", "ffprobe"), "path to ffprobe")
	fs.StringVar(&c.DatabaseURL, "database-url", env("MATINEE_DATABASE_URL", ""), "PostgreSQL URL (postgres://...); default is SQLite in the data directory")
	fs.StringVar(&c.TMDBKey, "tmdb-key", env("MATINEE_TMDB_KEY", DefaultTMDBKey), "themoviedb.org API key for posters and descriptions")
	fs.StringVar(&c.RatingCountry, "rating-country", env("MATINEE_RATING_COUNTRY", "US"), "country whose age ratings kids profiles go by, such as US, GB or DE")
	fs.StringVar(&c.MediaRoot, "media-root", env("MATINEE_MEDIA_ROOT", "/"), "folder the library picker starts in")
	fs.BoolVar(&c.Wikidata, "wikidata", env("MATINEE_WIKIDATA", "true") != "false", "look up awards on wikidata.org")
	fs.BoolVar(&c.MusicBrainz, "musicbrainz", env("MATINEE_MUSICBRAINZ", "true") != "false", "look up artists and albums on musicbrainz.org")
	fs.BoolVar(&c.Discovery, "discovery", env("MATINEE_DISCOVERY", "true") != "false", "announce this server on the local network so the apps find it")
	fs.StringVar(&c.ServerName, "server-name", env("MATINEE_SERVER_NAME", "Matinee"), "the name the apps show for this server")
	fs.StringVar(&c.PublicURL, "public-url", env("MATINEE_PUBLIC_URL", ""), "the address the apps should connect to, such as https://matinee.example.com")
	fs.StringVar(&c.HomeAddr, "home-addr", env("MATINEE_HOME_ADDR", ""), "listen address for the home network alone, when the main one sits behind a proxy (see the README)")
	fs.StringVar(&c.RemoteAddr, "remote-addr", env("MATINEE_REMOTE_ADDR", ":7443"), "HTTPS listen address for remote access, which an admin turns on in Settings; off to not offer it")
	fs.StringVar(&c.ConnectURL, "connect-url", env("MATINEE_CONNECT_URL", connect.DefaultURL), "service that tells the apps this server's current address while remote access is on (it never carries media); off for none")
	fs.StringVar(&c.BillingURL, "billing-url", env("MATINEE_BILLING_URL", ""), "where the connect service's subscription is bought; defaults to yeix.io for the Matinee project's service, off for none")
	interval := fs.String("scan-interval", env("MATINEE_SCAN_INTERVAL", "15m"), "periodic rescan interval, 0 to disable")
	watchPoll := fs.String("watch-poll-interval", env("MATINEE_WATCH_POLL_INTERVAL", "30s"), "how often folders on network shares are checked for new files")
	if err := fs.Parse(args); err != nil {
		return c, err
	}
	d, err := time.ParseDuration(*interval)
	if err != nil {
		return c, fmt.Errorf("scan-interval: %w", err)
	}
	c.ScanInterval = d
	if c.RemoteAddr == "off" {
		c.RemoteAddr = ""
	}
	if c.ConnectURL == "off" {
		c.ConnectURL = ""
	}
	switch {
	case c.BillingURL == "off" || c.ConnectURL == "":
		c.BillingURL = ""
	case c.BillingURL == "" && c.ConnectURL == connect.DefaultURL:
		c.BillingURL = connect.DefaultBillingURL
	}
	if c.WatchPoll, err = time.ParseDuration(*watchPoll); err != nil || c.WatchPoll < time.Second {
		return c, fmt.Errorf("watch-poll-interval: want a duration of at least 1s, like 30s")
	}
	if err := os.MkdirAll(c.TranscodeDir(), 0o755); err != nil {
		return c, fmt.Errorf("create data dir: %w", err)
	}
	return c, nil
}

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}
