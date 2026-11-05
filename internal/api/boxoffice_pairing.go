package api

import (
	"cmp"
	"context"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/yeixio/matinee/internal/arr"
	"github.com/yeixio/matinee/internal/store"
)

// More with Box Office: when Radarr or Sonarr goes to a Box Office that
// offers them (its status's boxOfficeFeatures), Matinee adds
//
//   - titles coming soon, with how far their download has got and when
//     it should be done ("progress"), shown to everyone, with Notify me;
//   - another edition of a movie in a library, such as the Director's Cut,
//     to replace its copy or to keep next to it ("editions");
//   - a better copy of a movie or an episode, or another for a bad one
//     ("upgrades").
//
// Editions and copies are asked for like titles: they follow the server's
// request setting (straight to Box Office when requests are added right
// away, or waiting for an admin), and Settings can make them always wait
// for an admin. Without such a Box Office none of this is offered, and
// Matinee works as it does with Radarr and Sonarr.

// settingAsksNeedApproval is "true" when editions and copies always wait
// for an admin, whatever the request setting says. Off by default.
const settingAsksNeedApproval = "boxoffice_asks_need_approval"

// Box Office's features.
const (
	featureProgress = "progress"
	featureEditions = "editions"
	featureUpgrades = "upgrades"
)

// offers says an app goes to a Box Office that said it has a feature.
// Unlike has, one that hasn't said yet doesn't count.
func (b *boxOffices) offers(app arr.App, feature string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	st := b.ids[app]
	return st.ID != "" && slices.Contains(st.Features, feature)
}

// boxOfficeFor is the client for an app that goes to a Box Office with a
// feature.
func (s *Server) boxOfficeFor(ctx context.Context, app arr.App, feature string) (*arr.Client, bool) {
	if !s.boxOffices.offers(app, feature) {
		return nil, false
	}
	cl, _, err := s.arrClient(ctx, app)
	return cl, err == nil
}

// pairingView says what Box Office offers here, for the clients.
type pairingView struct {
	Progress bool `json:"progress"`
	Editions bool `json:"editions"` // for movies
	Upgrades bool `json:"upgrades"` // for movies and episodes: which apps say so
	// UpgradeMovies and UpgradeShows say which kinds upgrades are for.
	UpgradeMovies bool `json:"upgradeMovies"`
	UpgradeShows  bool `json:"upgradeShows"`
	// AsksNeedApproval says editions and copies this person asks for wait
	// for an admin.
	AsksNeedApproval bool `json:"asksNeedApproval"`
}

// pairing is what Box Office offers, or nil without one.
func (s *Server) pairing(ctx context.Context, u store.User) (*pairingView, error) {
	p := pairingView{
		Progress:      s.boxOffices.offers(arr.Radarr, featureProgress) || s.boxOffices.offers(arr.Sonarr, featureProgress),
		Editions:      s.boxOffices.offers(arr.Radarr, featureEditions),
		UpgradeMovies: s.boxOffices.offers(arr.Radarr, featureUpgrades),
		UpgradeShows:  s.boxOffices.offers(arr.Sonarr, featureUpgrades),
	}
	p.Upgrades = p.UpgradeMovies || p.UpgradeShows
	if !p.Progress && !p.Editions && !p.Upgrades {
		return nil, nil
	}
	need, err := s.asksNeedApproval(ctx, u)
	p.AsksNeedApproval = need
	return &p, err
}

// asksNeedApproval says whether what someone asks Box Office for waits
// for an admin.
func (s *Server) asksNeedApproval(ctx context.Context, u store.User) (bool, error) {
	if u.IsAdmin {
		return false, nil
	}
	_, auto, err := s.canRequest(ctx, u)
	if err != nil || !auto {
		return true, err
	}
	v, err := s.Store.Setting(ctx, settingAsksNeedApproval)
	return v == "true", err
}

// ---- coming soon ----

// comingSoonTTL is how long Box Office's activity is kept here, so every
// open page asking doesn't each ask Box Office.
const comingSoonTTL = 5 * time.Second

// comingSoonTitle is a title on its way, as Home shows it: greyed out,
// with a progress bar.
type comingSoonTitle struct {
	Kind   string `json:"kind"` // movie or show
	TMDBID int    `json:"tmdbId,omitempty"`
	TVDBID int    `json:"tvdbId,omitempty"`
	Title  string `json:"title"`
	Year   int    `json:"year,omitempty"`
	Poster string `json:"poster,omitempty"`
	// Stage is searching (asked for, nothing found yet), or Box Office's:
	// queued, downloading, paused, waiting, checking, repairing,
	// unpacking, importing, waitingToMove, imported, failed, quarantined.
	Stage   string  `json:"stage"`
	Percent float64 `json:"percent"`
	ETA     int64   `json:"etaSeconds,omitempty"`
	// Downloads is how many are on the way for it (episodes, say).
	Downloads int    `json:"downloads,omitempty"`
	Season    int    `json:"seasonNumber,omitempty"`
	Episode   int    `json:"episodeNumber,omitempty"`
	Edition   string `json:"edition,omitempty"`
	Mode      string `json:"mode,omitempty"`
	// Message says what went wrong, for admins.
	Message string `json:"message,omitempty"`
	// ItemID is the title in a library, for one already there (another
	// edition or a better copy on its way).
	ItemID    int64 `json:"itemId,omitempty"`
	NotifyMe  bool  `json:"notifyMe,omitempty"`
	Requested bool  `json:"requested,omitempty"` // by this person
	UpdatedAt int64 `json:"updatedAt,omitempty"`
}

type comingSoonView struct {
	Available bool              `json:"available"`
	Titles    []comingSoonTitle `json:"titles"`
}

// stageOrder ranks where a download is, the one furthest along first, to
// pick the one a title shows when several are on the way.
var stageOrder = []string{"importing", "unpacking", "repairing", "checking", "waiting", "downloading", "queued", "paused", "waitingToMove", "searching", "failed", "quarantined", "imported"}

func stageRank(stage string) int {
	if i := slices.Index(stageOrder, stage); i >= 0 {
		return i
	}
	return len(stageOrder)
}

// boxOfficeActivity is what the Box Office with progress is downloading.
func (s *Server) boxOfficeActivity(ctx context.Context) (arr.BoxOfficeActivity, bool, error) {
	for _, app := range arr.Apps {
		cl, ok := s.boxOfficeFor(ctx, app, featureProgress)
		if !ok {
			continue
		}
		// Both of Box Office's APIs answer with everything: one is enough.
		a, err := cached(&s.arrCache, "boxoffice-activity:"+s.boxOffices.id(app), comingSoonTTL, func() (arr.BoxOfficeActivity, error) {
			callCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			return cl.BoxOfficeActivity(callCtx)
		})
		return a, true, err
	}
	return arr.BoxOfficeActivity{}, false, nil
}

// getComingSoon lists the titles on their way, for everyone: what Box
// Office is downloading, and requests it hasn't found yet.
func (s *Server) getComingSoon(w http.ResponseWriter, r *http.Request) {
	ctx, u := r.Context(), currentUser(r)
	a, ok, err := s.boxOfficeActivity(ctx)
	if !ok {
		writeJSON(w, http.StatusOK, comingSoonView{Titles: []comingSoonTitle{}})
		return
	}
	if err != nil {
		s.Log.Warn("Box Office's activity", "err", err)
		writeJSON(w, http.StatusOK, comingSoonView{Available: true, Titles: []comingSoonTitle{}})
		return
	}
	out, err := s.comingSoon(ctx, u, a)
	if err != nil {
		s.serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, comingSoonView{Available: true, Titles: out})
}

func (s *Server) comingSoon(ctx context.Context, u store.User, a arr.BoxOfficeActivity) ([]comingSoonTitle, error) {
	notify, err := s.Store.NotifyMeTitles(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	byKey := map[string]int{}
	out := []comingSoonTitle{}
	add := func(t comingSoonTitle) {
		k := store.NotifyMeKey(t.Kind, t.TMDBID, t.TVDBID)
		if t.Kind == "show" {
			k = store.NotifyMeKey(t.Kind, 0, t.TVDBID)
		}
		i, ok := byKey[k]
		if !ok {
			t.Downloads = 1
			byKey[k] = len(out)
			out = append(out, t)
			return
		}
		cur := &out[i]
		n := cur.Downloads + 1
		if stageRank(t.Stage) < stageRank(cur.Stage) {
			t.ItemID, t.Requested, t.Poster = cmp.Or(cur.ItemID, t.ItemID), cur.Requested || t.Requested, cmp.Or(t.Poster, cur.Poster)
			*cur = t
		}
		cur.Downloads = n
	}
	for _, d := range a.Items {
		kind := "movie"
		if d.Kind == "series" {
			kind = "show"
		}
		t := comingSoonTitle{Kind: kind, TMDBID: d.TMDBID, TVDBID: d.TVDBID, Title: d.Title, Year: d.Year, Stage: d.Stage,
			Percent: d.Percent, ETA: d.ETA, Edition: d.Edition, Mode: d.Mode}
		if len(d.Episodes) > 0 {
			t.Season, t.Episode = d.Episodes[0].Season, d.Episodes[0].Episode
		}
		if u.IsAdmin {
			t.Message = d.Message
		}
		if !d.UpdatedAt.IsZero() {
			t.UpdatedAt = d.UpdatedAt.Unix()
		}
		out2, err := s.comingSoonDetails(ctx, u, t, d.Poster, notify)
		if err != nil {
			return nil, err
		}
		add(out2)
	}
	// Requests that are approved but not on their way yet are being looked
	// for. Shown for a few weeks; after that they're no news.
	reqs, err := s.Store.Requests(ctx, 0, 200)
	if err != nil {
		return nil, err
	}
	since := time.Now().AddDate(0, 0, -30).Unix()
	for _, req := range reqs {
		if req.State != store.RequestApproved || req.Ask != "" || req.UpdatedAt < since {
			continue
		}
		k := store.NotifyMeKey(req.Kind, req.TMDBID, req.TVDBID)
		if req.Kind == "show" {
			k = store.NotifyMeKey(req.Kind, 0, req.TVDBID)
		}
		if _, ok := byKey[k]; ok {
			continue
		}
		if id, err := s.Store.LibraryMatch(ctx, req.Kind, req.TMDBID, req.Title, req.Year); err != nil {
			return nil, err
		} else if id != 0 {
			continue
		}
		t := comingSoonTitle{Kind: req.Kind, TMDBID: req.TMDBID, TVDBID: req.TVDBID, Title: req.Title, Year: req.Year, Stage: "searching", UpdatedAt: req.UpdatedAt}
		t, err := s.comingSoonDetails(ctx, u, t, req.Poster, notify)
		if err != nil {
			return nil, err
		}
		add(t)
	}
	// Furthest along first; what's done or stuck last.
	slices.SortStableFunc(out, func(a, b comingSoonTitle) int {
		return cmp.Or(stageRank(a.Stage)-stageRank(b.Stage), cmp.Compare(a.ETA, b.ETA))
	})
	return out, nil
}

// comingSoonDetails fills in a title's poster, its library item, and
// whether this person asked for it or to be told.
func (s *Server) comingSoonDetails(ctx context.Context, u store.User, t comingSoonTitle, poster string, notify map[string]bool) (comingSoonTitle, error) {
	t.Poster = poster
	if t.Kind == "movie" || t.TMDBID != 0 {
		id, err := s.Store.LibraryMatch(ctx, t.Kind, t.TMDBID, t.Title, t.Year)
		if err != nil {
			return t, err
		}
		t.ItemID = id
	}
	t.NotifyMe = notify[store.NotifyMeKey(t.Kind, t.TMDBID, t.TVDBID)]
	ids, err := s.Store.Requesters(ctx, t.Kind, t.TMDBID, t.TVDBID)
	if err != nil {
		return t, err
	}
	t.Requested = slices.Contains(ids, u.ID) && !t.NotifyMe
	if t.Poster == "" {
		if req, err := s.Store.OpenRequest(ctx, t.Kind, titleID(t.Kind, t.TMDBID, t.TVDBID)); err == nil {
			t.Poster = req.Poster
		}
	}
	return t, nil
}

// postNotifyMe asks for, or stops, being told when a title on its way
// arrives.
func (s *Server) postNotifyMe(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Kind   string `json:"kind"`
		TMDBID int    `json:"tmdbId"`
		TVDBID int    `json:"tvdbId"`
		On     *bool  `json:"on"`
	}
	if err := readJSON(r, &in); err != nil || (in.Kind != "movie" && in.Kind != "show") || (in.TMDBID <= 0 && in.TVDBID <= 0) {
		writeError(w, http.StatusBadRequest, "say which title: kind (movie or show) and tmdbId or tvdbId")
		return
	}
	on := in.On == nil || *in.On
	if err := s.Store.NotifyMe(r.Context(), currentUser(r).ID, in.Kind, in.TMDBID, in.TVDBID, on); err != nil {
		s.serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"notifyMe": on})
}

// ---- editions and better copies ----

// boxOfficeTitle is the movie or show an item is in Box Office, and the
// client for it, with a feature.
func (s *Server) boxOfficeTitle(w http.ResponseWriter, r *http.Request, it store.Item, feature string) (managed, bool) {
	app, ok := appFor(it.Kind)
	if !ok || !replaceable(it.Kind) {
		writeError(w, http.StatusBadRequest, "only movies and episodes have other copies")
		return managed{}, false
	}
	if feature == featureEditions && it.Kind != "movie" {
		writeError(w, http.StatusBadRequest, "only movies have editions")
		return managed{}, false
	}
	if !s.boxOffices.offers(app, feature) {
		writeError(w, http.StatusNotFound, "Box Office isn't connected for "+map[arr.App]string{arr.Radarr: "movies", arr.Sonarr: "shows"}[app])
		return managed{}, false
	}
	m, err := s.managedItem(r.Context(), it)
	if err != nil {
		s.arrError(w, r, app, err)
		return m, false
	}
	return m, true
}

// getItemEditions lists the editions of a movie Box Office can get, and
// those the library has. It takes a few seconds: Box Office searches.
func (s *Server) getItemEditions(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireRequester(w, r); !ok {
		return
	}
	it, ok := s.loadItem(w, r)
	if !ok {
		return
	}
	m, ok := s.boxOfficeTitle(w, r, it, featureEditions)
	if !ok {
		return
	}
	e, err := cached(&s.arrCache, "boxoffice-editions:"+strconv.Itoa(m.title.TMDBID), 10*time.Minute, func() (arr.BoxOfficeEditions, error) {
		return m.client.BoxOfficeEditions(r.Context(), m.title.TMDBID)
	})
	if err != nil {
		s.arrError(w, r, m.app, err)
		return
	}
	if e.Editions == nil {
		e.Editions = []arr.BoxOfficeEdition{}
	}
	if !currentUser(r).IsAdmin {
		e.Errors = nil
	}
	writeJSON(w, http.StatusOK, e)
}

// askResult is what came of asking for an edition or a copy: sent to Box
// Office, or waiting for an admin.
type askResult struct {
	State   string      `json:"state"` // approved (sent to Box Office), pending, failed
	Request requestView `json:"request"`
}

// postItemEdition asks for another edition of a movie: to replace its copy
// (mode replace) or to keep next to it (alongside).
func (s *Server) postItemEdition(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Edition string `json:"edition"`
		Mode    string `json:"mode"`
	}
	if err := readJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	in.Edition = clip(strings.TrimSpace(in.Edition), 100)
	if in.Edition == "" {
		writeError(w, http.StatusBadRequest, "say which edition")
		return
	}
	in.Mode = cmp.Or(in.Mode, "alongside")
	if in.Mode != "alongside" && in.Mode != "replace" {
		writeError(w, http.StatusBadRequest, "mode must be replace or alongside")
		return
	}
	s.ask(w, r, featureEditions, store.Request{Ask: store.AskEdition, Edition: in.Edition, Mode: in.Mode})
}

// postItemCopy asks for a better copy of a movie or an episode (want
// better), or another because the one there is bad (want replace).
func (s *Server) postItemCopy(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Want   string `json:"want"`
		Reason string `json:"reason"`
	}
	if err := readJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if in.Want != store.AskBetter && in.Want != store.AskReplace {
		writeError(w, http.StatusBadRequest, "want must be better or replace")
		return
	}
	s.ask(w, r, featureUpgrades, store.Request{Ask: in.Want, Reason: clip(strings.TrimSpace(in.Reason), 500)})
}

// ask records what someone asked Box Office for an item, and sends it on
// unless it waits for an admin.
func (s *Server) ask(w http.ResponseWriter, r *http.Request, feature string, req store.Request) {
	if _, ok := s.requireRequester(w, r); !ok {
		return
	}
	it, ok := s.loadItem(w, r)
	if !ok {
		return
	}
	m, ok := s.boxOfficeTitle(w, r, it, feature)
	if !ok {
		return
	}
	ctx, u := r.Context(), currentUser(r)
	req.UserID, req.Title, req.Year, req.Poster = u.ID, m.title.Title, m.title.Year, m.title.Poster
	if m.app == arr.Radarr {
		req.Kind, req.TMDBID = "movie", m.title.TMDBID
	} else {
		req.Kind, req.TMDBID, req.TVDBID, req.Season, req.Episode = "show", m.title.TMDBID, m.title.TVDBID, m.season, m.episode
		if req.TVDBID == 0 || req.Episode == 0 {
			writeError(w, http.StatusBadRequest, "Box Office doesn't know which episode it is")
			return
		}
	}
	need, err := s.asksNeedApproval(ctx, u)
	if err != nil {
		s.serverError(w, err)
		return
	}
	saved, err := s.Store.CreateRequest(ctx, req)
	if err != nil {
		s.serverError(w, err)
		return
	}
	if !need {
		if saved, err = s.approve(ctx, saved, 0); err != nil {
			s.serverError(w, err)
			return
		}
	}
	v, err := s.viewRequest(ctx, saved)
	if err != nil {
		s.serverError(w, err)
		return
	}
	code := http.StatusCreated
	if saved.State == store.RequestFailed {
		code = http.StatusBadGateway
	}
	writeJSON(w, code, askResult{State: saved.State, Request: v})
}

// sendAsk passes an approved edition or copy request on to Box Office.
func (s *Server) sendAsk(ctx context.Context, req store.Request) error {
	app, _ := appFor(req.Kind)
	feature := featureUpgrades
	if req.Ask == store.AskEdition {
		feature = featureEditions
	}
	cl, ok := s.boxOfficeFor(ctx, app, feature)
	if !ok {
		return errBoxOfficeGone
	}
	defer s.arrCache.clear() // editions and coming soon change
	switch req.Ask {
	case store.AskEdition:
		return cl.BoxOfficeAskEdition(ctx, req.TMDBID, req.Edition, req.Mode)
	case store.AskBetter, store.AskReplace:
		u := arr.BoxOfficeUpgrade{Want: req.Ask, Reason: req.Reason}
		if req.Kind == "movie" {
			u.TMDBID = req.TMDBID
		} else {
			u.TVDBID, u.Season, u.Episode = req.TVDBID, req.Season, req.Episode
		}
		return cl.BoxOfficeAskUpgrade(ctx, u)
	}
	return errors.New("the request doesn't say what to ask Box Office for")
}

var errBoxOfficeGone = errors.New("Box Office isn't connected any more")
