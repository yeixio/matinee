package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/yeixio/matinee/internal/gamestream"
	"github.com/yeixio/matinee/internal/xboxremote"
)

// Xbox remote play (preview): an admin signs Matinee in to their Xbox
// account once, then plays any of its consoles that have remote play on,
// from any player, as a game streamed like the others. Microsoft's
// servers start the session; the picture then comes straight from the
// console on the home network (internal/xboxremote).

func (s *Server) xboxRoutes() []route {
	return []route{
		{"GET /games/xbox", adminOnly, s.xboxInfo},
		{"POST /games/xbox/signin", adminOnly, s.startXboxSignIn},
		{"DELETE /games/xbox/account", adminOnly, s.removeXboxAccount},
	}
}

// xboxKey is the setting the sign-in is kept in. Its refresh token is
// what lets Matinee start remote play, so it never leaves the server.
const xboxKey = "xbox"

type xboxSetup struct {
	Refresh  string `json:"refresh,omitempty"`
	Gamertag string `json:"gamertag,omitempty"`
}

// xboxSignIn is a sign-in waiting for the admin on Microsoft's page.
type xboxSignIn struct {
	mu     sync.Mutex
	code   *xboxremote.DeviceCode
	err    string
	cancel context.CancelFunc
}

var xboxMu sync.Mutex // one change to the setup at a time

func (s *Server) loadXbox(ctx context.Context) (xboxSetup, error) {
	var x xboxSetup
	v, err := s.Store.Setting(ctx, xboxKey)
	if err != nil || v == "" {
		return x, err
	}
	return x, json.Unmarshal([]byte(v), &x)
}

func (s *Server) saveXbox(ctx context.Context, x xboxSetup) error {
	b, err := json.Marshal(x)
	if err != nil {
		return err
	}
	return s.Store.SetSetting(ctx, xboxKey, string(b))
}

// xboxTokens signs in with the kept sign-in, keeping the refresh token
// Microsoft hands back in its place.
func (s *Server) xboxTokens(ctx context.Context) (*xboxremote.Tokens, error) {
	x, err := s.loadXbox(ctx)
	if err != nil {
		return nil, err
	}
	if x.Refresh == "" {
		return nil, errors.New("sign in to Xbox first")
	}
	t, err := s.Xbox.Tokens(ctx, x.Refresh)
	if err != nil {
		return nil, err
	}
	if t.Refresh != x.Refresh || (t.Gamertag != "" && t.Gamertag != x.Gamertag) {
		xboxMu.Lock()
		if cur, err := s.loadXbox(ctx); err == nil && cur.Refresh == x.Refresh {
			cur.Refresh = t.Refresh
			if t.Gamertag != "" {
				cur.Gamertag = t.Gamertag
			}
			s.saveXbox(ctx, cur)
		}
		xboxMu.Unlock()
	}
	return t, nil
}

func (s *Server) xboxInfo(w http.ResponseWriter, r *http.Request) {
	if s.Games == nil || s.Xbox == nil {
		writeJSON(w, http.StatusOK, map[string]any{"available": false, "consoles": []any{}})
		return
	}
	res := map[string]any{"available": true, "signedIn": false, "consoles": []xboxremote.Console{}}
	s.xboxPending.mu.Lock()
	if s.xboxPending.code != nil {
		res["signIn"] = s.xboxPending.code
	}
	if s.xboxPending.err != "" {
		res["signInError"] = s.xboxPending.err
	}
	s.xboxPending.mu.Unlock()
	x, err := s.loadXbox(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if x.Refresh == "" {
		writeJSON(w, http.StatusOK, res)
		return
	}
	res["signedIn"], res["gamertag"] = true, x.Gamertag
	t, err := s.xboxTokens(r.Context())
	if err == nil {
		var consoles []xboxremote.Console
		if consoles, err = s.Xbox.Consoles(r.Context(), t); err == nil {
			res["consoles"] = consoles
		}
	}
	if err != nil {
		if errors.Is(err, xboxremote.ErrSignedOut) {
			res["signedIn"] = false
		}
		res["error"] = err.Error()
	}
	writeJSON(w, http.StatusOK, res)
}

// startXboxSignIn gets a code for the admin to type at microsoft.com/link,
// and waits for them in the background.
func (s *Server) startXboxSignIn(w http.ResponseWriter, r *http.Request) {
	if s.Games == nil || s.Xbox == nil {
		writeError(w, http.StatusNotFound, "this server can't play Xbox games")
		return
	}
	code, err := s.Xbox.StartSignIn(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	ctx, cancel := context.WithDeadline(context.Background(), code.Expires)
	p := &s.xboxPending
	p.mu.Lock()
	if p.cancel != nil {
		p.cancel()
	}
	p.code, p.err, p.cancel = code, "", cancel
	p.mu.Unlock()
	go func() {
		defer cancel()
		refresh, err := s.Xbox.WaitSignIn(ctx, code)
		if err == nil {
			xboxMu.Lock()
			err = s.saveXbox(ctx, xboxSetup{Refresh: refresh})
			xboxMu.Unlock()
			if err == nil {
				s.xboxTokens(ctx) // for the gamertag
			}
		}
		p.mu.Lock()
		defer p.mu.Unlock()
		if p.code != code {
			return // a newer sign-in took over
		}
		p.code, p.cancel = nil, nil
		if err != nil && !errors.Is(err, context.Canceled) {
			p.err = err.Error()
		}
	}()
	writeJSON(w, http.StatusOK, code)
}

// removeXboxAccount signs Matinee out of Xbox.
func (s *Server) removeXboxAccount(w http.ResponseWriter, r *http.Request) {
	s.xboxPending.mu.Lock()
	if s.xboxPending.cancel != nil {
		s.xboxPending.cancel()
	}
	s.xboxPending.code, s.xboxPending.err, s.xboxPending.cancel = nil, "", nil
	s.xboxPending.mu.Unlock()
	xboxMu.Lock()
	defer xboxMu.Unlock()
	if err := s.saveXbox(r.Context(), xboxSetup{}); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// xboxGame is a console of the account to play.
func (s *Server) xboxGame(ctx context.Context, id string) (gamestream.StartRemote, error) {
	if s.Games == nil || s.Xbox == nil {
		return nil, errors.New("this server can't play Xbox games")
	}
	t, err := s.xboxTokens(ctx)
	if err != nil {
		return nil, err
	}
	consoles, err := s.Xbox.Consoles(ctx, t)
	if err != nil {
		return nil, err
	}
	var c *xboxremote.Console
	for i := range consoles {
		if consoles[i].ID == id {
			c = &consoles[i]
		}
	}
	if c == nil {
		return nil, errors.New("no such Xbox")
	}
	client, log, name := s.Xbox, s.Log, c.Name
	return func(ctx context.Context) (gamestream.Remote, gamestream.Info, error) {
		st, err := client.Play(ctx, t, id, log)
		if err != nil {
			return nil, gamestream.Info{}, err
		}
		log.Info("playing an Xbox", "console", name)
		r, info := gamestream.BridgeRemote(st, st.Hello)
		return r, info, nil
	}, nil
}

// xboxStartTimeout is how long starting an Xbox may take: waking it from
// Sleep and starting remote play takes it up to a minute.
const xboxStartTimeout = 100 * time.Second
