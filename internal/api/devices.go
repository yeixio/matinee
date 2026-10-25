package api

import (
	_ "embed"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/yeixio/matinee/internal/auth"
	"github.com/yeixio/matinee/internal/store"
	"github.com/yeixio/matinee/internal/version"
)

// deviceCodeTTL is how long a code shown on a device stays usable.
const deviceCodeTTL = 10 * time.Minute

// pollInterval is how often, in seconds, a device should ask whether its code was approved.
const pollInterval = 3

//go:embed openapi.yaml
var openAPI []byte

func serveOpenAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/yaml")
	w.Write(openAPI)
}

// serverInfo is the first thing an app asks a server it was pointed at.
func (s *Server) serverInfo(w http.ResponseWriter, r *http.Request) {
	n, err := s.Store.CountUsers(r.Context())
	if err != nil {
		s.serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"product":    "matinee",
		"version":    version.Version,
		"apiVersion": APIVersion,
		"needsSetup": n == 0,
	})
}

// baseURL is the address the request reached this server at, as the person sees it.
func baseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	// Behind an ingress the original scheme arrives in X-Forwarded-Proto.
	if p := r.Header.Get("X-Forwarded-Proto"); p == "https" || p == "http" {
		scheme = p
	}
	return scheme + "://" + r.Host
}

func (s *Server) startDeviceCode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DeviceName string `json:"deviceName"`
		Platform   string `json:"platform"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if strings.TrimSpace(req.DeviceName) == "" {
		writeError(w, http.StatusBadRequest, "deviceName is required")
		return
	}
	dc, err := s.Store.StartDeviceCode(r.Context(), requestDevice(r, req.DeviceName, req.Platform), deviceCodeTTL)
	if err != nil {
		s.serverError(w, err)
		return
	}
	link := baseURL(r) + "/link"
	writeJSON(w, http.StatusOK, map[string]any{
		"deviceCode":              dc.DeviceCode,
		"userCode":                store.FormatUserCode(dc.UserCode),
		"verificationUrl":         link,
		"verificationUrlComplete": link + "?code=" + dc.UserCode,
		"expiresIn":               int(deviceCodeTTL.Seconds()),
		"interval":                pollInterval,
	})
}

func (s *Server) pollDeviceCode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DeviceCode string `json:"deviceCode"`
	}
	if err := readJSON(r, &req); err != nil || req.DeviceCode == "" {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	userID, dev, err := s.Store.RedeemDeviceCode(r.Context(), req.DeviceCode)
	switch {
	case errors.Is(err, store.ErrCodePending):
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "pending"})
		return
	case errors.Is(err, store.ErrCodeDenied):
		writeError(w, http.StatusForbidden, "sign-in was declined")
		return
	case errors.Is(err, store.ErrCodeExpired):
		writeError(w, http.StatusGone, "this code expired; ask for a new one")
		return
	case err != nil:
		s.serverError(w, err)
		return
	}
	token, err := s.Store.CreateSession(r.Context(), userID, auth.SessionTTL, dev)
	if err != nil {
		s.serverError(w, err)
		return
	}
	u, err := s.Store.SessionUser(r.Context(), token)
	if err != nil {
		s.serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "approved", "user": u, "token": token})
}

func (s *Server) getDeviceCode(w http.ResponseWriter, r *http.Request) {
	dc, err := s.Store.PendingDeviceCode(r.Context(), r.PathValue("code"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "that code isn't valid or has expired")
		return
	}
	if err != nil {
		s.serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"userCode":   store.FormatUserCode(dc.UserCode),
		"deviceName": dc.Device.Name,
		"platform":   dc.Device.Platform,
		"expiresAt":  dc.ExpiresAt.Unix(),
	})
}

func (s *Server) approveDeviceCode(w http.ResponseWriter, r *http.Request) {
	s.decideDeviceCode(w, r, true)
}
func (s *Server) denyDeviceCode(w http.ResponseWriter, r *http.Request) {
	s.decideDeviceCode(w, r, false)
}

func (s *Server) decideDeviceCode(w http.ResponseWriter, r *http.Request, approve bool) {
	err := s.Store.DecideDeviceCode(r.Context(), r.PathValue("code"), currentUser(r).ID, approve)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "that code isn't valid or has expired")
		return
	}
	if err != nil {
		s.serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listDevices(w http.ResponseWriter, r *http.Request) {
	sessions, err := s.Store.Sessions(r.Context(), currentUser(r).ID, auth.Token(r))
	if err != nil {
		s.serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sessions)
}

func (s *Server) deleteDevice(w http.ResponseWriter, r *http.Request) {
	err := s.Store.DeleteSessionByID(r.Context(), currentUser(r).ID, r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "no such device")
		return
	}
	if err != nil {
		s.serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
