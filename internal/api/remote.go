package api

import (
	"context"
	"errors"
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
// look it up, while the service knows its address; both "" otherwise.
func (s *Server) lookup() (id, url string) {
	if s.PublicURL != "" || s.Remote == nil || !s.Remote.Findable() {
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

// Matinee Connect, the paid service that lets the apps find this server
// after its address changes, is bought here (internal/remote/subscription.go).

func (s *Server) connectPlans(w http.ResponseWriter, r *http.Request) {
	if s.Remote == nil {
		writeError(w, http.StatusNotFound, "this server doesn't offer remote access")
		return
	}
	plans, err := s.Remote.Plans(r.Context())
	if err != nil {
		s.connectError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"plans": plans})
}

func (s *Server) connectSubscribe(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Plan string `json:"plan"`
	}
	if err := readJSON(r, &in); err != nil || (in.Plan != "monthly" && in.Plan != "yearly") {
		writeError(w, http.StatusBadRequest, "plan must be monthly or yearly")
		return
	}
	if s.Remote == nil {
		writeError(w, http.StatusNotFound, "this server doesn't offer remote access")
		return
	}
	url, err := s.Remote.Subscribe(r.Context(), in.Plan)
	if err != nil {
		s.connectError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"url": url})
}

func (s *Server) connectManage(w http.ResponseWriter, r *http.Request) {
	if s.Remote == nil {
		writeError(w, http.StatusNotFound, "this server doesn't offer remote access")
		return
	}
	url, err := s.Remote.ManageURL(r.Context())
	if err != nil {
		s.connectError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"url": url})
}

func (s *Server) connectError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, remote.ErrNoBilling):
		writeError(w, http.StatusNotFound, "this server doesn't use Matinee Connect")
	case errors.Is(err, remote.ErrNoSubscriber):
		writeError(w, http.StatusNotFound, "this server has no Matinee Connect subscription bought in its settings")
	default:
		s.Log.Warn("Matinee Connect billing", "err", err)
		writeError(w, http.StatusBadGateway, "billing couldn't be reached; try again in a minute")
	}
}
