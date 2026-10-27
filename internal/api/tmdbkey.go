package api

import (
	"cmp"
	"context"
	"errors"
	"strings"

	"github.com/yeixio/matinee/internal/metadata"
	"github.com/yeixio/matinee/internal/store"
)

// SettingTMDBKey holds a TMDB key an admin entered in Settings. It wins over
// MATINEE_TMDB_KEY and a key built into the server; clearing it goes back to
// those.
const SettingTMDBKey = "tmdb_api_key"

// Where the TMDB key in use comes from (tmdbStatus.Source).
const (
	TMDBKeyFromSettings    = "settings"
	TMDBKeyFromEnvironment = "environment" // MATINEE_TMDB_KEY or --tmdb-key
	TMDBKeyBuiltIn         = "built-in"
	TMDBKeyNone            = "none"
)

// TMDBKeyHolder is the TMDB client whose key admins can change while the
// server runs. *metadata.TMDB is one.
type TMDBKeyHolder interface {
	Key() string
	SetKey(key string)
	// Check asks TMDB whether it accepts key.
	Check(ctx context.Context, key string) error
}

// TMDBKey is the key the server starts with: the one saved in Settings, or
// else fallback (from the environment or built in).
func TMDBKey(ctx context.Context, st *store.Store, fallback string) (string, error) {
	key, err := st.Setting(ctx, SettingTMDBKey)
	if key == "" {
		key = fallback
	}
	return key, err
}

// tmdbStatus is serverSettings.TMDB. The key itself never leaves the server.
type tmdbStatus struct {
	// Source is where the key in use comes from: settings, environment,
	// built-in, or none.
	Source string `json:"source"`
	// Hint is the last four characters of the key in use, to tell keys apart.
	Hint string `json:"hint,omitempty"`
}

func (s *Server) tmdbStatus(ctx context.Context) (tmdbStatus, error) {
	if s.TMDB == nil {
		return tmdbStatus{Source: TMDBKeyNone}, nil
	}
	saved, err := s.Store.Setting(ctx, SettingTMDBKey)
	key := s.TMDB.Key()
	st := tmdbStatus{Source: TMDBKeyNone}
	switch {
	case key == "":
	case saved != "":
		st.Source = TMDBKeyFromSettings
	default:
		st.Source = cmp.Or(s.TMDBFallbackSource, TMDBKeyFromEnvironment)
	}
	if len(key) >= 8 {
		st.Hint = key[len(key)-4:]
	}
	return st, err
}

var (
	errNoTMDB          = errors.New("this server can't change its TMDB key")
	errTMDBUnreachable = errors.New("TMDB couldn't be reached")
)

// setTMDBKey saves the key an admin entered, once TMDB accepts it, and starts
// using it; "" goes back to the environment's or built-in key. A scan of
// every library then fills in what the old key couldn't. A key TMDB turns
// down fails with metadata.ErrBadKey.
func (s *Server) setTMDBKey(ctx context.Context, key string) error {
	if s.TMDB == nil {
		return errNoTMDB
	}
	key = strings.TrimSpace(key)
	if key != "" {
		if err := s.TMDB.Check(ctx, key); errors.Is(err, metadata.ErrBadKey) {
			return err
		} else if err != nil {
			s.Log.Warn("checking a TMDB key", "err", err)
			return errTMDBUnreachable
		}
	}
	if err := s.Store.SetSetting(ctx, SettingTMDBKey, key); err != nil {
		return err
	}
	s.TMDB.SetKey(cmp.Or(key, s.TMDBFallback))
	if s.OnTMDBKey != nil {
		go s.OnTMDBKey()
	}
	return nil
}

// ready reports whether an online source can be asked now: it's set, and
// has a key if it needs one (like *metadata.TMDB).
func ready(src any) bool {
	if src == nil {
		return false
	}
	r, ok := src.(interface{ Ready() bool })
	return !ok || r.Ready()
}
