package api

import (
	"bufio"
	"context"
	"crypto/rand"
	"errors"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/yeixio/matinee/internal/livetv"
	"github.com/yeixio/matinee/internal/store"
	"github.com/yeixio/matinee/internal/stream"
)

// Live TV (see internal/livetv and store/livetv.go): admins add playlists of
// channels, and the household watches them with a guide. Kids profiles and
// friends don't see live TV: its channels aren't rated, and friends see
// only the libraries they were given.

// household lets through people at home who aren't on a kids profile.
func household(h http.HandlerFunc) http.HandlerFunc {
	return grownUps(func(w http.ResponseWriter, r *http.Request) {
		if currentUser(r).Friend {
			writeError(w, http.StatusForbidden, "only the household can change this")
			return
		}
		h(w, r)
	})
}

func (s *Server) liveRoutes() []route {
	return []route{
		{"GET /live/channels", reader, household(s.liveChannels)},
		{"GET /live/guide", reader, household(s.liveGuide)},
		{"GET /live/channels/{id}", reader, household(s.liveChannel)},
		{"POST /live/channels/{id}/play", reader, household(s.playLiveChannel)},
		{"GET /live/channels/{id}/hls", reader, household(s.liveRelay)},
		{"PUT /live/channels/{id}", adminOnly, s.updateLiveChannel},
		{"GET /live/sources", adminOnly, s.listLiveSources},
		{"POST /live/sources", adminOnly, s.addLiveSource},
		{"PUT /live/sources/{id}", adminOnly, s.updateLiveSource},
		{"DELETE /live/sources/{id}", adminOnly, s.deleteLiveSource},
		{"POST /live/sources/{id}/refresh", adminOnly, s.refreshLiveSource},
		{"GET /live/sources/{id}/channels", adminOnly, s.liveSourceChannels},
		{"GET /live/iptv-org", adminOnly, s.iptvOrgChoices},
	}
}

func (s *Server) liveManager() *livetv.Manager {
	s.liveTVOnce.Do(func() {
		if s.LiveTV == nil {
			s.LiveTV = &livetv.Manager{Store: s.Store, Log: s.Log}
		}
		key := make([]byte, 32)
		rand.Read(key)
		s.liveSigner = livetv.Signer{Key: key}
		s.iptvOrg = &livetv.Catalog{Client: s.LiveTV.Client, Base: s.LiveTV.IPTVOrg}
		if s.iptvOrg.Client == nil {
			s.iptvOrg.Client = livetv.Client
		}
	})
	return s.LiveTV
}

func (s *Server) liveClient() *http.Client {
	if c := s.liveManager().Client; c != nil {
		return c
	}
	return livetv.Client
}

// liveListing is a channel with what's on it now and next.
type liveListing struct {
	store.LiveChannel
	Now  *store.LiveProgram `json:"now,omitempty"`
	Next *store.LiveProgram `json:"next,omitempty"`
}

// liveChannels lists the channels that play, each with what's on now and
// next, and the categories they're in.
func (s *Server) liveChannels(w http.ResponseWriter, r *http.Request) {
	chans, err := s.Store.LiveChannels(r.Context(), "", false)
	if err != nil {
		s.serverError(w, err)
		return
	}
	now := time.Now().Unix()
	progs, err := s.Store.LivePrograms(r.Context(), now, now+6*3600, nil)
	if err != nil {
		s.serverError(w, err)
		return
	}
	byChannel := map[string][]store.LiveProgram{}
	for _, p := range progs {
		byChannel[p.ChannelID] = append(byChannel[p.ChannelID], p)
	}
	out := make([]liveListing, 0, len(chans))
	cats := map[string]bool{}
	for _, c := range chans {
		l := liveListing{LiveChannel: c}
		for i, p := range byChannel[c.ID] {
			if p.Start <= now && l.Now == nil {
				l.Now = &byChannel[c.ID][i]
			} else if p.Start > now && l.Next == nil {
				l.Next = &byChannel[c.ID][i]
			}
		}
		for _, cat := range c.Categories {
			cats[cat] = true
		}
		out = append(out, l)
	}
	categories := make([]string, 0, len(cats))
	for c := range cats {
		categories = append(categories, c)
	}
	slices.Sort(categories)
	writeJSON(w, http.StatusOK, map[string]any{"channels": out, "categories": categories})
}

// liveGuide is what's on every channel from ?from= (Unix seconds, default
// now) for ?hours= (default 4, at most 24).
func (s *Server) liveGuide(w http.ResponseWriter, r *http.Request) {
	from := time.Now().Unix()
	if v, err := strconv.ParseInt(r.URL.Query().Get("from"), 10, 64); err == nil && v > 0 {
		from = v
	}
	hours := int64(4)
	if v, err := strconv.ParseInt(r.URL.Query().Get("hours"), 10, 64); err == nil && v > 0 {
		hours = min(v, 24)
	}
	chans, err := s.Store.LiveChannels(r.Context(), "", false)
	if err != nil {
		s.serverError(w, err)
		return
	}
	progs, err := s.Store.LivePrograms(r.Context(), from, from+hours*3600, nil)
	if err != nil {
		s.serverError(w, err)
		return
	}
	visible := map[string]bool{}
	for _, c := range chans {
		visible[c.ID] = true
	}
	progs = slices.DeleteFunc(progs, func(p store.LiveProgram) bool { return !visible[p.ChannelID] })
	writeJSON(w, http.StatusOK, map[string]any{"from": from, "to": from + hours*3600, "channels": chans, "programs": progs})
}

// visibleChannel loads a channel the person may watch: admins may watch
// broken and hidden ones too, to try them.
func (s *Server) visibleChannel(w http.ResponseWriter, r *http.Request) (store.LiveChannel, bool) {
	c, err := s.Store.LiveChannel(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) || err == nil && (c.Hidden || c.Broken) && !currentUser(r).IsAdmin {
		writeError(w, http.StatusNotFound, "channel not found")
		return c, false
	}
	if err != nil {
		s.serverError(w, err)
		return c, false
	}
	return c, true
}

// liveChannel is a channel with what's on it over the next day.
func (s *Server) liveChannel(w http.ResponseWriter, r *http.Request) {
	c, ok := s.visibleChannel(w, r)
	if !ok {
		return
	}
	now := time.Now().Unix()
	progs, err := s.Store.LivePrograms(r.Context(), now, now+24*3600, []string{c.ID})
	if err != nil {
		s.serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"channel": c, "programs": progs})
}

func liveStream(c store.LiveChannel) livetv.Stream {
	return livetv.Stream{URL: c.StreamURL, UserAgent: c.UserAgent, Referrer: c.Referrer}
}

// relayLink is the server's link for an address a channel's playlists name.
func (s *Server) relayLink(channel, addr, token string) string {
	v := url.Values{"u": {livetv.EncodeURL(addr)}, "s": {s.liveSigner.Sign(channel, addr)}}
	if token != "" {
		v.Set("token", token)
	}
	return "/api/v1/live/channels/" + url.PathEscape(channel) + "/hls?" + v.Encode()
}

// playLiveChannel says where to play a channel: its HLS stream through the
// server, or, for a stream that isn't HLS, the server's repackaging of it.
// {"url": "/api/v1/…m3u8…"}. Players add ?token= as for other streams.
func (s *Server) playLiveChannel(w http.ResponseWriter, r *http.Request) {
	c, ok := s.visibleChannel(w, r)
	if !ok {
		return
	}
	s.liveManager()
	hls := strings.HasSuffix(strings.ToLower(pathOf(c.StreamURL)), ".m3u8")
	if !hls {
		// Ask the channel what it sends.
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		req, err := liveStream(c).Request(ctx, c.StreamURL)
		if err != nil {
			writeError(w, http.StatusBadGateway, "the channel isn't answering right now")
			return
		}
		resp, err := s.liveClient().Do(req)
		if err != nil || resp.StatusCode != http.StatusOK {
			if err == nil {
				resp.Body.Close()
			}
			writeError(w, http.StatusBadGateway, "the channel isn't answering right now")
			return
		}
		head := make([]byte, 512)
		n, _ := io.ReadFull(resp.Body, head)
		resp.Body.Close()
		hls = livetv.IsHLS(resp.Header.Get("Content-Type"), head[:n])
	}
	if hls {
		writeJSON(w, http.StatusOK, map[string]any{"url": s.relayLink(c.ID, c.StreamURL, ""), "live": true})
		return
	}
	if s.Transcoder == nil {
		writeError(w, http.StatusBadGateway, "the channel isn't answering right now")
		return
	}
	id, err := s.Transcoder.StartLive(r.Context(), currentUser(r).ID, stream.LiveInput{
		Channel: c.ID, URL: c.StreamURL, UserAgent: firstNonEmpty(c.UserAgent, livetv.UserAgent), Referrer: c.Referrer,
	})
	if err != nil {
		s.Log.Info("live channel didn't start", "channel", c.Name, "err", err)
		writeError(w, http.StatusBadGateway, "the channel isn't answering right now")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"url": "/api/v1/hls/" + id + "/index.m3u8", "live": true})
}

func pathOf(addr string) string {
	if u, err := url.Parse(addr); err == nil {
		return u.Path
	}
	return addr
}

// liveRelay fetches an address one of a channel's playlists named, and
// passes it on: playlists rewritten so what they name comes through here
// too, and everything else as it is.
func (s *Server) liveRelay(w http.ResponseWriter, r *http.Request) {
	c, ok := s.visibleChannel(w, r)
	if !ok {
		return
	}
	s.liveManager()
	addr, err := livetv.DecodeURL(r.URL.Query().Get("u"))
	if err != nil || !s.liveSigner.Verify(c.ID, addr, r.URL.Query().Get("s")) {
		writeError(w, http.StatusForbidden, "that link has expired")
		return
	}
	req, err := liveStream(c).Request(r.Context(), addr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if rg := r.Header.Get("Range"); rg != "" {
		req.Header.Set("Range", rg)
	}
	resp, err := s.liveClient().Do(req)
	if err != nil {
		if r.Context().Err() == nil {
			s.Log.Debug("live relay", "channel", c.Name, "err", err)
		}
		writeError(w, http.StatusBadGateway, "the channel isn't answering right now")
		return
	}
	defer resp.Body.Close()
	w.Header().Set("Access-Control-Allow-Origin", "*")
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		writeError(w, http.StatusBadGateway, "the channel isn't answering right now")
		return
	}
	br := bufio.NewReaderSize(resp.Body, 4096)
	head, _ := br.Peek(512)
	if livetv.IsHLS(resp.Header.Get("Content-Type"), head) {
		body, err := io.ReadAll(io.LimitReader(br, 8<<20))
		if err != nil {
			writeError(w, http.StatusBadGateway, "the channel isn't answering right now")
			return
		}
		token := r.URL.Query().Get("token")
		out := livetv.Rewrite(body, resp.Request.URL, func(abs string, _ bool) string { return s.relayLink(c.ID, abs, token) })
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		w.Header().Set("Cache-Control", "no-cache")
		w.Write(out)
		return
	}
	for _, h := range []string{"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges"} {
		if v := resp.Header.Get(h); v != "" {
			w.Header().Set(h, v)
		}
	}
	w.Header().Set("Cache-Control", "private, max-age=60")
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, br)
}

// updateLiveChannel hides a channel from everyone, or shows it again: {"hidden": true}.
func (s *Server) updateLiveChannel(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Hidden bool `json:"hidden"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	err := s.Store.SetLiveChannelHidden(r.Context(), r.PathValue("id"), req.Hidden)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "channel not found")
		return
	}
	if err != nil {
		s.serverError(w, err)
		return
	}
	c, err := s.Store.LiveChannel(r.Context(), r.PathValue("id"))
	if err != nil {
		s.serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) listLiveSources(w http.ResponseWriter, r *http.Request) {
	srcs, err := s.Store.LiveSources(r.Context())
	if err != nil {
		s.serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, srcs)
}

// liveSourceRequest is a source as an admin fills it in.
type liveSourceRequest struct {
	Name        string            `json:"name"`
	PlaylistURL string            `json:"playlistUrl"`
	IPTVOrg     *store.LiveFilter `json:"iptvOrg"`
	GuideURL    string            `json:"guideUrl"`
}

// source checks a request and makes it a source, or writes what's wrong.
func (req liveSourceRequest) source(w http.ResponseWriter) (store.LiveSource, bool) {
	src := store.LiveSource{Name: strings.TrimSpace(req.Name), IPTVOrg: req.IPTVOrg}
	if req.IPTVOrg == nil {
		u, err := livetv.Normalize(req.PlaylistURL)
		if err != nil {
			writeError(w, http.StatusBadRequest, "add the playlist's web address")
			return src, false
		}
		src.PlaylistURL = u
	} else {
		trim := func(l []string, n int) []string {
			var out []string
			for _, v := range l {
				if v = strings.TrimSpace(v); v != "" && len(v) <= 20 && !slices.Contains(out, v) && len(out) < n {
					out = append(out, v)
				}
			}
			return out
		}
		src.IPTVOrg = &store.LiveFilter{Countries: trim(req.IPTVOrg.Countries, 30), Categories: trim(req.IPTVOrg.Categories, 30), Languages: trim(req.IPTVOrg.Languages, 30)}
	}
	if strings.TrimSpace(req.GuideURL) != "" {
		u, err := livetv.Normalize(req.GuideURL)
		if err != nil {
			writeError(w, http.StatusBadRequest, "the guide's address isn't a web address")
			return src, false
		}
		src.GuideURL = u
	}
	if src.Name == "" {
		if src.IPTVOrg != nil {
			src.Name = "iptv-org"
		} else {
			src.Name = hostOf(src.PlaylistURL)
		}
	}
	if r := []rune(src.Name); len(r) > 80 {
		src.Name = string(r[:80])
	}
	return src, true
}

func (s *Server) liveSourceError(w http.ResponseWriter, r *http.Request, err error) {
	var se *livetv.StatusError
	switch {
	case errors.Is(err, livetv.ErrBadURL):
		writeError(w, http.StatusBadRequest, "add the playlist's web address")
	case errors.Is(err, livetv.ErrNotPlaylist):
		writeError(w, http.StatusBadRequest, "that address isn't a channel playlist (M3U)")
	case errors.Is(err, livetv.ErrNoChannels):
		writeError(w, http.StatusBadRequest, "that playlist has no channels Matinee can play")
	case errors.Is(err, livetv.ErrRefreshing):
		writeError(w, http.StatusConflict, "that source is being refreshed already")
	case errors.As(err, &se) || err != nil:
		writeErrorWith(w, http.StatusBadGateway, "the playlist couldn't be read: {{reason}}", map[string]string{"reason": err.Error()})
	}
}

// addLiveSource adds a playlist of channels (liveSourceRequest), reads its
// channels, and answers with it; its guide and the channels' checks follow.
func (s *Server) addLiveSource(w http.ResponseWriter, r *http.Request) {
	var req liveSourceRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	src, ok := req.source(w)
	if !ok {
		return
	}
	m := s.liveManager()
	id, err := s.Store.AddLiveSource(r.Context(), src)
	if err != nil {
		s.serverError(w, err)
		return
	}
	// Finish even if the browser stops waiting.
	ctx := context.WithoutCancel(r.Context())
	guide, err := m.RefreshChannels(ctx, id)
	if err != nil {
		s.Store.DeleteLiveSource(ctx, id)
		s.liveSourceError(w, r, err)
		return
	}
	go m.Finish(s.backgroundCtx(), id, guide)
	src, err = s.Store.LiveSource(ctx, id)
	if err != nil {
		s.serverError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, src)
}

func (s *Server) backgroundCtx() context.Context {
	if s.BackgroundCtx != nil {
		return s.BackgroundCtx
	}
	return context.Background()
}

// updateLiveSource changes a source and reads it again.
func (s *Server) updateLiveSource(w http.ResponseWriter, r *http.Request) {
	var req liveSourceRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	src, ok := req.source(w)
	if !ok {
		return
	}
	src.ID = r.PathValue("id")
	err := s.Store.UpdateLiveSource(r.Context(), src)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "source not found")
		return
	}
	if err != nil {
		s.serverError(w, err)
		return
	}
	s.refreshLiveSource(w, r)
}

func (s *Server) deleteLiveSource(w http.ResponseWriter, r *http.Request) {
	err := s.Store.DeleteLiveSource(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "source not found")
		return
	}
	if err != nil {
		s.serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// refreshLiveSource reads a source's channels again now, and answers with
// it; its guide and new channels' checks follow.
func (s *Server) refreshLiveSource(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := s.Store.LiveSource(r.Context(), id); errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "source not found")
		return
	}
	m := s.liveManager()
	ctx := context.WithoutCancel(r.Context())
	guide, err := m.RefreshChannels(ctx, id)
	if err != nil {
		s.liveSourceError(w, r, err)
		return
	}
	go m.Finish(s.backgroundCtx(), id, guide)
	src, err := s.Store.LiveSource(ctx, id)
	if err != nil {
		s.serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, src)
}

// liveSourceChannels lists all of a source's channels, broken and hidden
// ones too, for admins.
func (s *Server) liveSourceChannels(w http.ResponseWriter, r *http.Request) {
	chans, err := s.Store.LiveChannels(r.Context(), r.PathValue("id"), true)
	if err != nil {
		s.serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, chans)
}

// iptvOrgChoices lists the countries, categories and languages an admin
// can pick from iptv-org.
func (s *Server) iptvOrgChoices(w http.ResponseWriter, r *http.Request) {
	s.liveManager()
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	c, err := s.iptvOrg.Choices(ctx)
	if err != nil {
		if s.noInternet(w, err) {
			return
		}
		s.Log.Info("reading iptv-org's lists", "err", err)
		writeError(w, http.StatusBadGateway, "iptv-org isn't answering right now")
		return
	}
	writeJSON(w, http.StatusOK, c)
}
