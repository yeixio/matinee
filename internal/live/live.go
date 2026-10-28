// Package live keeps track of what's happening on the server right now: which
// players are open, what each one is playing, and a feed of events for home
// automation (see the Home Assistant guide). Players that can take commands
// get them from here too, so another device can pause or start something on
// them.
//
// Players say hello and report what they play with Report, and ask for
// commands with Poll, which waits until one comes or the wait runs out. A
// player that stops asking goes offline.
package live

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"sync"
	"time"
)

const (
	// offlineAfter is how long a player may go without reporting or asking
	// for commands before it counts as gone. Players ask every MaxWait
	// seconds or sooner.
	offlineAfter = 45 * time.Second
	// stalledAfter is how long something may play without a report before
	// it counts as stopped, for players that crash mid-film.
	stalledAfter = 60 * time.Second
	// forgetAfter is how long an offline player stays in the list.
	forgetAfter = time.Hour
	// MaxWait is the longest a player's request for commands is held open.
	MaxWait = 30 * time.Second
	// queueSize is how many commands wait for a player that's slow to ask.
	queueSize = 8
)

// Things a player can be asked to do. Players list the ones they can in
// Hello.Commands.
const (
	CommandPlay      = "play"      // carry on playing
	CommandPause     = "pause"     // pause
	CommandStop      = "stop"      // stop and close the player
	CommandSeek      = "seek"      // jump to PositionMs
	CommandNext      = "next"      // the next episode or track
	CommandPrevious  = "previous"  // the previous one, or back to the start
	CommandPlayItem  = "playItem"  // open ItemID and play it
	CommandVolume    = "volume"    // set the volume to Volume, 0 to 1
	CommandMute      = "mute"      // mute or unmute (Muted)
	CommandSleep     = "sleep"     // stop after Minutes, or at the end (EndOfItem); 0 cancels
	CommandSubtitles = "subtitles" // show subtitle Track, or none for ""
)

// Commands are every command there is.
var Commands = []string{CommandPlay, CommandPause, CommandStop, CommandSeek, CommandNext, CommandPrevious,
	CommandPlayItem, CommandVolume, CommandMute, CommandSleep, CommandSubtitles}

// Event types in the feed.
const (
	EventClientUpdated   = "client.updated"   // a player appeared, changed, or went offline
	EventClientRemoved   = "client.removed"   // an offline player was forgotten
	EventPlaybackStarted = "playback.started" // a player started something
	EventPlaybackPaused  = "playback.paused"
	EventPlaybackResumed = "playback.resumed"
	EventPlaybackStopped = "playback.stopped" // Finished says whether it reached the end
	EventItemAdded       = "item.added"       // a scan found something new
	EventLibraryScanned  = "library.scanned"  // a library finished scanning
)

var (
	ErrNoClient    = errors.New("no such player")
	ErrOffline     = errors.New("player is offline")
	ErrUnsupported = errors.New("player can't do that")
	ErrBusy        = errors.New("player isn't keeping up")
)

// Item is what's playing, or what an event is about.
type Item struct {
	ID        int64  `json:"id"`
	LibraryID int64  `json:"libraryId"`
	Kind      string `json:"kind"`
	Title     string `json:"title"`
	Year      int    `json:"year,omitempty"`
	ShowTitle string `json:"showTitle,omitempty"`
	Season    int    `json:"season,omitempty"`
	Episode   int    `json:"episode,omitempty"`
}

// User is whose player it is: an account or a profile.
type User struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Guest bool   `json:"guest,omitempty"` // the home network's guest account or one of its profiles
}

// Library is a library an event is about.
type Library struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"`
}

// Hello is what a player says about itself every time it reports.
type Hello struct {
	Name     string   `json:"name"`     // what people call the device, such as "Living room TV"
	Platform string   `json:"platform"` // web, ios, android, tvos, androidtv
	App      string   `json:"app,omitempty"`
	Version  string   `json:"version,omitempty"`
	Commands []string `json:"commands"` // the commands it takes; none means it can't be controlled
}

// Playing is what a player has open. PositionMs was true at UpdatedAt and
// moves on from there unless Paused.
type Playing struct {
	Item       Item     `json:"item"`
	PositionMs int64    `json:"positionMs"`
	DurationMs int64    `json:"durationMs,omitempty"`
	Paused     bool     `json:"paused"`
	Buffering  bool     `json:"buffering,omitempty"`
	Volume     *float64 `json:"volume,omitempty"` // 0 to 1, when the player has its own volume
	Muted      bool     `json:"muted,omitempty"`
	// Subtitles is the subtitle track showing, empty for none.
	Subtitles string `json:"subtitles,omitempty"`
	// Transcoding says the server converts the stream; false is direct play.
	Transcoding bool `json:"transcoding,omitempty"`
	// SleepAt is when the sleep timer stops playback (milliseconds), or 0.
	SleepAt int64 `json:"sleepAt,omitempty"`
	// SleepAtEnd means the sleep timer stops at the end of this item.
	SleepAtEnd bool  `json:"sleepAtEnd,omitempty"`
	UpdatedAt  int64 `json:"updatedAt"` // milliseconds
}

// Client is an open web player or app.
type Client struct {
	ID string `json:"id"`
	Hello
	User     User     `json:"user"`
	Online   bool     `json:"online"`
	LastSeen int64    `json:"lastSeen"` // milliseconds
	Playing  *Playing `json:"playing,omitempty"`
	// Remote says the player is away from home, so it has no address on the
	// home network.
	Remote bool `json:"remote,omitempty"`

	account int64 // who may report and ask for its commands
	queue   chan Command
	since   time.Time // when Playing last changed state, for events
}

// Command is something a player is asked to do.
type Command struct {
	ID         string   `json:"id"`
	Type       string   `json:"type"`
	PositionMs *int64   `json:"positionMs,omitempty"` // seek; where to start, for playItem
	ItemID     int64    `json:"itemId,omitempty"`     // playItem
	Item       *Item    `json:"item,omitempty"`       // playItem: what ItemID is, for the player to show
	Volume     *float64 `json:"volume,omitempty"`
	Muted      *bool    `json:"muted,omitempty"`
	Minutes    *int     `json:"minutes,omitempty"`
	EndOfItem  bool     `json:"endOfItem,omitempty"`
	Track      *string  `json:"track,omitempty"`
	// From names who sent it, such as "Home Assistant", so a player can say so.
	From string `json:"from,omitempty"`
}

// Event is one entry in the feed.
type Event struct {
	Type     string   `json:"type"`
	Time     int64    `json:"time"` // milliseconds
	Client   *Client  `json:"client,omitempty"`
	ClientID string   `json:"clientId,omitempty"` // client.removed
	Item     *Item    `json:"item,omitempty"`
	User     *User    `json:"user,omitempty"`
	Library  *Library `json:"library,omitempty"`
	// PositionMs is where playback was, for playback events.
	PositionMs int64 `json:"positionMs,omitempty"`
	Finished   bool  `json:"finished,omitempty"`
}

// Hub is the server's live state. The zero value is not usable; call New.
type Hub struct {
	mu      sync.Mutex
	clients map[string]*Client
	subs    map[chan Event]struct{}
	now     func() time.Time
	seq     int
}

// New makes a hub. Run it so players go offline when they leave.
func New() *Hub {
	return &Hub{clients: map[string]*Client{}, subs: map[chan Event]struct{}{}, now: time.Now}
}

// Run marks quiet players offline, until ctx ends.
func (h *Hub) Run(ctx context.Context) {
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			h.sweep()
		}
	}
}

func (h *Hub) sweep() {
	h.mu.Lock()
	defer h.mu.Unlock()
	now := h.now()
	for id, p := range h.clients {
		seen := time.UnixMilli(p.LastSeen)
		switch {
		case !p.Online && now.Sub(seen) > forgetAfter:
			delete(h.clients, id)
			h.publish(Event{Type: EventClientRemoved, ClientID: id})
		case p.Online && now.Sub(seen) > offlineAfter:
			p.Online = false
			h.stopped(p, false)
			h.publish(Event{Type: EventClientUpdated, Client: p.copy()})
		case p.Playing != nil && now.Sub(time.UnixMilli(p.Playing.UpdatedAt)) > stalledAfter:
			h.stopped(p, false)
			h.publish(Event{Type: EventClientUpdated, Client: p.copy()})
		}
	}
}

// Report is a player saying hello and what it's playing (nil for nothing).
// account is the signed-in account (or the home guest) the player uses;
// another account can't report for the same player.
func (h *Hub) Report(id string, account int64, user User, hello Hello, playing *Playing, remote bool) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	now := h.now()
	p, ok := h.clients[id]
	if ok && p.account != account {
		return ErrNoClient
	}
	if !ok {
		p = &Client{ID: id, account: account, queue: make(chan Command, queueSize)}
		h.clients[id] = p
	}
	hello.Commands = slices.DeleteFunc(slices.Clone(hello.Commands), func(c string) bool { return !slices.Contains(Commands, c) })
	p.Hello, p.User, p.Online, p.LastSeen, p.Remote = hello, user, true, now.UnixMilli(), remote

	before := p.Playing
	if playing != nil {
		pl := *playing
		pl.UpdatedAt = now.UnixMilli()
		p.Playing = &pl
	} else {
		p.Playing = nil
	}
	switch {
	case before != nil && (playing == nil || before.Item.ID != playing.Item.ID):
		h.publish(Event{Type: EventPlaybackStopped, Item: &before.Item, User: &p.User, PositionMs: before.PositionMs,
			Finished: before.DurationMs > 0 && before.PositionMs >= before.DurationMs*92/100})
		if playing != nil {
			h.publish(Event{Type: EventPlaybackStarted, Item: &p.Playing.Item, User: &p.User, PositionMs: playing.PositionMs})
		}
	case before == nil && playing != nil:
		h.publish(Event{Type: EventPlaybackStarted, Item: &p.Playing.Item, User: &p.User, PositionMs: playing.PositionMs})
	case before != nil && before.Paused != playing.Paused:
		typ := EventPlaybackResumed
		if playing.Paused {
			typ = EventPlaybackPaused
		}
		h.publish(Event{Type: typ, Item: &p.Playing.Item, User: &p.User, PositionMs: playing.PositionMs})
	}
	h.publish(Event{Type: EventClientUpdated, Client: p.copy()})
	return nil
}

// stopped ends what p plays, telling subscribers. Call with h.mu held.
func (h *Hub) stopped(p *Client, finished bool) {
	if p.Playing == nil {
		return
	}
	pl := p.Playing
	p.Playing = nil
	h.publish(Event{Type: EventPlaybackStopped, Item: &pl.Item, User: &p.User, PositionMs: pl.PositionMs, Finished: finished})
}

// Leave is a player closing: it goes offline at once.
func (h *Hub) Leave(id string, account int64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	p, ok := h.clients[id]
	if !ok || p.account != account {
		return
	}
	p.Online = false
	h.stopped(p, false)
	h.publish(Event{Type: EventClientUpdated, Client: p.copy()})
}

// Poll waits up to wait for commands for player id, and counts as the
// player being there. ErrNoClient means it should report first.
func (h *Hub) Poll(ctx context.Context, id string, account int64, wait time.Duration) ([]Command, error) {
	h.mu.Lock()
	p, ok := h.clients[id]
	if !ok || p.account != account {
		h.mu.Unlock()
		return nil, ErrNoClient
	}
	p.LastSeen = h.now().UnixMilli()
	if !p.Online {
		p.Online = true
		h.publish(Event{Type: EventClientUpdated, Client: p.copy()})
	}
	q := p.queue
	h.mu.Unlock()

	var cmds []Command
	take := func() {
		for {
			select {
			case c := <-q:
				cmds = append(cmds, c)
			default:
				return
			}
		}
	}
	take()
	if len(cmds) == 0 && wait > 0 {
		t := time.NewTimer(min(wait, MaxWait))
		defer t.Stop()
		select {
		case c := <-q:
			cmds = append(cmds, c)
			take()
		case <-t.C:
		case <-ctx.Done():
		}
	}
	h.mu.Lock()
	if p, ok := h.clients[id]; ok {
		p.LastSeen = h.now().UnixMilli()
	}
	h.mu.Unlock()
	return cmds, nil
}

// Send queues a command for a player. allowed says whether the sender may
// control this player.
func (h *Hub) Send(id string, allowed func(Client) bool, c Command) (Command, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	p, ok := h.clients[id]
	if !ok || !allowed(*p) {
		return c, ErrNoClient
	}
	if !p.Online {
		return c, ErrOffline
	}
	if !slices.Contains(p.Commands, c.Type) {
		return c, ErrUnsupported
	}
	h.seq++
	c.ID = h.now().Format("20060102150405") + "-" + strconv.Itoa(h.seq)
	select {
	case p.queue <- c:
		return c, nil
	default:
		return c, ErrBusy
	}
}

// Client returns one player, if allowed may see it.
func (h *Hub) Client(id string, allowed func(Client) bool) (Client, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	p, ok := h.clients[id]
	if !ok || !allowed(*p) {
		return Client{}, false
	}
	return *p.copy(), true
}

// Players lists the players allowed may see, online ones first.
func (h *Hub) Clients(allowed func(Client) bool) []Client {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := []Client{}
	for _, p := range h.clients {
		if allowed(*p) {
			out = append(out, *p.copy())
		}
	}
	slices.SortFunc(out, func(a, b Client) int {
		if a.Online != b.Online {
			if a.Online {
				return -1
			}
			return 1
		}
		if a.Name != b.Name {
			if a.Name < b.Name {
				return -1
			}
			return 1
		}
		if a.ID < b.ID {
			return -1
		}
		return 1
	})
	return out
}

// Account is the account a player reports as.
func (p Client) Account() int64 { return p.account }

func (p *Client) copy() *Client {
	c := *p
	c.Commands = slices.Clone(p.Commands)
	if p.Playing != nil {
		pl := *p.Playing
		c.Playing = &pl
	}
	c.queue = nil
	return &c
}

// Publish adds an event to the feed.
func (h *Hub) Publish(e Event) {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.publish(e)
}

func (h *Hub) publish(e Event) {
	if e.Time == 0 {
		e.Time = h.now().UnixMilli()
	}
	for ch := range h.subs {
		select {
		case ch <- e:
		default:
			// A subscriber that falls far behind is cut off and reconnects,
			// starting again from the player list.
			delete(h.subs, ch)
			close(ch)
		}
	}
}

// Subscribe follows the feed until cancel is called. The channel closes if
// the subscriber falls too far behind.
func (h *Hub) Subscribe() (events <-chan Event, cancel func()) {
	ch := make(chan Event, 256)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if _, ok := h.subs[ch]; ok {
			delete(h.subs, ch)
			close(ch)
		}
	}
}
