package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/yeixio/matinee/internal/gamestream"
	"github.com/yeixio/matinee/internal/psremote"
)

// PlayStation Remote Play (preview): an admin pairs Matinee with a PS4 or
// PS5 on the home network once, then plays it from any player as a game,
// streamed like the others. The console's stream stays on the home network
// (internal/psremote).

func (s *Server) playStationRoutes() []route {
	return []route{
		{"GET /games/playstation", adminOnly, s.playStationInfo},
		{"PUT /games/playstation/account", adminOnly, s.setPlayStationAccount},
		{"DELETE /games/playstation/account", adminOnly, s.removePlayStationAccount},
		{"POST /games/playstation/consoles", adminOnly, s.pairPlayStation},
		{"DELETE /games/playstation/consoles/{id}", adminOnly, s.removePlayStation},
		{"POST /games/playstation/consoles/{id}/wake", adminOnly, s.wakePlayStation},
	}
}

// playStationKey is the setting the account and the paired consoles are
// kept in, with what pairing gave, which never leaves the server.
const playStationKey = "playstation"

type playStationSetup struct {
	// Account is the PSN account ID, in hex.
	Account  string             `json:"account,omitempty"`
	Consoles []psremote.Console `json:"consoles,omitempty"`
}

var playStationMu sync.Mutex // one change to the setup at a time

func (s *Server) loadPlayStation(ctx context.Context) (playStationSetup, error) {
	var ps playStationSetup
	v, err := s.Store.Setting(ctx, playStationKey)
	if err != nil || v == "" {
		return ps, err
	}
	return ps, json.Unmarshal([]byte(v), &ps)
}

func (s *Server) savePlayStation(ctx context.Context, ps playStationSetup) error {
	b, err := json.Marshal(ps)
	if err != nil {
		return err
	}
	return s.Store.SetSetting(ctx, playStationKey, string(b))
}

func (s *Server) playStationReady(w http.ResponseWriter) bool {
	if s.Games == nil || !s.PlayStation.Available() {
		writeError(w, http.StatusNotFound, "this server can't play PlayStation games")
		return false
	}
	return true
}

// consoleView is a console as admins see it: without its keys, and with
// whether it's on now.
type consoleView struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Host  string `json:"host"`
	PS5   bool   `json:"ps5"`
	State string `json:"state"` // ready, standby or off
	App   string `json:"app,omitempty"`
}

func (s *Server) playStationInfo(w http.ResponseWriter, r *http.Request) {
	if s.Games == nil || !s.PlayStation.Available() {
		writeJSON(w, http.StatusOK, map[string]any{"available": false, "consoles": []any{}})
		return
	}
	ps, err := s.loadPlayStation(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	views := make([]consoleView, len(ps.Consoles))
	var wg sync.WaitGroup
	for i, c := range ps.Consoles {
		views[i] = consoleView{ID: c.ID, Name: c.Name, Host: c.Host, PS5: c.PS5, State: "off"}
		wg.Go(func() {
			if st, err := s.PlayStation.Status(r.Context(), c.Host); err == nil {
				views[i].State, views[i].App = st.State, st.App
			}
		})
	}
	wg.Wait()
	writeJSON(w, http.StatusOK, map[string]any{"available": true, "signedIn": ps.Account != "",
		"signInUrl": psremote.SignInURL(), "consoles": views})
}

// setPlayStationAccount saves the PSN account ID pairing needs: from the
// address Sony's sign-in ended on, or pasted as it is.
func (s *Server) setPlayStationAccount(w http.ResponseWriter, r *http.Request) {
	if !s.playStationReady(w) {
		return
	}
	var body struct {
		Redirect  string `json:"redirect"`
		AccountID string `json:"accountId"`
	}
	if err := readJSON(r, &body); err != nil || (body.Redirect == "") == (body.AccountID == "") {
		writeError(w, http.StatusBadRequest, "bad request")
		return
	}
	var account string
	var err error
	if body.AccountID != "" {
		account, err = psremote.AccountID(body.AccountID)
	} else {
		var code string
		if code, err = psremote.CodeFromRedirect(body.Redirect); err == nil {
			account, err = psremote.SignIn(r.Context(), nil, code)
		}
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	playStationMu.Lock()
	defer playStationMu.Unlock()
	ps, err := s.loadPlayStation(r.Context())
	if err == nil {
		ps.Account = account
		err = s.savePlayStation(r.Context(), ps)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// removePlayStationAccount forgets the account, and the consoles paired
// with it.
func (s *Server) removePlayStationAccount(w http.ResponseWriter, r *http.Request) {
	playStationMu.Lock()
	defer playStationMu.Unlock()
	if err := s.savePlayStation(r.Context(), playStationSetup{}); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) pairPlayStation(w http.ResponseWriter, r *http.Request) {
	if !s.playStationReady(w) {
		return
	}
	var body struct {
		Host string `json:"host"`
		PS5  bool   `json:"ps5"`
		PIN  string `json:"pin"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "bad request")
		return
	}
	if err := psremote.CheckHost(body.Host); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ps, err := s.loadPlayStation(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if ps.Account == "" {
		writeError(w, http.StatusBadRequest, "sign in to PlayStation first")
		return
	}
	c, err := s.PlayStation.Pair(r.Context(), body.Host, body.PS5, ps.Account, body.PIN)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	id := make([]byte, 6)
	rand.Read(id)
	c.ID = hex.EncodeToString(id)

	playStationMu.Lock()
	defer playStationMu.Unlock()
	if ps, err = s.loadPlayStation(r.Context()); err == nil {
		// Pairing the same console again replaces it.
		kept := ps.Consoles[:0]
		for _, o := range ps.Consoles {
			if o.Host != c.Host && (o.MAC == "" || o.MAC != c.MAC) {
				kept = append(kept, o)
			}
		}
		ps.Consoles = append(kept, c)
		err = s.savePlayStation(r.Context(), ps)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, consoleView{ID: c.ID, Name: c.Name, Host: c.Host, PS5: c.PS5, State: "ready"})
}

func (s *Server) removePlayStation(w http.ResponseWriter, r *http.Request) {
	playStationMu.Lock()
	defer playStationMu.Unlock()
	ps, err := s.loadPlayStation(r.Context())
	if err == nil {
		kept := ps.Consoles[:0]
		for _, c := range ps.Consoles {
			if c.ID != r.PathValue("id") {
				kept = append(kept, c)
			}
		}
		ps.Consoles = kept
		err = s.savePlayStation(r.Context(), ps)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// findPlayStation is a paired console and the account it's played with.
func (s *Server) findPlayStation(ctx context.Context, id string) (psremote.Console, string, error) {
	ps, err := s.loadPlayStation(ctx)
	if err != nil {
		return psremote.Console{}, "", err
	}
	for _, c := range ps.Consoles {
		if c.ID == id {
			return c, ps.Account, nil
		}
	}
	return psremote.Console{}, "", errNoConsole
}

var errNoConsole = errors.New("no such PlayStation")

func (s *Server) wakePlayStation(w http.ResponseWriter, r *http.Request) {
	if !s.playStationReady(w) {
		return
	}
	c, _, err := s.findPlayStation(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if err := s.PlayStation.Wake(r.Context(), c); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// playStationGame is a paired console to play: woken first if it's
// resting.
func (s *Server) playStationGame(ctx context.Context, id string) (gamestream.StartRemote, error) {
	if s.Games == nil || !s.PlayStation.Available() {
		return nil, errors.New("this server can't play PlayStation games")
	}
	c, account, err := s.findPlayStation(ctx, id)
	if err != nil {
		return nil, err
	}
	bridge, log := s.PlayStation, s.Log
	return func(ctx context.Context) (gamestream.Remote, gamestream.Info, error) {
		if err := bridge.WakeAndWait(ctx, c); err != nil {
			return nil, gamestream.Info{}, err
		}
		// The console sends what it can, up to 1080p (720p from a PS4
		// that isn't a Pro).
		st, err := bridge.Play(ctx, c, account, 1080, 60, log)
		if err != nil {
			return nil, gamestream.Info{}, err
		}
		log.Info("playing a PlayStation", "console", c.Name, "size", st.Hello.Width, "fps", st.Hello.FPS)
		r, info := gamestream.ConsoleRemote(st)
		return r, info, nil
	}, nil
}

// consoleStartTimeout is how long starting a console may take: waking a
// PS5 from rest mode takes about 20 seconds, and Remote Play a few more.
const consoleStartTimeout = 100 * time.Second
