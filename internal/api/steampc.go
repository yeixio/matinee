package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/yeixio/matinee/internal/gamestream"
	"github.com/yeixio/matinee/internal/pcremote"
)

// Steam on a PC you already have (preview): the PC runs Sunshine, an
// admin pairs Matinee with it once by typing the PIN Matinee shows into
// Sunshine's page, and the Steam button then plays Steam's Big Picture on
// that PC, streamed like any game (internal/pcremote). The other way to
// play Steam is a game host of the server's own, such as a Steam container
// (MATINEE_GAME_HOST); when both are set up, the paired PC is played.

func (s *Server) steamPCRoutes() []route {
	return []route{
		{"GET /games/steampc", adminOnly, s.steamPCInfo},
		{"POST /games/steampc/find", adminOnly, s.findSteamPC},
		{"POST /games/steampc/pair", adminOnly, s.pairSteamPC},
		{"DELETE /games/steampc/pair", adminOnly, s.cancelSteamPCPairing},
		{"DELETE /games/steampc", adminOnly, s.removeSteamPC},
	}
}

// steamPCKey is the setting Matinee's certificate and the paired PC are
// kept in; they never leave the server.
const steamPCKey = "steampc"

type steamPCSetup struct {
	Identity pcremote.Identity `json:"identity"`
	PC       *pcremote.PC      `json:"pc,omitempty"`
}

var steamPCMu sync.Mutex // one change to the setup at a time

func (s *Server) loadSteamPC(ctx context.Context) (steamPCSetup, error) {
	var sp steamPCSetup
	v, err := s.Store.Setting(ctx, steamPCKey)
	if err != nil || v == "" {
		return sp, err
	}
	return sp, json.Unmarshal([]byte(v), &sp)
}

func (s *Server) saveSteamPC(ctx context.Context, sp steamPCSetup) error {
	b, err := json.Marshal(sp)
	if err != nil {
		return err
	}
	return s.Store.SetSetting(ctx, steamPCKey, string(b))
}

func (s *Server) steamPCReady(w http.ResponseWriter) bool {
	if s.Games == nil || !s.SteamPC.Available() {
		writeError(w, http.StatusNotFound, "this server can't play games on a PC")
		return false
	}
	return true
}

// pairedSteamPC is the paired PC and the client to reach it with, or
// nil when none is paired.
func (s *Server) pairedSteamPC(ctx context.Context) (*pcremote.Client, *pcremote.PC) {
	if s.Games == nil || !s.SteamPC.Available() {
		return nil, nil
	}
	sp, err := s.loadSteamPC(ctx)
	if err != nil || sp.PC == nil {
		return nil, nil
	}
	return &pcremote.Client{Identity: sp.Identity}, sp.PC
}

// steamPCPairing is a pairing waiting for its PIN to be typed into
// Sunshine, which can take the admin a minute.
type steamPCPairing struct {
	PIN    string `json:"pin"`
	Name   string `json:"name"`
	Host   string `json:"host"`
	State  string `json:"state"` // waiting, paired or failed
	Error  string `json:"error,omitempty"`
	cancel context.CancelFunc
}

var (
	pairingMu sync.Mutex
	pairing   *steamPCPairing
)

// steamPCPairWait is how long pairing waits for the PIN.
const steamPCPairWait = 3 * time.Minute

func (s *Server) steamPCInfo(w http.ResponseWriter, r *http.Request) {
	res := map[string]any{"available": s.Games != nil && s.SteamPC.Available(), "container": s.GameHost != nil}
	pairingMu.Lock()
	if pairing != nil {
		p := *pairing
		res["pairing"] = p
	}
	pairingMu.Unlock()
	c, pc := s.pairedSteamPC(r.Context())
	if pc != nil {
		view := map[string]any{"name": pc.Name, "host": pc.Host, "port": pc.Port, "state": "ready"}
		ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
		info, err := c.Info(ctx, *pc)
		cancel()
		switch {
		case errors.Is(err, pcremote.ErrNotPaired):
			view["state"], view["error"] = "unpaired", err.Error()
		case err != nil:
			view["state"], view["error"] = "off", err.Error()
		case info.Busy():
			view["state"] = "busy"
		}
		if err == nil && info.Name != "" {
			view["name"] = info.Name
		}
		res["pc"] = view
	}
	switch {
	case pc != nil:
		res["active"] = "pc"
	case s.GameHost != nil:
		res["active"] = "container"
	default:
		res["active"] = ""
	}
	writeJSON(w, http.StatusOK, res)
}

// findSteamPC checks that Sunshine answers at an address, before pairing.
func (s *Server) findSteamPC(w http.ResponseWriter, r *http.Request) {
	if !s.steamPCReady(w) {
		return
	}
	var body struct {
		Address string `json:"address"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "bad request")
		return
	}
	host, port, err := pcremote.SplitAddress(body.Address)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	info, err := (&pcremote.Client{Timeout: 5 * time.Second}).Find(r.Context(), host, port)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"name": info.Name, "version": info.AppVersion, "host": host, "port": port})
}

// pairSteamPC starts pairing with the PC at an address and answers with
// the PIN to type into Sunshine; GET /games/steampc says how it went.
func (s *Server) pairSteamPC(w http.ResponseWriter, r *http.Request) {
	if !s.steamPCReady(w) {
		return
	}
	var body struct {
		Address string `json:"address"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "bad request")
		return
	}
	host, port, err := pcremote.SplitAddress(body.Address)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	info, err := (&pcremote.Client{Timeout: 5 * time.Second}).Find(r.Context(), host, port)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	steamPCMu.Lock()
	sp, err := s.loadSteamPC(r.Context())
	if err == nil && sp.Identity.Cert == "" {
		if sp.Identity, err = pcremote.NewIdentity(); err == nil {
			err = s.saveSteamPC(r.Context(), sp)
		}
	}
	steamPCMu.Unlock()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), steamPCPairWait+30*time.Second)
	p := &steamPCPairing{PIN: pcremote.NewPIN(), Name: info.Name, Host: host, State: "waiting", cancel: cancel}
	pairingMu.Lock()
	if pairing != nil && pairing.cancel != nil {
		pairing.cancel()
	}
	pairing = p
	pairingMu.Unlock()

	log := s.Log
	go func() {
		defer cancel()
		c := &pcremote.Client{Identity: sp.Identity}
		pc, err := c.Pair(ctx, host, port, p.PIN, steamPCPairWait)
		if err == nil {
			steamPCMu.Lock()
			var cur steamPCSetup
			if cur, err = s.loadSteamPC(context.Background()); err == nil {
				cur.PC = &pc
				err = s.saveSteamPC(context.Background(), cur)
			}
			steamPCMu.Unlock()
		}
		pairingMu.Lock()
		defer pairingMu.Unlock()
		if pairing != p {
			return
		}
		if err != nil {
			if ctx.Err() == context.Canceled {
				pairing = nil
				return
			}
			p.State, p.Error = "failed", err.Error()
			log.Info("couldn't pair with a Steam PC", "host", host, "err", err)
			return
		}
		p.State, p.Name = "paired", pc.Name
		log.Info("paired with a Steam PC", "name", pc.Name, "host", host)
	}()
	writeJSON(w, http.StatusOK, map[string]any{"pin": p.PIN, "name": info.Name})
}

func (s *Server) cancelSteamPCPairing(w http.ResponseWriter, r *http.Request) {
	pairingMu.Lock()
	if pairing != nil {
		if pairing.cancel != nil {
			pairing.cancel()
		}
		pairing = nil
	}
	pairingMu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

// removeSteamPC has the PC forget Matinee, if it answers, and forgets the
// PC.
func (s *Server) removeSteamPC(w http.ResponseWriter, r *http.Request) {
	steamPCMu.Lock()
	defer steamPCMu.Unlock()
	sp, err := s.loadSteamPC(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if sp.PC != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		(&pcremote.Client{Identity: sp.Identity}).Unpair(ctx, *sp.PC)
		cancel()
	}
	sp.PC = nil
	if err := s.saveSteamPC(r.Context(), sp); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	pairingMu.Lock()
	pairing = nil
	pairingMu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

// steamPCGame is Steam's Big Picture on the paired PC, or nil when no PC
// is paired.
func (s *Server) steamPCGame(ctx context.Context) gamestream.StartRemote {
	c, pc := s.pairedSteamPC(ctx)
	if pc == nil {
		return nil
	}
	bridge, log := s.SteamPC, s.Log
	return func(ctx context.Context) (gamestream.Remote, gamestream.Info, error) {
		apps, err := c.Apps(ctx, *pc)
		if err != nil {
			return nil, gamestream.Info{}, err
		}
		app, ok := pcremote.SteamApp(apps)
		if !ok {
			return nil, gamestream.Info{}, errors.New("Sunshine on the PC has no Steam Big Picture app: add one in Sunshine's Applications page")
		}
		st, err := bridge.Play(ctx, c, *pc, app.ID, pcremote.DefaultSettings, log)
		if err != nil {
			return nil, gamestream.Info{}, err
		}
		log.Info("playing on a Steam PC", "pc", pc.Name, "app", app.Title, "size", st.Hello.Width, "fps", st.Hello.FPS)
		r, info := gamestream.BridgeRemote(st, st.Hello)
		return r, info, nil
	}
}
