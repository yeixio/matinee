package livetv

import (
	"cmp"
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/yeixio/matinee/internal/store"
)

// How often Matinee goes back to sources and streams.
const (
	refreshEvery = 12 * time.Hour // playlists and guides
	checkEvery   = 6 * time.Hour  // each channel's stream
	guideAhead   = 48 * time.Hour // how far ahead the guide is kept
	checkWorkers = 8
)

// Client fetches playlists, guides and streams. Only admins add sources,
// so it may reach the home network, where a tuner's playlist often is.
var Client = &http.Client{Transport: &http.Transport{
	Proxy:                 http.ProxyFromEnvironment,
	DialContext:           (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
	TLSHandshakeTimeout:   10 * time.Second,
	ResponseHeaderTimeout: 20 * time.Second,
	IdleConnTimeout:       90 * time.Second,
	MaxIdleConnsPerHost:   8,
}}

// Manager keeps sources' channels and guides up to date, and checks
// channels' streams.
type Manager struct {
	Store *store.Store
	Log   *slog.Logger
	// Client is used for everything; nil is Client. IPTVOrg is iptv-org's
	// base, empty for IPTVOrgBase.
	Client  *http.Client
	IPTVOrg string

	mu      sync.Mutex
	running map[string]bool // sources being refreshed
}

func (m *Manager) client() *http.Client { return cmp.Or(m.Client, Client) }

// Spec is where a source's channels come from.
func (m *Manager) Spec(src store.LiveSource) Spec {
	sp := Spec{Playlist: src.PlaylistURL, Base: m.IPTVOrg}
	if src.IPTVOrg != nil {
		sp.IPTVOrg = &Filter{Countries: src.IPTVOrg.Countries, Categories: src.IPTVOrg.Categories, Languages: src.IPTVOrg.Languages}
	}
	return sp
}

// Refresh reads a source's playlist and guide again, then checks any new
// channels. A refresh already running for the source is left to finish.
func (m *Manager) Refresh(ctx context.Context, id string) error {
	guide, err := m.RefreshChannels(ctx, id)
	if err != nil {
		return err
	}
	m.Finish(ctx, id, guide)
	return nil
}

// RefreshChannels reads a source's playlist again, and returns the guide
// to read for it. Finish reads that guide and checks new channels.
func (m *Manager) RefreshChannels(ctx context.Context, id string) (string, error) {
	m.mu.Lock()
	if m.running == nil {
		m.running = map[string]bool{}
	}
	if m.running[id] {
		m.mu.Unlock()
		return "", ErrRefreshing
	}
	m.running[id] = true
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		delete(m.running, id)
		m.mu.Unlock()
	}()

	src, err := m.Store.LiveSource(ctx, id)
	if err != nil {
		return "", err
	}
	pctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	p, err := Load(pctx, m.client(), m.Spec(src))
	cancel()
	if err != nil {
		m.Log.Info("live TV source didn't refresh", "source", src.Name, "err", err)
		m.Store.SetLiveSourceError(ctx, id, err.Error())
		return "", err
	}
	chans := make([]store.LiveChannel, 0, len(p.Entries))
	for _, e := range p.Entries {
		if isAdult(e.Groups) {
			continue
		}
		chans = append(chans, store.LiveChannel{Name: e.Name, Number: e.Number, GuideID: e.GuideID, Logo: e.Logo, Categories: e.Groups,
			StreamURL: e.URL, UserAgent: e.UserAgent, Referrer: e.Referrer})
	}
	if len(chans) == 0 {
		m.Store.SetLiveSourceError(ctx, id, ErrNoChannels.Error())
		return "", ErrNoChannels
	}
	if err := m.Store.SaveLiveChannels(ctx, id, chans); err != nil {
		return "", err
	}
	m.Log.Info("live TV source refreshed", "source", src.Name, "channels", len(chans))
	return cmp.Or(src.GuideURL, p.GuideURL), nil
}

// ErrRefreshing means the source is being read already.
var ErrRefreshing = errors.New("that source is being refreshed already")

// Finish reads a source's guide, if it has one, and checks channels that
// haven't been checked yet.
func (m *Manager) Finish(ctx context.Context, id, guide string) {
	if guide != "" {
		m.refreshGuide(ctx, id, guide)
	}
	m.check(ctx, 0)
}

func isAdult(groups []string) bool {
	for _, g := range groups {
		switch strings.ToLower(g) {
		case "xxx", "adult", "adults", "18+":
			return true
		}
	}
	return false
}

// variant matches what playlists add to a channel's name to tell its
// streams apart, such as " (720p)" or " [Geo-blocked]".
var variant = regexp.MustCompile(`\s*[\(\[][^\)\]]*[\)\]]\s*$`)

func nameKey(s string) string {
	for {
		t := variant.ReplaceAllString(s, "")
		if t == s {
			break
		}
		s = t
	}
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

// refreshGuide reads a source's guide and keeps what's on its channels
// over the next two days. Channels are matched by their guide id, or by
// name when the playlist gives none.
func (m *Manager) refreshGuide(ctx context.Context, sourceID, addr string) {
	chans, err := m.Store.LiveChannels(ctx, sourceID, true)
	if err != nil {
		return
	}
	byGuide := map[string][]string{}
	byName := map[string][]string{}
	for _, c := range chans {
		if c.GuideID != "" {
			byGuide[c.GuideID] = append(byGuide[c.GuideID], c.ID)
			// iptv-org's ids name a feed after @; guides use the channel's.
			if base, _, ok := strings.Cut(c.GuideID, "@"); ok {
				byGuide[base] = append(byGuide[base], c.ID)
			}
		} else {
			byName[nameKey(c.Name)] = append(byName[nameKey(c.Name)], c.ID)
		}
	}
	targets := map[string][]string{} // guide channel id → channels
	for k, v := range byGuide {
		targets[k] = v
	}
	var programs []store.LiveProgram
	err = func() error {
		gctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		defer cancel()
		u, err := Normalize(addr)
		if err != nil {
			return err
		}
		resp, err := Open(gctx, m.client(), u)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		from := time.Now().Add(-2 * time.Hour)
		return ParseGuide(io.LimitReader(resp.Body, maxGuide), from, time.Now().Add(guideAhead),
			func(id string) bool { return len(targets[id]) > 0 },
			func(gc GuideChannel) {
				if len(byGuide[gc.ID]) > 0 {
					return
				}
				for _, n := range gc.Names {
					if ids := byName[nameKey(n)]; len(ids) > 0 {
						targets[gc.ID] = append(targets[gc.ID], ids...)
						delete(byName, nameKey(n))
					}
				}
			},
			func(p Program) {
				for _, ch := range targets[p.Channel] {
					programs = append(programs, store.LiveProgram{ChannelID: ch, Start: p.Start.Unix(), Stop: p.Stop.Unix(), Title: p.Title,
						Subtitle: p.Subtitle, Description: p.Description, Category: p.Category, Image: p.Image})
				}
			})
	}()
	msg := ""
	if err != nil {
		msg = err.Error()
		m.Log.Info("live TV guide didn't load", "source", sourceID, "err", err)
	} else if len(programs) == 0 && len(chans) > 0 {
		msg = "the guide has nothing for these channels over the next two days"
	}
	if msg != "" && len(programs) == 0 {
		m.Store.SaveLiveGuide(ctx, sourceID, nil, msg)
		return
	}
	if err := m.Store.SaveLiveGuide(ctx, sourceID, programs, ""); err != nil {
		m.Log.Warn("saving the live TV guide", "err", err)
	}
}

// check checks the streams of channels not checked since before (Unix
// seconds; 0 means only ones never checked), a few at a time.
func (m *Manager) check(ctx context.Context, before int64) {
	if before == 0 {
		before = 1
	}
	done := map[string]bool{} // each channel once per round
	for ctx.Err() == nil {
		chans, err := m.Store.LiveChannelsToCheck(ctx, before, 200)
		chans = slices.DeleteFunc(chans, func(c store.LiveChannel) bool { return done[c.ID] })
		if err != nil || len(chans) == 0 {
			return
		}
		for _, c := range chans {
			done[c.ID] = true
		}
		work := make(chan store.LiveChannel)
		var wg sync.WaitGroup
		for range checkWorkers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for c := range work {
					cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
					err := Check(cctx, m.client(), Stream{URL: c.StreamURL, UserAgent: c.UserAgent, Referrer: c.Referrer})
					cancel()
					if ctx.Err() != nil {
						return
					}
					m.Store.RecordLiveCheck(ctx, c.ID, err == nil)
				}
			}()
		}
		for _, c := range chans {
			work <- c
		}
		close(work)
		wg.Wait()
	}
}

// CheckNow checks every channel's stream.
func (m *Manager) CheckNow(ctx context.Context) { m.check(ctx, time.Now().Unix()+1) }

// Run keeps everything up to date until ctx ends.
func (m *Manager) Run(ctx context.Context) {
	tick := time.NewTicker(15 * time.Minute)
	defer tick.Stop()
	for {
		m.runOnce(ctx)
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

func (m *Manager) runOnce(ctx context.Context) {
	srcs, err := m.Store.LiveSources(ctx)
	if err != nil {
		return
	}
	for _, src := range srcs {
		if time.Since(time.Unix(src.RefreshedAt, 0)) > refreshEvery {
			if err := m.Refresh(ctx, src.ID); err != nil && !errors.Is(err, context.Canceled) {
				continue
			}
		}
	}
	m.check(ctx, time.Now().Add(-checkEvery).Unix())
	m.Store.PruneLivePrograms(ctx, time.Now().Add(-6*time.Hour).Unix())
}
