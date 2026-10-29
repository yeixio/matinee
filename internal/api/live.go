package api

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/yeixio/matinee/internal/auth"
	"github.com/yeixio/matinee/internal/live"
	"github.com/yeixio/matinee/internal/store"
)

// Remote control and the live feed (see internal/live).
//
// Every open web player and app is a client. It says hello with
// PUT /clients/{client} when it opens and whenever what it plays changes
// (every 10 seconds or so while playing), and asks for commands with
// GET /clients/{client}/commands, which waits until one comes. Grown-ups who
// signed in see their own clients and can send them commands; admins see and
// control every client, which is what Home Assistant does. Admins can follow
// everything that happens with GET /events.

// clientIDPattern is what a client's own id looks like: one it made up once
// and keeps, such as a UUID.
var clientIDPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{8,64}$`)

// eventKeepAlive is how often the feed sends a comment when nothing happens,
// so proxies don't close it.
const eventKeepAlive = 25 * time.Second

type clientReport struct {
	live.Hello
	Playing *struct {
		ItemID      int64    `json:"itemId"`
		PositionMs  int64    `json:"positionMs"`
		DurationMs  int64    `json:"durationMs"`
		Paused      bool     `json:"paused"`
		Buffering   bool     `json:"buffering"`
		Volume      *float64 `json:"volume"`
		Muted       bool     `json:"muted"`
		Subtitles   string   `json:"subtitles"`
		Transcoding bool     `json:"transcoding"`
		SleepAt     int64    `json:"sleepAt"`
		SleepAtEnd  bool     `json:"sleepAtEnd"`
	} `json:"playing"`
}

func clientID(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := r.PathValue("client")
	if !clientIDPattern.MatchString(id) {
		writeError(w, http.StatusBadRequest, "bad client id")
		return "", false
	}
	return id, true
}

// LiveItem is how the feed describes an item.
func LiveItem(it store.Item) live.Item {
	e := live.Item{ID: it.ID, LibraryID: it.LibraryID, Kind: it.Kind, Title: it.Title, ShowTitle: it.ShowTitle}
	if it.Year != nil {
		e.Year = *it.Year
	}
	if it.SeasonIndex != nil {
		e.Season = *it.SeasonIndex
	}
	if it.Kind == "episode" && it.IndexNumber != nil {
		e.Episode = *it.IndexNumber
	}
	return e
}

func liveUser(u store.User) live.User {
	return live.User{ID: u.ID, Name: u.Username, Guest: u.Guest}
}

// putClient is a client saying hello and what it plays.
func (s *Server) putClient(w http.ResponseWriter, r *http.Request) {
	id, ok := clientID(w, r)
	if !ok {
		return
	}
	var req clientReport
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	// The web player goes by its browser, as in the list of devices.
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		req.Name = auth.BrowserName(r.UserAgent())
	}
	u := currentUser(r)
	var playing *live.Playing
	if p := req.Playing; p != nil {
		it, err := s.Store.Item(r.Context(), u.ID, p.ItemID)
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "item not found")
			return
		}
		if err != nil {
			s.serverError(w, err)
			return
		}
		if p.DurationMs == 0 && it.DurationMs != nil {
			p.DurationMs = *it.DurationMs
		}
		playing = &live.Playing{Item: LiveItem(it), PositionMs: max(p.PositionMs, 0), DurationMs: p.DurationMs,
			Paused: p.Paused, Buffering: p.Buffering, Volume: p.Volume, Muted: p.Muted, Subtitles: p.Subtitles,
			Transcoding: p.Transcoding, SleepAt: p.SleepAt, SleepAtEnd: p.SleepAtEnd}
	}
	if err := s.live().Report(id, u.Account(), liveUser(u), req.Hello, playing, !s.fromHome(r)); err != nil {
		writeError(w, http.StatusConflict, "this client id belongs to someone else")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"pollSeconds": int(live.MaxWait.Seconds())})
}

// deleteClient is a client closing.
func (s *Server) deleteClient(w http.ResponseWriter, r *http.Request) {
	id, ok := clientID(w, r)
	if !ok {
		return
	}
	s.live().Leave(id, currentUser(r).Account())
	w.WriteHeader(http.StatusNoContent)
}

// clientCommands waits for commands for a client.
func (s *Server) clientCommands(w http.ResponseWriter, r *http.Request) {
	id, ok := clientID(w, r)
	if !ok {
		return
	}
	wait, _ := strconv.Atoi(r.URL.Query().Get("wait"))
	cmds, err := s.live().Poll(r.Context(), id, currentUser(r).Account(), time.Duration(max(wait, 0))*time.Second)
	if errors.Is(err, live.ErrNoClient) {
		writeError(w, http.StatusNotFound, "say hello first")
		return
	}
	if cmds == nil {
		cmds = []live.Command{}
	}
	// restarting: the server is shutting down, for an update or a restart.
	writeJSON(w, http.StatusOK, map[string]any{"commands": cmds, "restarting": s.live().Closing()})
}

// controls says which clients a person may see and control: an admin's are
// everyone's, anyone else's are their own account's.
func controls(u store.User) func(live.Client) bool {
	return func(p live.Client) bool {
		return u.IsAdmin || (!u.Guest && p.Account() == u.Account())
	}
}

func (s *Server) listClients(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.live().Clients(controls(currentUser(r))))
}

func (s *Server) getClient(w http.ResponseWriter, r *http.Request) {
	id, ok := clientID(w, r)
	if !ok {
		return
	}
	p, ok := s.live().Client(id, controls(currentUser(r)))
	if !ok {
		writeError(w, http.StatusNotFound, "no such client")
		return
	}
	writeJSON(w, http.StatusOK, p)
}

// sendCommand asks a client to do something, such as pause or play a film.
func (s *Server) sendCommand(w http.ResponseWriter, r *http.Request) {
	id, ok := clientID(w, r)
	if !ok {
		return
	}
	var c live.Command
	if err := readJSON(r, &c); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if msg := s.checkCommand(r, id, &c); msg != "" {
		code := http.StatusBadRequest
		if msg == "item not found" || msg == "no such client" {
			code = http.StatusNotFound
		}
		writeError(w, code, msg)
		return
	}
	c.From = currentUser(r).Username
	if name, err := s.Store.SessionDeviceName(r.Context(), auth.Token(r)); err == nil && name != "" {
		c.From = name
	}
	c, err := s.live().Send(id, controls(currentUser(r)), c)
	switch {
	case errors.Is(err, live.ErrNoClient):
		writeError(w, http.StatusNotFound, "no such client")
	case errors.Is(err, live.ErrOffline):
		writeError(w, http.StatusConflict, "that client is offline")
	case errors.Is(err, live.ErrUnsupported):
		writeError(w, http.StatusConflict, "that client can't do that")
	case errors.Is(err, live.ErrBusy):
		writeError(w, http.StatusServiceUnavailable, "that client isn't answering")
	case err != nil:
		s.serverError(w, err)
	default:
		writeJSON(w, http.StatusAccepted, c)
	}
}

// checkCommand checks a command's details, and fills in what a playItem
// command plays. It returns what's wrong, or "".
func (s *Server) checkCommand(r *http.Request, client string, c *live.Command) string {
	switch c.Type {
	case live.CommandSeek:
		if c.PositionMs == nil || *c.PositionMs < 0 {
			return "positionMs is required"
		}
	case live.CommandVolume:
		if c.Volume == nil || *c.Volume < 0 || *c.Volume > 1 {
			return "volume must be between 0 and 1"
		}
	case live.CommandMute:
		if c.Muted == nil {
			return "muted is required"
		}
	case live.CommandSleep:
		if c.Minutes == nil && !c.EndOfItem {
			return "minutes or endOfItem is required"
		}
		if c.Minutes != nil && (*c.Minutes < 0 || *c.Minutes > 24*60) {
			return "minutes must be between 0 and 1440"
		}
	case live.CommandSubtitles:
		if c.Track == nil {
			return "track is required"
		}
	case live.CommandPlayItem:
		if c.PositionMs != nil && *c.PositionMs < 0 {
			return "positionMs is required"
		}
		// It plays as whoever uses the client, so it has to be something
		// they may watch: a kids profile only gets what it could pick itself.
		p, ok := s.live().Client(client, controls(currentUser(r)))
		if !ok {
			return "no such client"
		}
		it, err := s.Store.Item(r.Context(), p.User.ID, c.ItemID)
		if err == nil && !it.Playable {
			it, err = s.playTarget(r.Context(), p.User.ID, it)
		}
		if err != nil {
			return "item not found"
		}
		c.ItemID = it.ID
		i := LiveItem(it)
		c.Item = &i
	case live.CommandPlay, live.CommandPause, live.CommandStop, live.CommandNext, live.CommandPrevious:
	default:
		return "unknown command"
	}
	return ""
}

// playTarget is what playing a show, season or album means: the episode
// the person is partway through, else the first they haven't watched, else
// the very first. Specials come last.
func (s *Server) playTarget(ctx context.Context, userID int64, it store.Item) (store.Item, error) {
	var playable []store.Item
	var walk func(store.Item, int) error
	walk = func(parent store.Item, depth int) error {
		kids, err := s.Store.Children(ctx, userID, parent.ID)
		if err != nil {
			return err
		}
		var specials []store.Item
		for _, k := range kids {
			if k.Kind == "season" && k.IndexNumber != nil && *k.IndexNumber == 0 {
				specials = append(specials, k)
				continue
			}
			if k.Playable {
				playable = append(playable, k)
			} else if depth < 2 {
				if err := walk(k, depth+1); err != nil {
					return err
				}
			}
		}
		for _, k := range specials {
			if err := walk(k, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(it, 0); err != nil {
		return it, err
	}
	if len(playable) == 0 {
		return it, store.ErrNotFound
	}
	for _, p := range playable {
		if p.PositionMs > 0 && !p.Played {
			return p, nil
		}
	}
	for _, p := range playable {
		if !p.Played {
			return p, nil
		}
	}
	return playable[0], nil
}

// events follows everything that happens, as server-sent events. It starts
// with a snapshot of every client, so a follower that reconnects knows
// where things stand.
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	rc := http.NewResponseController(w)
	events, cancel := s.live().Subscribe()
	defer cancel()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no") // nginx would hold events back
	w.WriteHeader(http.StatusOK)
	send := func(typ string, v any) bool {
		b, _ := json.Marshal(v)
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", typ, b); err != nil {
			return false
		}
		return rc.Flush() == nil
	}
	if !send("snapshot", map[string]any{"type": "snapshot", "time": time.Now().UnixMilli(), "clients": s.live().Clients(controls(currentUser(r)))}) {
		return
	}
	t := time.NewTicker(eventKeepAlive)
	defer t.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case e, ok := <-events:
			if !ok {
				return
			}
			if !send(e.Type, e) {
				return
			}
		case <-t.C:
			if _, err := fmt.Fprint(w, ": still here\n\n"); err != nil || rc.Flush() != nil {
				return
			}
		}
	}
}

const settingUUID = "server_uuid"

// ServerUUID is this server's own id, made up the first time it runs. It
// stays the same when the server is renamed or moves.
func ServerUUID(ctx context.Context, db *store.Store) (string, error) {
	id, err := db.Setting(ctx, settingUUID)
	if err != nil || id != "" {
		return id, err
	}
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	b[6], b[8] = b[6]&0x0f|0x40, b[8]&0x3f|0x80
	id = fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
	return id, db.SetSetting(ctx, settingUUID, id)
}

// live is the server's hub; tests and servers that didn't set one get
// their own.
func (s *Server) live() *live.Hub {
	s.liveOnce.Do(func() {
		if s.Live == nil {
			s.Live = live.New()
		}
	})
	return s.Live
}
