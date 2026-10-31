package api

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/yeixio/matinee/internal/store"
)

// Each row of titles (on Home, under a title, …) that has more than it
// shows says so with `more`, and GET /rows/{id} has all of its titles, for
// the row's own page. Rows are:
//
//	continueWatching, recentlyAdded, newReleases, watchList
//	continueListening, newEpisodes, genre:{genre}, holiday:{id}, collection:{id}   (Home's other rows)
//	related:{item id}                              (More like this)

// Home's own rows, each a list in the /home answer.
const (
	rowContinue  = "continueWatching"
	rowRecent    = "recentlyAdded"
	rowReleases  = "newReleases"
	rowWatchList = "watchList"
	// Books and episodes someone is listening to, a row among Home's
	// others, so apps that don't know it still show it. New episodes
	// (podcasts.go) is one too.
	rowListening = "continueListening"
)

const (
	continueItems = 20   // titles in Continue watching on Home
	homeRowItems  = 30   // titles in Home's other lists
	rowAllItems   = 1000 // titles on a row's page at most
)

// cut keeps the first n items, and says whether there were more.
func cut(items []store.Item, n int) ([]store.Item, bool) {
	if len(items) > n {
		return items[:n], true
	}
	return items, false
}

// rowPage is a row with all of its titles.
func (s *Server) rowPage(w http.ResponseWriter, r *http.Request) {
	row, err := s.fullRow(r.Context(), currentUser(r), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "no such row")
		return
	}
	if err != nil {
		s.serverError(w, err)
		return
	}
	row.More = false
	writeLargeJSON(w, r, row)
}

func (s *Server) fullRow(ctx context.Context, u store.User, id string) (HomeRow, error) {
	row := HomeRow{ID: id, Title: id}
	var err error
	kind, arg, _ := strings.Cut(id, ":")
	switch kind {
	case rowContinue:
		row.Title = "Continue watching"
		row.Items, err = s.Store.ContinueWatching(ctx, u.ID, rowAllItems)
	case rowListening:
		row.Title = "Continue listening"
		row.Items, err = s.Store.ContinueListening(ctx, u.ID, rowAllItems)
	case rowNewEpisodes:
		row.Title = "New episodes"
		row.Items, err = s.Store.NewEpisodes(ctx, u.ID, today().AddDate(0, 0, -newEpisodeDays).Unix(), rowAllItems)
	case rowRecent:
		row.Title = "Recently added"
		row.Items, err = s.Store.RecentlyAdded(ctx, u.ID, rowAllItems)
	case rowReleases:
		row.Title = "New releases"
		row.Items, err = s.Store.NewReleases(ctx, u.ID, today().Year()-newReleaseYrs+1, rowAllItems)
	case rowWatchList:
		row.Title = "Watch list"
		row.Items, err = s.Store.WatchList(ctx, u.ID, rowAllItems)
	case "genre":
		if arg == "" {
			return row, store.ErrNotFound
		}
		row.Title, row.Genre = genreRowTitle(arg), arg
		row.Items, err = s.Store.InGenre(ctx, u.ID, arg, rowAllItems)
	case "holiday":
		i := slices.IndexFunc(holidays, func(h holiday) bool { return h.ID == arg })
		if i < 0 {
			return row, store.ErrNotFound
		}
		h := holidays[i]
		row.Title, row.Holiday = h.Title, h.ID
		row.Items, err = s.holidayItems(ctx, u.ID, h, rowAllItems, nil)
	case "collection":
		c, err := s.Store.Collection(ctx, arg)
		if err == nil && !canSee(u, c) {
			err = store.ErrNotFound
		}
		if err != nil {
			return row, err
		}
		row.Title, row.CollectionID, row.Starter = c.Name, c.ID, c.Starter
		row.Items, _, err = s.resolveCollection(ctx, u.ID, c, rowAllItems)
		return row, err
	case "related":
		itemID, perr := strconv.ParseInt(arg, 10, 64)
		if perr != nil {
			return row, store.ErrNotFound
		}
		it, err := s.Store.Item(ctx, u.ID, itemID)
		if err != nil {
			return row, err
		}
		row.Title, row.RelatedTo = "More like "+it.Title, it.Title
		row.Items, err = s.Store.RelatedN(ctx, u.ID, it, rowAllItems)
		return row, err
	default:
		return row, store.ErrNotFound
	}
	return row, err
}
