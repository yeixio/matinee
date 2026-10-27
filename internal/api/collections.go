package api

import (
	"cmp"
	"context"
	"errors"
	"hash/fnv"
	"math/rand/v2"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/yeixio/matinee/internal/store"
)

// Collections (see store.SavedCollection) and box sets. Anyone can look at
// the shared collections and their own; signing in is needed to make them.
// Shared ones are made and changed by admins, so the Home guest only looks.
// A smart collection is evaluated for whoever is looking, so its watched
// rules follow their progress.

const (
	collectionRowItems  = 20 // titles in a pinned collection's row
	collectionPreview   = 12 // titles with each collection in the list
	collectionNameLimit = 100
)

// manualSorts are the orders a manual collection can have; it has no
// filter to sort by release date or last played.
var manualSorts = map[string]bool{"": true, "title": true, "added": true, "year": true, "rating": true, "runtime": true, "shuffle": true}

type savedView struct {
	store.SavedCollection
	Count   int          `json:"count"`
	Items   []store.Item `json:"items"`
	CanEdit bool         `json:"canEdit"`
	// Contains says whether the item asked about (?item=) is in this manual collection.
	Contains *bool `json:"contains,omitempty"`
}

func canSee(u store.User, c store.SavedCollection) bool {
	return c.OwnerID == nil || *c.OwnerID == u.ID
}

func canEdit(u store.User, c store.SavedCollection) bool {
	if u.Guest {
		return false
	}
	if c.OwnerID == nil {
		return u.IsAdmin
	}
	return *c.OwnerID == u.ID
}

// resolveCollection lists a collection's titles for a user, at most limit
// of them when limit > 0, and how many there are in all.
func (s *Server) resolveCollection(ctx context.Context, userID int64, c store.SavedCollection, limit int) ([]store.Item, int, error) {
	var items []store.Item
	var err error
	if c.Kind == store.CollectionSmart {
		f := store.ItemFilter{}
		if c.Filter != nil {
			f = *c.Filter
		}
		f.Limit = 0
		if c.Sort != "" && c.Sort != "shuffle" {
			f.Sort, f.Order = c.Sort, c.Order
		}
		items, err = s.Store.FilterItems(ctx, userID, f)
		// A limit saved in the filter caps the collection, like "the 10 newest".
		if c.Filter != nil && c.Filter.Limit > 0 && len(items) > c.Filter.Limit {
			items = items[:c.Filter.Limit]
		}
	} else {
		items, err = s.Store.ManualItems(ctx, userID, c.ID)
		if err == nil {
			sortItems(items, c.Sort, c.Order)
		}
	}
	if err != nil {
		return nil, 0, err
	}
	if c.Sort == "shuffle" {
		h := fnv.New64a()
		h.Write([]byte(c.ID))
		rand.New(rand.NewPCG(dayNumber(today()), h.Sum64())).Shuffle(len(items), func(a, b int) { items[a], items[b] = items[b], items[a] })
	}
	n := len(items)
	if limit > 0 && n > limit {
		items = items[:limit]
	}
	return items, n, nil
}

// sortItems puts a manual collection in the given order; "" keeps the order
// titles were added in.
func sortItems(items []store.Item, by, order string) {
	key := func(a, b store.Item) int {
		switch by {
		case "title":
			return strings.Compare(store.SortTitle(a.Title), store.SortTitle(b.Title))
		case "added":
			return -cmp.Compare(a.AddedAt, b.AddedAt) // newest first
		case "year":
			return -cmp.Compare(orZero(a.Year), orZero(b.Year))
		case "rating":
			return -cmp.Compare(orZero(a.Rating), orZero(b.Rating))
		case "runtime":
			return cmp.Compare(orZero(a.DurationMs), orZero(b.DurationMs))
		}
		return 0
	}
	if by == "" || by == "shuffle" {
		return
	}
	slices.SortStableFunc(items, func(a, b store.Item) int {
		if order == "asc" && by != "title" && by != "runtime" || order == "desc" && (by == "title" || by == "runtime") {
			return -key(a, b)
		}
		return key(a, b)
	})
}

func (s *Server) viewCollection(ctx context.Context, u store.User, c store.SavedCollection, limit int) (savedView, error) {
	items, n, err := s.resolveCollection(ctx, u.ID, c, limit)
	return savedView{SavedCollection: c, Count: n, Items: items, CanEdit: canEdit(u, c)}, err
}

// listCollections lists the collections someone can see and the box sets.
// ?library=ID lists only the collections pinned to that library, as rows.
// ?item=ID says which manual collections hold that item.
func (s *Server) listCollections(w http.ResponseWriter, r *http.Request) {
	ctx, u := r.Context(), currentUser(r)
	all, err := s.Store.Collections(ctx, u.ID)
	if err != nil {
		s.serverError(w, err)
		return
	}
	q := r.URL.Query()
	limit := collectionPreview
	if lib := q.Get("library"); lib != "" {
		id, err := strconv.ParseInt(lib, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad library")
			return
		}
		all = slices.DeleteFunc(all, func(c store.SavedCollection) bool { return c.PinLibrary == nil || *c.PinLibrary != id })
		limit = collectionRowItems
	}
	var holding []string
	if item := q.Get("item"); item != "" {
		id, err := strconv.ParseInt(item, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad item")
			return
		}
		if holding, err = s.Store.ItemCollections(ctx, id); err != nil {
			s.serverError(w, err)
			return
		}
	}
	out := []savedView{}
	for _, c := range all {
		v, err := s.viewCollection(ctx, u, c, limit)
		if err != nil {
			s.serverError(w, err)
			return
		}
		if q.Get("item") != "" && c.Kind == store.CollectionManual {
			in := slices.Contains(holding, c.ID)
			v.Contains = &in
		}
		out = append(out, v)
	}
	boxSets := []store.BoxSet{}
	if q.Get("library") == "" {
		if boxSets, err = s.Store.BoxSets(ctx, u.ID); err != nil {
			s.serverError(w, err)
			return
		}
	}
	writeLargeJSON(w, r, map[string]any{"collections": out, "boxSets": boxSets})
}

// loadCollection finds the collection in the path, if the user may see it.
func (s *Server) loadCollection(w http.ResponseWriter, r *http.Request) (store.SavedCollection, bool) {
	c, err := s.Store.Collection(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) || err == nil && !canSee(currentUser(r), c) {
		writeError(w, http.StatusNotFound, "collection not found")
		return c, false
	}
	if err != nil {
		s.serverError(w, err)
		return c, false
	}
	return c, true
}

func (s *Server) loadEditableCollection(w http.ResponseWriter, r *http.Request) (store.SavedCollection, bool) {
	c, ok := s.loadCollection(w, r)
	if ok && !canEdit(currentUser(r), c) {
		writeError(w, http.StatusForbidden, "only an admin can change a shared collection")
		return c, false
	}
	return c, ok
}

func (s *Server) getCollection(w http.ResponseWriter, r *http.Request) {
	c, ok := s.loadCollection(w, r)
	if !ok {
		return
	}
	v, err := s.viewCollection(r.Context(), currentUser(r), c, 0)
	if err != nil {
		s.serverError(w, err)
		return
	}
	writeLargeJSON(w, r, v)
}

// collectionInput is what people send to make or change a collection.
type collectionInput struct {
	Name       string            `json:"name"`
	Kind       string            `json:"kind"` // only when making one
	Filter     *store.ItemFilter `json:"filter"`
	Sort       string            `json:"sort"`
	Order      string            `json:"order"`
	PinHome    bool              `json:"pinHome"`
	PinLibrary *int64            `json:"pinLibrary"`
	Shared     bool              `json:"shared"` // only when making one; admins only
}

// apply checks the input and copies it onto c, whose Kind is already set.
func (in collectionInput) apply(c *store.SavedCollection) string {
	in.Name = strings.TrimSpace(in.Name)
	switch {
	case in.Name == "":
		return "a collection needs a name"
	case len(in.Name) > collectionNameLimit:
		return "that name is too long"
	case in.Order != "" && in.Order != "asc" && in.Order != "desc":
		return "order is asc or desc"
	case c.Kind == store.CollectionSmart && in.Filter == nil:
		return "a smart collection needs a filter"
	case c.Kind == store.CollectionSmart && in.Sort != "" && !store.CollectionSorts[in.Sort]:
		return "unknown sort"
	case c.Kind == store.CollectionManual && !manualSorts[in.Sort]:
		return "a collection kept by hand sorts by title, added, year, rating, runtime or shuffle"
	}
	if c.Kind == store.CollectionSmart {
		if err := in.Filter.Validate(); err != nil {
			return err.Error()
		}
	}
	c.Name, c.Sort, c.Order, c.PinHome, c.PinLibrary = in.Name, in.Sort, in.Order, in.PinHome, in.PinLibrary
	if c.Kind == store.CollectionSmart {
		c.Filter = in.Filter
	}
	return ""
}

func (s *Server) createCollection(w http.ResponseWriter, r *http.Request) {
	var in collectionInput
	if err := readJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "bad request")
		return
	}
	u := currentUser(r)
	if in.Kind != store.CollectionSmart && in.Kind != store.CollectionManual {
		writeError(w, http.StatusBadRequest, "kind is smart or manual")
		return
	}
	if in.Shared && !u.IsAdmin {
		writeError(w, http.StatusForbidden, "only an admin can share a collection")
		return
	}
	c := store.SavedCollection{Kind: in.Kind}
	if msg := in.apply(&c); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	if !in.Shared {
		c.OwnerID = &u.ID
	}
	if !s.libraryExists(w, r, c.PinLibrary) {
		return
	}
	c, err := s.Store.CreateCollection(r.Context(), c)
	if err != nil {
		s.serverError(w, err)
		return
	}
	v, err := s.viewCollection(r.Context(), u, c, collectionPreview)
	if err != nil {
		s.serverError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, v)
}

func (s *Server) libraryExists(w http.ResponseWriter, r *http.Request, id *int64) bool {
	if id == nil {
		return true
	}
	_, err := s.Store.Library(r.Context(), *id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusBadRequest, "no such library")
		return false
	}
	if err != nil {
		s.serverError(w, err)
		return false
	}
	return true
}

func (s *Server) updateCollection(w http.ResponseWriter, r *http.Request) {
	c, ok := s.loadEditableCollection(w, r)
	if !ok {
		return
	}
	var in collectionInput
	if err := readJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "bad request")
		return
	}
	if in.Kind != "" && in.Kind != c.Kind {
		writeError(w, http.StatusBadRequest, "a collection's kind can't change")
		return
	}
	if msg := in.apply(&c); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	if !s.libraryExists(w, r, c.PinLibrary) {
		return
	}
	c, err := s.Store.UpdateCollection(r.Context(), c)
	if err != nil {
		s.serverError(w, err)
		return
	}
	v, err := s.viewCollection(r.Context(), currentUser(r), c, collectionPreview)
	if err != nil {
		s.serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) deleteCollection(w http.ResponseWriter, r *http.Request) {
	c, ok := s.loadEditableCollection(w, r)
	if !ok {
		return
	}
	if err := s.Store.DeleteCollection(r.Context(), c.ID); err != nil {
		s.serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// collectionItem loads a manual collection the user may change and the
// item in the path.
func (s *Server) collectionItem(w http.ResponseWriter, r *http.Request) (store.SavedCollection, int64, bool) {
	c, ok := s.loadEditableCollection(w, r)
	if !ok {
		return c, 0, false
	}
	if c.Kind != store.CollectionManual {
		writeError(w, http.StatusBadRequest, "a smart collection fills itself")
		return c, 0, false
	}
	id, err := strconv.ParseInt(r.PathValue("item"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad item")
		return c, 0, false
	}
	return c, id, true
}

func (s *Server) addToCollection(w http.ResponseWriter, r *http.Request) {
	c, id, ok := s.collectionItem(w, r)
	if !ok {
		return
	}
	if _, err := s.Store.Item(r.Context(), currentUser(r).ID, id); errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "item not found")
		return
	} else if err != nil {
		s.serverError(w, err)
		return
	}
	if err := s.Store.AddToCollection(r.Context(), c.ID, id); err != nil {
		s.serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) removeFromCollection(w http.ResponseWriter, r *http.Request) {
	c, id, ok := s.collectionItem(w, r)
	if !ok {
		return
	}
	if err := s.Store.RemoveFromCollection(r.Context(), c.ID, id); err != nil {
		s.serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) getBoxSet(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	b, err := s.Store.BoxSet(r.Context(), currentUser(r).ID, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "box set not found")
		return
	}
	if err != nil {
		s.serverError(w, err)
		return
	}
	items, err := s.Store.CollectionItems(r.Context(), currentUser(r).ID, id)
	if err != nil {
		s.serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": b.ID, "name": b.Name, "items": items})
}

// pinnedRows are the collections pinned to Home, as rows, skipping empty ones.
func (s *Server) pinnedRows(ctx context.Context, u store.User) ([]HomeRow, error) {
	all, err := s.Store.Collections(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	rows := []HomeRow{}
	for _, c := range all {
		if !c.PinHome {
			continue
		}
		items, n, err := s.resolveCollection(ctx, u.ID, c, collectionRowItems)
		if err != nil {
			return nil, err
		}
		if len(items) > 0 {
			rows = append(rows, HomeRow{ID: "collection:" + c.ID, Title: c.Name, CollectionID: c.ID, Starter: c.Starter, Items: items, More: n > len(items)})
		}
	}
	return rows, nil
}

func orZero[T any](p *T) T {
	var zero T
	if p == nil {
		return zero
	}
	return *p
}
