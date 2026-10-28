package plugin

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/yeixio/matinee/internal/store"
)

// Event is something that happened on the server, as plugins hear of it.
type Event struct {
	Type    string            `json:"type"`
	Time    int64             `json:"time"` // milliseconds
	Item    *EventItem        `json:"item,omitempty"`
	Library *libraryRef       `json:"library,omitempty"`
	User    *EventUser        `json:"user,omitempty"`
	Played  *EventPlay        `json:"played,omitempty"`
	Config  map[string]string `json:"config,omitempty"`
}

// EventItem is the item an event is about.
type EventItem struct {
	ID        int64  `json:"id"`
	LibraryID int64  `json:"libraryId"`
	Kind      string `json:"kind"`
	Title     string `json:"title"`
	Year      int    `json:"year,omitempty"`
	ShowTitle string `json:"showTitle,omitempty"`
	Season    int    `json:"season,omitempty"`
	Episode   int    `json:"episode,omitempty"`
	TMDBID    int64  `json:"tmdbId,omitempty"`
}

// EventUser is who an event is about: an account or a profile.
type EventUser struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// EventPlay is how far someone got, for playback events.
type EventPlay struct {
	PositionMs int64 `json:"positionMs"`
	DurationMs int64 `json:"durationMs,omitempty"`
	Finished   bool  `json:"finished,omitempty"`
}

// LibraryEvent makes an event about a library.
func LibraryEvent(typ string, id int64, name, kind string) Event {
	return Event{Type: typ, Library: &libraryRef{id, name, kind}}
}

// Emit tells every running plugin that listens for this kind of event.
// It never waits for a plugin: one that falls far behind misses events.
func (m *Manager) Emit(e Event) {
	if m == nil {
		return
	}
	if e.Time == 0 {
		e.Time = time.Now().UnixMilli()
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, p := range m.plugins {
		if p.events == nil || !p.Running() || !slices.Contains(p.Events, e.Type) {
			continue
		}
		select {
		case p.events <- e:
		default:
			m.Log.Warn("plugin is behind; dropping an event", "plugin", p.ID, "event", e.Type)
		}
	}
}

// deliver hands a plugin its events one at a time, until stop closes.
func (m *Manager) deliver(p *Plugin, events <-chan Event, stop <-chan struct{}) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-stop
		cancel()
	}()
	for {
		select {
		case <-stop:
			return
		case e := <-events:
			in, err := m.serviceInstance(ctx, p)
			if err != nil {
				if ctx.Err() == nil {
					m.Log.Warn("plugin event", "plugin", p.ID, "event", e.Type, "err", err)
				}
				continue
			}
			m.mu.RLock()
			e.Config = p.configCopy()
			m.mu.RUnlock()
			if err := in.call(ctx, "event", e, nil); err != nil && ctx.Err() == nil {
				m.Log.Warn("plugin event", "plugin", p.ID, "event", e.Type, "err", err)
			}
		}
	}
}

// playbackTracker turns the progress reports players send every few
// seconds into playback.started and playback.stopped events: a report after
// a quiet spell starts playback, and a quiet spell or reaching the end
// stops it.
type playbackTracker struct {
	emit  func(Event)
	mu    sync.Mutex
	plays map[[2]int64]*play
	timer *time.Timer
}

type play struct {
	item EventItem
	user EventUser
	pos  int64
	dur  int64
	last time.Time
}

// quiet is how long without a progress report counts as stopped.
const quiet = 90 * time.Second

// Progress records a progress report. Call it for every report.
func (m *Manager) Progress(user EventUser, item EventItem, positionMs, durationMs int64, finished bool) {
	if m == nil || !m.listening(EventPlaybackStarted, EventPlaybackStopped) {
		return
	}
	m.plays.report(user, item, positionMs, durationMs, finished)
}

func (t *playbackTracker) report(user EventUser, item EventItem, pos, dur int64, finished bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.plays == nil {
		t.plays = map[[2]int64]*play{}
	}
	key := [2]int64{user.ID, item.ID}
	p, ok := t.plays[key]
	if !ok {
		p = &play{item: item, user: user}
		t.plays[key] = p
		t.emit(Event{Type: EventPlaybackStarted, Item: &p.item, User: &p.user, Played: &EventPlay{PositionMs: pos, DurationMs: dur}})
	}
	p.pos, p.dur, p.last = pos, dur, time.Now()
	if finished {
		delete(t.plays, key)
		t.emit(Event{Type: EventPlaybackStopped, Item: &p.item, User: &p.user, Played: &EventPlay{PositionMs: p.dur, DurationMs: p.dur, Finished: true}})
		return
	}
	if t.timer == nil {
		t.timer = time.AfterFunc(quiet, t.sweep)
	}
}

// sweep stops plays that went quiet.
func (t *playbackTracker) sweep() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.timer = nil
	for key, p := range t.plays {
		if time.Since(p.last) >= quiet {
			delete(t.plays, key)
			t.emit(Event{Type: EventPlaybackStopped, Item: &p.item, User: &p.user, Played: &EventPlay{PositionMs: p.pos, DurationMs: p.dur}})
		}
	}
	if len(t.plays) > 0 {
		t.timer = time.AfterFunc(quiet/3, t.sweep)
	}
}

// listening reports whether a running plugin listens for any of these events.
func (m *Manager) listening(types ...string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, p := range m.plugins {
		if p.Running() && slices.ContainsFunc(p.Events, func(e string) bool { return slices.Contains(types, e) }) {
			return true
		}
	}
	return false
}

func eventItem(it store.Item) EventItem {
	e := EventItem{ID: it.ID, LibraryID: it.LibraryID, Kind: it.Kind, Title: it.Title, ShowTitle: it.ShowTitle}
	if it.Year != nil {
		e.Year = *it.Year
	}
	if it.SeasonIndex != nil {
		e.Season = *it.SeasonIndex
	}
	if it.Kind == "episode" && it.IndexNumber != nil {
		e.Episode = *it.IndexNumber
	}
	if it.TMDBID != nil {
		e.TMDBID = *it.TMDBID
	}
	return e
}

// ItemAdded tells plugins that a scan added an item.
func (m *Manager) ItemAdded(ctx context.Context, itemID int64) {
	if m == nil || !m.listening(EventItemAdded) {
		return
	}
	it, err := m.Store.Item(ctx, 0, itemID)
	if err != nil {
		return
	}
	e := eventItem(it)
	m.Emit(Event{Type: EventItemAdded, Item: &e})
}

// Played records a progress report from a player, for playback events.
func (m *Manager) Played(user store.User, it store.Item, positionMs int64, finished bool) {
	var dur int64
	if it.DurationMs != nil {
		dur = *it.DurationMs
	}
	m.Progress(EventUser{ID: user.ID, Name: user.Username}, eventItem(it), positionMs, dur, finished)
}
