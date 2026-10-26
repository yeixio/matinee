package api

import (
	"context"
	"net/http"
	"strconv"

	"github.com/yeixio/matinee/internal/remote"
)

// Remote access: with it on, the server opens a port on the home router and
// gets a certificate for the router's public address, so the apps reach it
// away from home without anyone setting up the router (internal/remote).
// It's off until an admin turns it on, since it opens a port to the
// internet. A server with its own address (MATINEE_PUBLIC_URL), such as one
// behind an ingress, doesn't need it.

type remoteKey struct{}

// RemoteHandler serves the same API as Handler, on the remote access port.
// Nothing that arrives there counts as coming from home.
func (s *Server) RemoteHandler() http.Handler {
	h := s.Handler()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), remoteKey{}, true)))
	})
}

// SettingRemoteAccess holds serverSettings.RemoteAccess. The server reads it
// at startup.
const SettingRemoteAccess = "remote_access"

// RemoteAccessOn reads the setting.
func RemoteAccessOn(ctx context.Context, s settingStore) (bool, error) {
	v, err := s.Setting(ctx, SettingRemoteAccess)
	return v == "true", err
}

type settingStore interface {
	Setting(ctx context.Context, key string) (string, error)
}

// remoteURL is where the apps reach this server away from home, or "".
func (s *Server) remoteURL() string {
	if s.PublicURL != "" {
		return s.PublicURL
	}
	if s.Remote != nil {
		return s.Remote.URL()
	}
	return ""
}

func (s *Server) setRemoteAccess(ctx context.Context, on bool) error {
	if err := s.Store.SetSetting(ctx, SettingRemoteAccess, strconv.FormatBool(on)); err != nil {
		return err
	}
	if s.Remote != nil {
		s.Remote.SetEnabled(on)
	}
	return nil
}

func (s *Server) remoteStatus() *remote.Status {
	if s.Remote == nil {
		return nil
	}
	st := s.Remote.Status()
	return &st
}
