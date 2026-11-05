package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/yeixio/matinee/internal/arr"
	"github.com/yeixio/matinee/internal/overseerr"
	"github.com/yeixio/matinee/internal/store"
)

// Box Office in place of Overseerr: when Overseerr isn't connected and
// Radarr or Sonarr goes to a Box Office that has Discover, the Discover tab
// comes from Box Office instead. The rows are the same (with Box Office's
// recommendations for the library on top of the ones for what someone
// watched), and asking for a title is an ordinary request, which follows
// the server's request setting and waits for an admin when it says so.

// discoverTTL is how long Box Office's rows are kept here; Box Office
// keeps TMDB's lists longer, but where each title stands changes.
const discoverTTL = 10 * time.Minute

// boxOfficeDiscovery is the client for the Box Office that fills the
// Discover tab, if there is one.
func (s *Server) boxOfficeDiscovery(ctx context.Context) (*arr.Client, bool) {
	for _, app := range arr.Apps {
		if !s.boxOffices.has(app, "discover") {
			continue
		}
		if cl, _, err := s.arrClient(ctx, app); err == nil {
			return cl, true
		}
	}
	return nil, false
}

// Matinee's row ids, and Box Office's for the same rows.
var boxOfficeRowIDs = map[string]string{
	"trending":        "trending",
	"popular-movies":  "popular-movies",
	"popular-shows":   "popular-series",
	"upcoming-movies": "upcoming-movies",
	"upcoming-shows":  "upcoming-series",
	"for-you":         "for-you",
}

// boxOfficeHomeRows are the rows taken from Box Office's home, in order,
// after the ones for what someone watched.
var boxOfficeHomeRows = []string{"for-you", "trending", "popular-movies", "popular-shows", "upcoming-movies", "upcoming-shows"}

func boxOfficeKind(kind string) string {
	if kind == "show" {
		return "series"
	}
	return kind
}

// boxOfficeStatus is where a title from Box Office stands for Matinee: in
// a library here, on its way (Box Office has it), asked for, or none.
func (s *Server) boxOfficeStatus(ctx context.Context, t arr.BoxOfficeTitle) (discoverTitle, error) {
	kind := "movie"
	if t.Kind == "series" {
		kind = "show"
	}
	v := discoverTitle{Title: overseerr.Title{Kind: kind, TMDBID: t.TMDBID, Title: t.Title, Year: t.Year, Overview: t.Overview,
		Poster: t.Poster, Backdrop: t.Backdrop, Rating: t.Rating}}
	id, err := s.Store.LibraryMatch(ctx, kind, t.TMDBID, t.Title, t.Year)
	if err != nil || id != 0 {
		v.Status, v.ItemID = titleAvailable, id
		return v, err
	}
	if t.Status != "" {
		v.Status = titleAdded
		return v, nil
	}
	req, err := s.Store.OpenRequestTMDB(ctx, kind, t.TMDBID)
	if errors.Is(err, store.ErrNotFound) {
		return v, nil
	}
	if err != nil {
		return v, err
	}
	v.Status = titleAdded
	if req.State == store.RequestPending {
		v.Status = titleRequested
	}
	return v, nil
}

func (s *Server) boxOfficeTitles(ctx context.Context, titles []arr.BoxOfficeTitle, dropOwned bool) ([]discoverTitle, error) {
	out := []discoverTitle{}
	for _, t := range titles {
		v, err := s.boxOfficeStatus(ctx, t)
		if err != nil {
			return nil, err
		}
		if dropOwned && v.Status == titleAvailable {
			continue
		}
		out = append(out, v)
	}
	return out, nil
}

// boxOfficeDiscoverError answers for a failed call to Box Office's Discover.
func (s *Server) boxOfficeDiscoverError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, arr.ErrNotFound) {
		writeError(w, http.StatusNotFound, "no such row")
		return
	}
	s.Log.Warn("box office discover", "err", err)
	if !currentUser(r).IsAdmin {
		writeError(w, http.StatusBadGateway, "Box Office isn't answering right now. Try again later.")
		return
	}
	writeError(w, http.StatusBadGateway, err.Error())
}

func (s *Server) discoverFromBoxOffice(w http.ResponseWriter, r *http.Request, cl *arr.Client) {
	ctx, u := r.Context(), currentUser(r)
	can, _, err := s.canRequest(ctx, u)
	if err != nil {
		s.serverError(w, err)
		return
	}
	watched, err := s.Store.RecentlyWatched(ctx, u.ID, becauseRows)
	if err != nil {
		s.serverError(w, err)
		return
	}
	rows := make([]arr.BoxOfficeRow, len(watched))
	errs := make([]error, len(watched))
	var home arr.BoxOfficeDiscover
	var homeErr error
	var wg sync.WaitGroup
	for i, t := range watched {
		wg.Go(func() {
			rows[i], errs[i] = cached(&s.discoverCache, "bo:like:"+t.Kind+":"+strconv.Itoa(t.TMDBID), discoverTTL, func() (arr.BoxOfficeRow, error) {
				return cl.BoxOfficeDiscoverRow(ctx, "like-"+boxOfficeKind(t.Kind)+"-"+strconv.Itoa(t.TMDBID), 1)
			})
		})
	}
	wg.Go(func() {
		home, homeErr = cached(&s.discoverCache, "bo:home", discoverTTL, func() (arr.BoxOfficeDiscover, error) { return cl.BoxOfficeDiscoverHome(ctx) })
	})
	wg.Wait()

	out := discoverView{Rows: []discoverRow{}, CanRequest: can}
	add := func(row discoverRow, items []arr.BoxOfficeTitle, dropOwned bool) bool {
		if row.Items, err = s.boxOfficeTitles(ctx, items, dropOwned); err != nil {
			s.serverError(w, err)
			return false
		}
		if len(row.Items) > 0 {
			out.Rows = append(out.Rows, row)
		}
		return true
	}
	for i, t := range watched {
		if errs[i] != nil {
			s.Log.Warn("box office discover", "row", "because", "err", errs[i])
			continue
		}
		row := discoverRow{ID: "because-" + strconv.FormatInt(t.ItemID, 10), Title: "Because you watched " + t.Title, Because: t.Title, More: rows[i].More}
		if !add(row, rows[i].Items, true) {
			return
		}
	}
	if homeErr != nil {
		if len(out.Rows) == 0 {
			s.boxOfficeDiscoverError(w, r, homeErr)
			return
		}
		s.Log.Warn("box office discover", "err", homeErr)
	}
	for _, id := range boxOfficeHomeRows {
		for _, br := range home.Rows {
			if br.ID != boxOfficeRowIDs[id] {
				continue
			}
			if !add(discoverRow{ID: id, Title: br.Title, More: br.More}, br.Items, false) {
				return
			}
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) discoverRowFromBoxOffice(w http.ResponseWriter, r *http.Request, cl *arr.Client, page int) {
	ctx, u, id := r.Context(), currentUser(r), r.PathValue("id")
	row := discoverRow{ID: id}
	boID, dropOwned := boxOfficeRowIDs[id], false
	if v, ok := strings.CutPrefix(id, "because-"); ok {
		itemID, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			writeError(w, http.StatusNotFound, "no such row")
			return
		}
		it, err := s.Store.Item(ctx, u.ID, itemID)
		if errors.Is(err, store.ErrNotFound) || err == nil && (it.TMDBID == nil || it.Kind != "movie" && it.Kind != "show") {
			writeError(w, http.StatusNotFound, "no such row")
			return
		}
		if err != nil {
			s.serverError(w, err)
			return
		}
		boID, dropOwned = "like-"+boxOfficeKind(it.Kind)+"-"+strconv.FormatInt(*it.TMDBID, 10), true
		row.Title, row.Because = "Because you watched "+it.Title, it.Title
	}
	if boID == "" {
		writeError(w, http.StatusNotFound, "no such row")
		return
	}
	p, err := cached(&s.discoverCache, "bo:row:"+boID+":"+strconv.Itoa(page), discoverTTL, func() (arr.BoxOfficeRow, error) {
		return cl.BoxOfficeDiscoverRow(ctx, boID, page)
	})
	if err != nil {
		s.boxOfficeDiscoverError(w, r, err)
		return
	}
	if row.Title == "" {
		row.Title = p.Title
	}
	if row.Items, err = s.boxOfficeTitles(ctx, p.Items, dropOwned); err != nil {
		s.serverError(w, err)
		return
	}
	can, _, err := s.canRequest(ctx, u)
	if err != nil {
		s.serverError(w, err)
		return
	}
	pages := min(max(p.Pages, 1), discoverRowPages)
	row.More = page < pages
	writeJSON(w, http.StatusOK, map[string]any{"row": row, "page": page, "pages": pages, "canRequest": can})
}

func (s *Server) discoverSearchBoxOffice(w http.ResponseWriter, r *http.Request, cl *arr.Client, q string) {
	ctx := r.Context()
	found, err := cl.BoxOfficeDiscoverSearch(ctx, q)
	if err != nil {
		s.boxOfficeDiscoverError(w, r, err)
		return
	}
	out, err := s.boxOfficeTitles(ctx, found, false)
	if err != nil {
		s.serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// discoverRequestBoxOffice asks for a title from the Discover tab as any
// other request, through Radarr or Sonarr (both Box Office here).
func (s *Server) discoverRequestBoxOffice(w http.ResponseWriter, r *http.Request, auto bool, kind string, tmdbID int) {
	ctx := r.Context()
	app, _ := appFor(kind)
	cl, _, err := s.arrClient(ctx, app)
	if err != nil {
		s.arrError(w, r, app, err)
		return
	}
	var t arr.Title
	if kind == "movie" {
		t, err = cl.Lookup(ctx, tmdbID)
	} else {
		// Sonarr names shows by TVDB id; Box Office finds them by TMDB's too.
		var found []arr.Title
		found, err = cl.Search(ctx, "tmdb:"+strconv.Itoa(tmdbID))
		if err == nil {
			err = arr.ErrNotFound
			for _, f := range found {
				if f.TMDBID == tmdbID || len(found) == 1 {
					t, err = f, nil
					break
				}
			}
		}
	}
	if err != nil {
		s.arrError(w, r, app, err)
		return
	}
	req, ok := s.requestTitle(w, r, auto, kind, cl, t, 0)
	if !ok {
		return
	}
	s.discoverCache.clear()
	v := discoverTitle{Title: overseerr.Title{Kind: kind, TMDBID: t.TMDBID, Title: t.Title, Year: t.Year, Overview: t.Overview, Poster: t.Poster, Backdrop: t.Backdrop},
		Status: titleRequested}
	if req.State != store.RequestPending {
		v.Status = titleAdded
	}
	writeJSON(w, http.StatusCreated, v)
}
