// Package config loads server settings from flags and environment variables.
package config

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// DefaultTMDBKey is the project's own TMDB key, linked into official builds with
// -ldflags "-X github.com/yeixio/matinee/internal/config.DefaultTMDBKey=...", so
// posters work without users registering a key. MATINEE_TMDB_KEY overrides it.
var DefaultTMDBKey = ""

type Config struct {
	Addr         string        // HTTP listen address
	DataDir      string        // SQLite database (when used), image cache and transcode scratch space
	FFmpegPath   string        // ffmpeg binary
	FFprobePath  string        // ffprobe binary
	ScanInterval time.Duration // periodic library rescan; 0 disables it
	TMDBKey      string        // themoviedb.org API key or read token; empty disables online metadata
	DatabaseURL  string        // postgres:// URL; empty uses SQLite in DataDir
	MediaRoot    string        // where the folder picker starts when adding a library
	MusicBrainz  bool          // look up artists and albums on MusicBrainz (no key needed)
	Wikidata     bool          // look up movies' and shows' awards on Wikidata (no key needed)
	Discovery    bool          // announce the server on the local network so the apps find it
	ServerName   string        // the name the apps show for this server
	PublicURL    string        // the address the apps should connect to; empty uses this host's IP and port
	HomeAddr     string        // a second listen address only for the home network; empty means Addr serves both
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
	fs.StringVar(&c.MediaRoot, "media-root", env("MATINEE_MEDIA_ROOT", "/"), "folder the library picker starts in")
	fs.BoolVar(&c.Wikidata, "wikidata", env("MATINEE_WIKIDATA", "true") != "false", "look up awards on wikidata.org")
	fs.BoolVar(&c.MusicBrainz, "musicbrainz", env("MATINEE_MUSICBRAINZ", "true") != "false", "look up artists and albums on musicbrainz.org")
	fs.BoolVar(&c.Discovery, "discovery", env("MATINEE_DISCOVERY", "true") != "false", "announce this server on the local network so the apps find it")
	fs.StringVar(&c.ServerName, "server-name", env("MATINEE_SERVER_NAME", "Matinee"), "the name the apps show for this server")
	fs.StringVar(&c.PublicURL, "public-url", env("MATINEE_PUBLIC_URL", ""), "the address the apps should connect to, such as https://matinee.example.com")
	fs.StringVar(&c.HomeAddr, "home-addr", env("MATINEE_HOME_ADDR", ""), "listen address for the home network alone, when the main one sits behind a proxy (see the README)")
	interval := fs.String("scan-interval", env("MATINEE_SCAN_INTERVAL", "15m"), "periodic rescan interval, 0 to disable")
	if err := fs.Parse(args); err != nil {
		return c, err
	}
	d, err := time.ParseDuration(*interval)
	if err != nil {
		return c, fmt.Errorf("scan-interval: %w", err)
	}
	c.ScanInterval = d
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
