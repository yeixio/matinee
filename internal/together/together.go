// Package together keeps Watch together sessions: people on different
// devices, at home or away, watching the same thing at the same moment.
//
// Everything goes through this server. Whoever starts a session gets a short
// code to share; others join with it. When anyone plays, pauses, seeks or
// moves on to another title, the session's state changes and every member's
// player follows. Players wait for changes with Wait, which returns as soon
// as the state moves past the version they last saw, so a change reaches
// everyone within one round trip.
//
// The state says where playback was at UpdatedAt and whether it's paused;
// players work out where it is now from the server's clock (ServerTime in
// every answer), and seek when they drift too far from it.
package together

import (
	"context"
	"crypto/rand"
	"errors"
	"slices"
	"strings"
	"sync"
	"time"
)

const (
	// memberTimeout is how long a member may go without asking for changes
	// before they count as gone. Players ask every MaxWait or sooner.
	memberTimeout = 70 * time.Second
	// emptyTimeout is how long a session lasts with nobody in it, so someone
	// whose connection dropped can come back to it.
	emptyTimeout = 15 * time.Minute
	// MaxWait is the longest Wait holds a request.
	MaxWait = 25 * time.Second
	// maxMembers keeps a session to a living-room sized group.
	maxMembers = 16
	// maxSessions is how many sessions a server keeps at once.
	maxSessions = 200
	codeLength  = 6
)

// Things people do in a session.
const (
	ActionPlay  = "play"  // carry on playing from PositionMs
	ActionPause = "pause" // pause at PositionMs
	ActionSeek  = "seek"  // jump to PositionMs, keeping play or pause
	ActionItem  = "item"  // switch everyone to ItemID, from PositionMs
	ActionJoin  = "join"  // someone joined
	ActionLeave = "leave" // someone left
	ActionStart = "start" // the session started
)

var (
	ErrNoSession = errors.New("that Watch together code doesn't work; it may have ended")
	ErrNotMember = errors.New("join the session first")
	ErrFull      = errors.New("this Watch together session is full")
	ErrTooMany   = errors.New("too many Watch together sessions right now")
	ErrAction    = errors.New("unknown action")
	ErrExists    = errors.New("that session is still going; join it")
)

func validCode(code string) bool {
	if len(code) != codeLength {
		return false
	}
	for _, c := range code {
		if !strings.ContainsRune(codeAlphabet, c) {
			return false
		}
	}
	return true
}

// Member is someone's player in a session.
type Member struct {
	ClientID string `json:"clientId"`
	UserID   int64  `json:"userId"`
	Name     string `json:"name"`               // the person
	Device   string `json:"device,omitempty"`   // such as "Living room TV"
	Platform string `json:"platform,omitempty"` // web, ios, android, tvos…
	Host     bool   `json:"host,omitempty"`     // started the session
	JoinedAt int64  `json:"joinedAt"`
	lastSeen time.Time
}

// Last is the latest thing someone did, for players to show ("Sam paused").
type Last struct {
	Action   string `json:"action"`
	By       string `json:"by"`
	ClientID string `json:"clientId"`
	At       int64  `json:"at"` // milliseconds
}

// State is a session as members see it.
type State struct {
	Code       string   `json:"code"`
	Version    int64    `json:"version"`
	ItemID     int64    `json:"itemId"`
	PositionMs int64    `json:"positionMs"` // where playback was at UpdatedAt
	Paused     bool     `json:"paused"`
	UpdatedAt  int64    `json:"updatedAt"`  // milliseconds, server clock
	ServerTime int64    `json:"serverTime"` // milliseconds, when this was sent
	Members    []Member `json:"members"`
	Last       *Last    `json:"last,omitempty"`
	// Ended means the session is over; players stop following it.
	Ended bool `json:"ended,omitempty"`
}

// Person is who's asking.
type Person struct {
	UserID   int64
	Name     string
	ClientID string
	Device   string
	Platform string
}

type session struct {
	State
	changed chan struct{} // closed and replaced on every change
	empty   time.Time     // when the last member left; zero while anyone's in
}

// Hub holds every session. The zero value isn't usable; call New.
type Hub struct {
	mu       sync.Mutex
	sessions map[string]*session
	// ended remembers sessions someone ended, so members still waiting hear
	// it's over instead of starting it again.
	ended map[string]time.Time
	now   func() time.Time
	// closing is closed when the server starts shutting down.
	closing   chan struct{}
	closeOnce sync.Once
}

func New() *Hub {
	return &Hub{sessions: map[string]*session{}, ended: map[string]time.Time{}, now: time.Now, closing: make(chan struct{})}
}

// Close is the server shutting down: waits return at once, so players hear
// about the restart and come back to their session afterwards (see Start).
func (h *Hub) Close() {
	h.closeOnce.Do(func() { close(h.closing) })
}

// codeAlphabet leaves out letters and digits people mix up when reading a
// code off a TV.
const codeAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

func newCode() string {
	b := make([]byte, codeLength)
	rand.Read(b)
	for i := range b {
		b[i] = codeAlphabet[int(b[i])%len(codeAlphabet)]
	}
	return string(b)
}

// NormalizeCode takes a code as someone typed it.
func NormalizeCode(code string) string {
	return strings.ToUpper(strings.NewReplacer("-", "", " ", "").Replace(strings.TrimSpace(code)))
}

func (h *Hub) ms() int64 { return h.now().UnixMilli() }

// Start makes a session around what p is playing, with p as its host.
//
// Sessions live in memory, so a server restart loses them. A member whose
// session is gone starts it again under the same code (resume), with what
// its own player has, and the others join it again as they come back.
func (h *Hub) Start(p Person, itemID, positionMs int64, paused bool, resume string) (State, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sweep()
	resume = NormalizeCode(resume)
	if resume != "" {
		if s := h.sessions[resume]; s != nil {
			return State{}, ErrExists
		}
		if _, ok := h.ended[resume]; ok || !validCode(resume) {
			return State{}, ErrNoSession
		}
	}
	// One session per player: starting again leaves the old one.
	for _, s := range h.sessions {
		if s.memberAs(p) != nil {
			h.leave(s, p.ClientID)
		}
	}
	if len(h.sessions) >= maxSessions {
		return State{}, ErrTooMany
	}
	code := resume
	for code == "" || h.sessions[code] != nil {
		code = newCode()
	}
	now := h.ms()
	s := &session{changed: make(chan struct{})}
	s.State = State{Code: code, Version: 1, ItemID: itemID, PositionMs: max(positionMs, 0), Paused: paused, UpdatedAt: now}
	s.Members = []Member{{ClientID: p.ClientID, UserID: p.UserID, Name: p.Name, Device: p.Device, Platform: p.Platform,
		Host: true, JoinedAt: now, lastSeen: h.now()}}
	s.Last = &Last{Action: ActionStart, By: p.Name, ClientID: p.ClientID, At: now}
	h.sessions[code] = s
	return h.view(s), nil
}

// Peek is a session's state for someone deciding whether to join.
func (h *Hub) Peek(code string) (State, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	s, err := h.get(code)
	if err != nil {
		return State{}, err
	}
	return h.view(s), nil
}

// Join adds p's player to a session, or welcomes it back.
func (h *Hub) Join(code string, p Person) (State, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	s, err := h.get(code)
	if err != nil {
		return State{}, err
	}
	if m := s.member(p.ClientID); m != nil {
		if m.UserID != p.UserID {
			return State{}, ErrNotMember
		}
		m.lastSeen = h.now()
		return h.view(s), nil
	}
	if len(s.Members) >= maxMembers {
		return State{}, ErrFull
	}
	for _, o := range h.sessions {
		if o != s && o.memberAs(p) != nil {
			h.leave(o, p.ClientID)
		}
	}
	s.Members = append(s.Members, Member{ClientID: p.ClientID, UserID: p.UserID, Name: p.Name, Device: p.Device,
		Platform: p.Platform, JoinedAt: h.ms(), lastSeen: h.now()})
	s.empty = time.Time{}
	h.changed(s, ActionJoin, p)
	return h.view(s), nil
}

// Act is a member playing, pausing, seeking or switching titles.
func (h *Hub) Act(code string, p Person, action string, positionMs, itemID int64) (State, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	s, err := h.get(code)
	if err != nil {
		return State{}, err
	}
	m := s.memberAs(p)
	if m == nil {
		return State{}, ErrNotMember
	}
	m.lastSeen = h.now()
	positionMs = max(positionMs, 0)
	switch action {
	case ActionPlay:
		s.Paused = false
	case ActionPause:
		s.Paused = true
	case ActionSeek:
	case ActionItem:
		if itemID <= 0 {
			return State{}, ErrAction
		}
		s.ItemID = itemID
	default:
		return State{}, ErrAction
	}
	s.PositionMs, s.UpdatedAt = positionMs, h.ms()
	h.changed(s, action, p)
	return h.view(s), nil
}

// Leave takes p's player out of a session.
func (h *Hub) Leave(code string, p Person) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	s, err := h.get(code)
	if err != nil {
		return err
	}
	if s.memberAs(p) == nil {
		return ErrNotMember
	}
	h.leave(s, p.ClientID)
	return nil
}

// End stops a session for everyone.
func (h *Hub) End(code string, p Person) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	s, err := h.get(code)
	if err != nil {
		return err
	}
	if s.memberAs(p) == nil {
		return ErrNotMember
	}
	h.end(s)
	return nil
}

// Wait returns the session's state once its version passes since, or when
// the wait runs out. Asking counts as being there.
func (h *Hub) Wait(ctx context.Context, code string, p Person, since int64, wait time.Duration) (State, error) {
	wait = min(wait, MaxWait)
	timer := time.NewTimer(wait)
	defer timer.Stop()
	for {
		h.mu.Lock()
		s, err := h.get(code)
		if err != nil {
			_, ended := h.ended[NormalizeCode(code)]
			h.mu.Unlock()
			if ended {
				// Someone ended it: say so, so players stop following.
				return State{Code: NormalizeCode(code), Ended: true, ServerTime: h.ms(), Members: []Member{}}, nil
			}
			return State{}, err
		}
		m := s.memberAs(p)
		if m == nil {
			h.mu.Unlock()
			return State{}, ErrNotMember
		}
		m.lastSeen = h.now()
		if s.Version != since || wait <= 0 {
			st := h.view(s)
			h.mu.Unlock()
			return st, nil
		}
		ch := s.changed
		h.mu.Unlock()
		select {
		case <-ch:
		case <-timer.C:
			wait = 0
		case <-h.closing:
			wait = 0
		case <-ctx.Done():
			return State{}, ctx.Err()
		}
	}
}

// Run drops members who stopped asking and sessions nobody came back to,
// until ctx ends.
func (h *Hub) Run(ctx context.Context) {
	t := time.NewTicker(10 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			h.mu.Lock()
			h.sweep()
			h.mu.Unlock()
		}
	}
}

// sweep needs h.mu.
func (h *Hub) sweep() {
	now := h.now()
	for _, s := range h.sessions {
		for _, m := range slices.Clone(s.Members) {
			if now.Sub(m.lastSeen) > memberTimeout {
				h.leave(s, m.ClientID)
			}
		}
		if len(s.Members) == 0 && now.Sub(s.empty) > emptyTimeout {
			h.end(s)
		}
	}
	for code, at := range h.ended {
		if now.Sub(at) > emptyTimeout {
			delete(h.ended, code)
		}
	}
}

func (h *Hub) get(code string) (*session, error) {
	s := h.sessions[NormalizeCode(code)]
	if s == nil {
		return nil, ErrNoSession
	}
	return s, nil
}

func (s *session) member(clientID string) *Member {
	for i := range s.Members {
		if s.Members[i].ClientID == clientID {
			return &s.Members[i]
		}
	}
	return nil
}

// memberAs is p's player in the session, if it's theirs.
func (s *session) memberAs(p Person) *Member {
	if m := s.member(p.ClientID); m != nil && m.UserID == p.UserID {
		return m
	}
	return nil
}

func (h *Hub) leave(s *session, clientID string) {
	i := slices.IndexFunc(s.Members, func(m Member) bool { return m.ClientID == clientID })
	if i < 0 {
		return
	}
	m := s.Members[i]
	s.Members = slices.Delete(s.Members, i, i+1)
	if m.Host && len(s.Members) > 0 {
		s.Members[0].Host = true // someone has to be
	}
	if len(s.Members) == 0 {
		s.empty = h.now()
	}
	h.changed(s, ActionLeave, Person{UserID: m.UserID, Name: m.Name, ClientID: m.ClientID})
}

func (h *Hub) end(s *session) {
	delete(h.sessions, s.Code)
	h.ended[s.Code] = h.now()
	s.Ended = true
	close(s.changed)
	s.changed = make(chan struct{})
}

func (h *Hub) changed(s *session, action string, p Person) {
	s.Version++
	s.Last = &Last{Action: action, By: p.Name, ClientID: p.ClientID, At: h.ms()}
	close(s.changed)
	s.changed = make(chan struct{})
}

// view copies a session's state for sending; needs h.mu.
func (h *Hub) view(s *session) State {
	st := s.State
	st.Members = slices.Clone(s.Members)
	st.ServerTime = h.ms()
	return st
}
