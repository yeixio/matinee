package api

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"time"

	"github.com/yeixio/matinee/internal/store"
)

// PersonSource looks up people's biographies; metadata.TMDB is one.
type PersonSource interface {
	Person(ctx context.Context, id int64) (*store.PersonInfo, error)
}

// Biographies are looked up again after this long.
const personRefresh = 30 * 24 * time.Hour

// addDetails fills in an item's details and credits. Seasons and episodes
// borrow what they don't have from their show.
func (s *Server) addDetails(ctx context.Context, v *itemView) error {
	it := v.Item
	switch it.Kind {
	case "movie", "show", "season", "episode":
	default:
		return nil
	}
	d, err := s.Store.ItemDetails(ctx, it.ID)
	if err != nil {
		return err
	}
	credits, err := s.Store.ItemCredits(ctx, it.ID)
	if err != nil {
		return err
	}
	if (it.Kind == "season" || it.Kind == "episode") && len(v.Parents) > 0 {
		show := v.Parents[0]
		sd, err := s.Store.ItemDetails(ctx, show.ID)
		if err != nil {
			return err
		}
		if sd != nil {
			if d == nil {
				d = &store.Details{}
			}
			d.Genres, d.Certification, d.Networks = sd.Genres, sd.Certification, sd.Networks
		}
		// An episode's cast is its season's regulars; its own credits are
		// guest stars and the people who directed and wrote it.
		for i := len(v.Parents) - 1; i >= 0; i-- {
			needCast := len(credits.Cast) == 0
			needCrew := it.Kind == "season" && len(credits.Crew) == 0
			if !needCast && !needCrew {
				break
			}
			pc, err := s.Store.ItemCredits(ctx, v.Parents[i].ID)
			if err != nil {
				return err
			}
			if needCast {
				credits.Cast = pc.Cast
			}
			if needCrew {
				credits.Crew = pc.Crew
			}
		}
	}
	v.Details = d
	v.Credits = &credits
	return nil
}

type relatedView struct {
	// Collection is the series the movie belongs to, such as a franchise's
	// films, with the ones in the library. Absent for titles in none.
	Collection *collectionView `json:"collection,omitempty"`
	// Items are other titles in the library like this one, best first.
	Items []store.Item `json:"items"`
}

type collectionView struct {
	store.Collection
	Items []store.Item `json:"items"`
}

func (s *Server) itemRelated(w http.ResponseWriter, r *http.Request) {
	it, ok := s.loadItem(w, r)
	if !ok {
		return
	}
	ctx, user := r.Context(), currentUser(r)
	v := relatedView{Items: []store.Item{}}
	if it.Kind != "movie" && it.Kind != "show" {
		writeJSON(w, http.StatusOK, v)
		return
	}
	d, err := s.Store.ItemDetails(ctx, it.ID)
	if err != nil {
		s.serverError(w, err)
		return
	}
	if d != nil && d.Collection != nil {
		items, err := s.Store.CollectionItems(ctx, user.ID, d.Collection.ID)
		if err != nil {
			s.serverError(w, err)
			return
		}
		// A collection of one is just this movie.
		if len(items) > 1 {
			v.Collection = &collectionView{Collection: *d.Collection, Items: items}
		}
	}
	if v.Items, err = s.Store.Related(ctx, user.ID, it); err != nil {
		s.serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

type personView struct {
	store.Person
	// Titles are the movies and shows in the library the person is in,
	// newest first, with their parts in each.
	Titles []store.PersonTitle `json:"titles"`
}

func (s *Server) loadPerson(w http.ResponseWriter, r *http.Request) (store.Person, bool) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad id")
		return store.Person{}, false
	}
	p, err := s.Store.Person(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "person not found")
		return p, false
	}
	if err != nil {
		s.serverError(w, err)
		return p, false
	}
	return p, true
}

func (s *Server) getPerson(w http.ResponseWriter, r *http.Request) {
	p, ok := s.loadPerson(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	if s.People != nil && time.Since(time.Unix(p.InfoAt, 0)) > personRefresh {
		lookup, cancel := context.WithTimeout(ctx, 5*time.Second)
		info, err := s.People.Person(lookup, p.ID)
		cancel()
		switch {
		case err != nil:
			// Show what's known; the biography can come next time.
			s.Log.Warn("person lookup", "person", p.Name, "err", err)
		case info != nil:
			if err := s.Store.SetPersonInfo(ctx, p.ID, *info); err != nil {
				s.serverError(w, err)
				return
			}
			if p, err = s.Store.Person(ctx, p.ID); err != nil {
				s.serverError(w, err)
				return
			}
		}
	}
	titles, err := s.Store.PersonTitles(ctx, currentUser(r).ID, p.ID)
	if err != nil {
		s.serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, personView{Person: p, Titles: titles})
}

var tmdbProfileSize = regexp.MustCompile(`^(https://image\.tmdb\.org/t/p/)w185/`)

func (s *Server) personImage(w http.ResponseWriter, r *http.Request) {
	p, ok := s.loadPerson(w, r)
	if !ok {
		return
	}
	if p.Photo == "" {
		http.NotFound(w, r)
		return
	}
	// TVs ask for size=large, falling back to the usual size.
	if r.URL.Query().Get("size") == "large" {
		if large := tmdbProfileSize.ReplaceAllString(p.Photo, "${1}h632/"); large != p.Photo {
			s.Art.Serve(w, r, large, p.Photo)
			return
		}
	}
	s.Art.Serve(w, r, p.Photo)
}
