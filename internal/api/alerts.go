package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"net/mail"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/yeixio/matinee/internal/alerts"
)

// Admin alerts (Settings → Server health → Alerts): the server emails an
// admin, or posts to their webhooks, when a disk is nearly full, a library
// folder can't be read, errors pile up, transcodes keep failing, or the
// server starts again (see internal/alerts). Everything goes straight from
// this server to the admin's own mail server and webhooks.

func (s *Server) alerts() *alerts.Manager {
	s.alertsOnce.Do(func() {
		if s.Alerts == nil {
			s.Alerts = &alerts.Manager{Store: s.Store, Log: s.Log, Server: s.Name, URL: s.PublicURL, Read: s.AlertReadings}
		}
	})
	return s.Alerts
}

// AlertReadings is the server now, for the alert rules.
func (s *Server) AlertReadings(ctx context.Context) alerts.Readings {
	var r alerts.Readings
	if m := s.health(); m.Logs != nil {
		_, r.Errors = m.Logs.Counts()
		r.Problems = m.Logs.Problems(50)
	}
	if s.Transcoder != nil {
		_, r.TranscodeFailures = s.Transcoder.Counts()
	}
	dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	libs, err := s.Store.Libraries(dbCtx)
	if err != nil {
		// Without the libraries, only the server's own folders are checked.
		libs = nil
	}
	for _, d := range s.disks(s.storageUses(libs)) {
		disk := alerts.Disk{Total: d.Total, Free: d.Free, Reachable: d.Reachable}
		var names []string
		for _, u := range d.Uses {
			name := u.Use
			if u.Use == "library" {
				name, disk.Library = u.Library, true
			}
			if !slices.Contains(names, name) {
				names = append(names, name)
			}
		}
		disk.Name, disk.Path = strings.Join(names, ", "), d.Uses[0].Path
		r.Disks = append(r.Disks, disk)
	}
	return r
}

// alertsView is GET /admin/alerts. Passwords, tokens and webhook addresses
// (which work as passwords) never leave the server.
type alertsView struct {
	Email    alertsEmailView     `json:"email"`
	Webhooks []alertsWebhookView `json:"webhooks"`
	Rules    alerts.Rules        `json:"rules"`
	Firing   []alerts.Alert      `json:"firing"`
	History  []alerts.Alert      `json:"history"`
	// Links says alerts link to Server health (the server knows its address).
	Links bool `json:"links"`
}

type alertsEmailView struct {
	alerts.Email
	HasPassword bool `json:"hasPassword"`
}

type alertsWebhookView struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	Name string `json:"name,omitempty"`
	// Hint shows the address's host and its last few characters.
	Hint     string `json:"hint"`
	HasToken bool   `json:"hasToken,omitempty"`
}

func webhookHint(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	tail := strings.TrimRight(u.Path, "/")
	if len(tail) > 4 {
		tail = tail[len(tail)-4:]
	}
	return u.Host + "/…" + tail
}

func (s *Server) alertsView(ctx context.Context) (alertsView, error) {
	m := s.alerts()
	c, err := m.Config(ctx)
	if err != nil {
		return alertsView{}, err
	}
	v := alertsView{Rules: c.Rules, Webhooks: []alertsWebhookView{}, Firing: m.Firing(), History: m.History(ctx), Links: m.URL != ""}
	v.Email.Email, v.Email.HasPassword = c.Email, c.Email.Password != ""
	v.Email.Password = ""
	for _, h := range c.Webhooks {
		v.Webhooks = append(v.Webhooks, alertsWebhookView{ID: h.ID, Kind: h.Kind, Name: h.Name, Hint: webhookHint(h.URL), HasToken: h.Token != ""})
	}
	return v, nil
}

func (s *Server) getAlerts(w http.ResponseWriter, r *http.Request) {
	v, err := s.alertsView(r.Context())
	if err != nil {
		s.serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// alertsInput is PUT /admin/alerts. A webhook that keeps its id keeps its
// address and token when they're left empty, and the email keeps its
// password while the server and username stay the same.
type alertsInput struct {
	alerts.Config
}

func (in alertsInput) validate() string {
	e := in.Email
	if e.On {
		if e.Host == "" {
			return "enter the mail server"
		}
		if e.Port <= 0 || e.Port > 65535 {
			return "enter the mail server's port"
		}
		switch e.Security {
		case "starttls", "tls", "none":
		default:
			return "pick how to connect to the mail server"
		}
		if _, err := mail.ParseAddress(e.From); err != nil {
			return "enter the address alerts come from"
		}
		if _, err := mail.ParseAddressList(e.To); err != nil || e.To == "" {
			return "enter the addresses alerts go to, separated by commas"
		}
	}
	if len(in.Webhooks) > 20 {
		return "that's too many webhooks"
	}
	for _, h := range in.Webhooks {
		switch h.Kind {
		case alerts.KindDiscord, alerts.KindNtfy, alerts.KindHomeAssistant, alerts.KindJSON:
		default:
			return "unknown kind of webhook"
		}
		if alerts.CheckWebhook(h) != nil {
			return "enter the webhook's full address, starting with https:// or http://"
		}
	}
	return ""
}

func (s *Server) putAlerts(w http.ResponseWriter, r *http.Request) {
	var in alertsInput
	if err := readJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	ctx := r.Context()
	m := s.alerts()
	old, err := m.Config(ctx)
	if err != nil {
		s.serverError(w, err)
		return
	}
	c := s.mergeAlerts(old, in.Config)
	if msg := (alertsInput{c}).validate(); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	if err := m.SaveConfig(ctx, c); err != nil {
		s.serverError(w, err)
		return
	}
	s.getAlerts(w, r)
}

// mergeAlerts fills in what the admin left out to keep: secrets, and the
// ids of new webhooks.
func (s *Server) mergeAlerts(old, c alerts.Config) alerts.Config {
	e := &c.Email
	e.Host, e.Username, e.From, e.To = strings.TrimSpace(e.Host), strings.TrimSpace(e.Username), strings.TrimSpace(e.From), strings.TrimSpace(e.To)
	if e.Security == "" {
		e.Security = "starttls"
	}
	if e.Password == "" && e.Host == old.Email.Host && e.Username == old.Email.Username {
		e.Password = old.Email.Password
	}
	byID := map[string]alerts.Webhook{}
	for _, h := range old.Webhooks {
		byID[h.ID] = h
	}
	for i := range c.Webhooks {
		h := &c.Webhooks[i]
		h.URL, h.Name, h.Token = strings.TrimSpace(h.URL), strings.TrimSpace(h.Name), strings.TrimSpace(h.Token)
		if was, ok := byID[h.ID]; ok && h.ID != "" {
			if h.URL == "" {
				h.URL = was.URL
			}
			if h.Token == "" && h.URL == was.URL {
				h.Token = was.Token
			}
			continue
		}
		b := make([]byte, 6)
		rand.Read(b)
		h.ID = hex.EncodeToString(b)
	}
	if c.Webhooks == nil {
		c.Webhooks = []alerts.Webhook{}
	}
	return c
}

// testAlerts sends a test alert everywhere alerts go: to the settings in
// the request, if any (so an admin can try them before saving), or else the
// saved ones.
func (s *Server) testAlerts(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	m := s.alerts()
	c, err := m.Config(ctx)
	if err != nil {
		s.serverError(w, err)
		return
	}
	if r.ContentLength != 0 {
		var in alertsInput
		if err := readJSON(r, &in); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request")
			return
		}
		c = s.mergeAlerts(c, in.Config)
		if msg := (alertsInput{c}).validate(); msg != "" {
			writeError(w, http.StatusBadRequest, msg)
			return
		}
	}
	if c.Channels() == 0 {
		writeError(w, http.StatusConflict, "set up email or a webhook first")
		return
	}
	writeJSON(w, http.StatusOK, m.Test(ctx, c))
}
