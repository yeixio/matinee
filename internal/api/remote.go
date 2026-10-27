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

// SettingRemoteForwardedPort holds serverSettings.RemoteForwardedPort.
const SettingRemoteForwardedPort = "remote_forwarded_port"

// RemoteForwardedPort reads the setting: the router port an admin forwarded
// to the remote access port by hand, or 0.
func RemoteForwardedPort(ctx context.Context, s settingStore) (int, error) {
	v, err := s.Setting(ctx, SettingRemoteForwardedPort)
	if err != nil || v == "" {
		return 0, err
	}
	return strconv.Atoi(v)
}

// validForwardedPort reports whether the certificate check can come in
// through router port p: Let's Encrypt only checks on 443 and 80.
func validForwardedPort(p int) bool { return p == 0 || p == 443 || p == 80 }

func (s *Server) setRemoteForwardedPort(ctx context.Context, port int) error {
	if err := s.Store.SetSetting(ctx, SettingRemoteForwardedPort, strconv.Itoa(port)); err != nil {
		return err
	}
	if s.Remote != nil {
		s.Remote.SetForwardedPort(port)
	}
	return nil
}

// lookup is this server's id with the connect service and where the apps
// look it up, while remote access can use it; both "" otherwise.
func (s *Server) lookup() (id, url string) {
	if s.PublicURL != "" || s.Remote == nil || s.Remote.URL() == "" {
		return "", ""
	}
	return s.Remote.Lookup()
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
